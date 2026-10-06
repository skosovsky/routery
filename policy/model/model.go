package model

import (
	"errors"
	"maps"
	"math"
	"slices"
	"time"

	"github.com/skosovsky/routery/policy"
)

// ErrInvalidConstraints indicates malformed caller requirements or policy identity.
var ErrInvalidConstraints = errors.New("routery/policy/model: invalid constraints")

// ErrEstimateUnavailable indicates missing required measurement or default facts.
var ErrEstimateUnavailable = errors.New("routery/policy/model: estimate unavailable")

// ErrStaleDescriptor indicates missing or expired mandatory capability facts.
var ErrStaleDescriptor = errors.New("routery/policy/model: stale descriptor")

// Reason is bounded eligibility metadata, never a description of request content.
type Reason uint8

const (
	Eligible Reason = iota
	CapabilityMissing
	ContextTooSmall
	DataPolicyMismatch
	QualityMissing
	QualityTooLow
)

// Metric identifies distinct latency/performance estimates.
type Metric uint8

const (
	FirstFragment Metric = iota
	InterFragment
	FullResponse
	Throughput
)

// Measurement preserves provenance and declared freshness, not a service guarantee.
type Measurement struct {
	Identity   string
	Source     string
	ObservedAt time.Time
	ValidUntil time.Time
	Window     time.Duration
	Samples    uint64
}

// Cost is a caller-supplied estimate, with caller-defined units and optional currency.
type Cost struct {
	Amount      float64
	Unit        string
	Currency    string
	Measurement Measurement
}

// Performance records measured regime and percentile for one declared metric.
type Performance struct {
	Value       float64
	Unit        string
	Percentile  float64
	CacheRegime string
	Measurement Measurement
}

// Quality is task-scoped measured quality, not a global model property.
type Quality struct {
	Task        string
	Score       float64
	Measurement Measurement
}

// Estimates has no built-in ranking weights. Callers choose how to rank preferences.
type Estimates struct {
	Cost        *Cost
	Performance map[Metric]Performance
	Quality     *Quality
}

// Descriptor contains opt-in model facts. Unknown capability is not supported.
type Descriptor[Capability comparable] struct {
	Capabilities  map[Capability]bool
	ContextWindow uint64
	Residency     string
	RetainsData   *bool
	FreshUntil    time.Time
	Estimates     Estimates
}

// Request is a projection owned by the host, excluding secret or provider payloads.
type Request[Capability comparable] struct {
	Policy         string
	Task           string
	Tokens         uint64
	Required       []Capability
	Residencies    []string
	AllowRetention bool
	MinimumQuality *float64
}

// OptionalEstimatePolicy declares handling of absent or stale preferences.
type OptionalEstimatePolicy uint8

const (
	IgnoreOptional OptionalEstimatePolicy = iota
	RejectOptional
	DefaultOptional
)

// Config declares the accepted policy identity and required optional ranking inputs.
// Defaults must have provenance/freshness too; they never satisfy mandatory quality.
type Config struct {
	Policy             string
	Optional           OptionalEstimatePolicy
	RequireCost        bool
	RequirePerformance []Metric
	Defaults           Estimates
}

// Selector builds a model adapter over the existing generic selector.
// rank receives sanitized optional estimates after hard constraints pass.
func Selector[Capability comparable, Key comparable, Scope comparable](
	config Config,
	rank func(policy.Evaluation[Request[Capability]], policy.Candidate[Key, Scope, Descriptor[Capability]]) (float64, error),
) policy.Selector[Request[Capability], Key, Scope, Descriptor[Capability], Reason] {
	config.RequirePerformance = slices.Clone(config.RequirePerformance)
	config.Defaults = cloneEstimates(config.Defaults)
	return policy.Selector[Request[Capability], Key, Scope, Descriptor[Capability], Reason]{
		Validate: func(evaluation policy.Evaluation[Request[Capability]]) error {
			if rank == nil || config.Policy == "" || evaluation.Input.Policy != config.Policy ||
				config.Optional > DefaultOptional {
				return ErrInvalidConstraints
			}
			minimum := evaluation.Input.MinimumQuality
			if minimum != nil && (evaluation.Input.Task == "" || !validNumber(*minimum) || *minimum > 1) {
				return ErrInvalidConstraints
			}
			for _, metric := range config.RequirePerformance {
				if metric > Throughput {
					return ErrInvalidConstraints
				}
			}
			return nil
		},
		Freeze: func(descriptor Descriptor[Capability]) Descriptor[Capability] {
			descriptor.Capabilities = maps.Clone(descriptor.Capabilities)
			descriptor.Estimates = cloneEstimates(descriptor.Estimates)
			if descriptor.RetainsData != nil {
				retains := *descriptor.RetainsData
				descriptor.RetainsData = &retains
			}
			return descriptor
		},
		Eligible: func(evaluation policy.Evaluation[Request[Capability]], candidate policy.Candidate[Key, Scope, Descriptor[Capability]]) (policy.Eligibility[Reason], error) {
			return eligibility(evaluation, candidate.Descriptor)
		},
		Rank: func(evaluation policy.Evaluation[Request[Capability]], candidate policy.Candidate[Key, Scope, Descriptor[Capability]]) (float64, error) {
			estimates, err := effectiveEstimates(
				candidate.Descriptor.Estimates,
				config,
				evaluation.Now,
				evaluation.Input.Task,
			)
			if err != nil {
				return 0, err
			}
			candidate.Descriptor.Estimates = estimates
			return rank(evaluation, candidate)
		},
	}
}

func eligibility[Capability comparable](
	evaluation policy.Evaluation[Request[Capability]],
	descriptor Descriptor[Capability],
) (policy.Eligibility[Reason], error) {
	if descriptor.FreshUntil.IsZero() || !evaluation.Now.Before(descriptor.FreshUntil) {
		return policy.Eligibility[Reason]{}, ErrStaleDescriptor
	}
	for _, capability := range evaluation.Input.Required {
		if !descriptor.Capabilities[capability] {
			return policy.Eligibility[Reason]{Allowed: false, Reason: CapabilityMissing}, nil
		}
	}
	if evaluation.Input.Tokens > descriptor.ContextWindow {
		return policy.Eligibility[Reason]{Allowed: false, Reason: ContextTooSmall}, nil
	}
	if (len(evaluation.Input.Residencies) > 0 && !slices.Contains(evaluation.Input.Residencies, descriptor.Residency)) ||
		(!evaluation.Input.AllowRetention && (descriptor.RetainsData == nil || *descriptor.RetainsData)) {
		return policy.Eligibility[Reason]{Allowed: false, Reason: DataPolicyMismatch}, nil
	}
	if minimum := evaluation.Input.MinimumQuality; minimum != nil {
		quality := descriptor.Estimates.Quality
		if !validQuality(quality, evaluation.Now) || quality.Task != evaluation.Input.Task {
			return policy.Eligibility[Reason]{Allowed: false, Reason: QualityMissing}, nil
		}
		if quality.Score < *minimum {
			return policy.Eligibility[Reason]{Allowed: false, Reason: QualityTooLow}, nil
		}
	}
	return policy.Eligibility[Reason]{Allowed: true, Reason: Eligible}, nil
}

func effectiveEstimates(estimates Estimates, config Config, now time.Time, task string) (Estimates, error) {
	effective := cloneEstimates(estimates)
	if !validCost(effective.Cost, now) {
		effective.Cost = nil
	}
	if !validTaskQuality(effective.Quality, now, task) {
		effective.Quality = nil
	}
	for metric, value := range effective.Performance {
		if metric > Throughput || !validPerformance(value, now) {
			delete(effective.Performance, metric)
		}
	}
	if config.Optional == DefaultOptional {
		effective = withDefaults(effective, config.Defaults, now, task)
	}
	if config.Optional != IgnoreOptional {
		if config.RequireCost && effective.Cost == nil {
			return Estimates{}, ErrEstimateUnavailable
		}
		for _, metric := range config.RequirePerformance {
			if _, ok := effective.Performance[metric]; !ok {
				return Estimates{}, ErrEstimateUnavailable
			}
		}
	}
	return effective, nil
}

func withDefaults(effective, declared Estimates, now time.Time, task string) Estimates {
	defaults := cloneEstimates(declared)
	if effective.Cost == nil && validCost(defaults.Cost, now) {
		effective.Cost = defaults.Cost
	}
	if effective.Quality == nil && validTaskQuality(defaults.Quality, now, task) {
		effective.Quality = defaults.Quality
	}
	if effective.Performance == nil {
		effective.Performance = make(map[Metric]Performance)
	}
	for metric, value := range defaults.Performance {
		if _, ok := effective.Performance[metric]; !ok && metric <= Throughput && validPerformance(value, now) {
			effective.Performance[metric] = value
		}
	}
	return effective
}

func fresh(measurement Measurement, now time.Time) bool {
	return measurement.Identity != "" && measurement.Source != "" && !measurement.ObservedAt.After(now) &&
		!measurement.ObservedAt.IsZero() && now.Before(measurement.ValidUntil)
}

func validNumber(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0
}

func validCost(cost *Cost, now time.Time) bool {
	return cost != nil && cost.Unit != "" && validNumber(cost.Amount) && fresh(cost.Measurement, now)
}

func validQuality(quality *Quality, now time.Time) bool {
	return quality != nil && quality.Task != "" && validNumber(quality.Score) && quality.Score <= 1 &&
		quality.Measurement.Samples > 0 && fresh(quality.Measurement, now)
}

func validPerformance(value Performance, now time.Time) bool {
	return validNumber(value.Value) && value.Unit != "" && validNumber(value.Percentile) && value.Percentile <= 100 &&
		value.CacheRegime != "" && value.Measurement.Samples > 0 && value.Measurement.Window > 0 && fresh(value.Measurement, now)
}

func cloneEstimates(estimates Estimates) Estimates {
	estimates.Performance = maps.Clone(estimates.Performance)
	if estimates.Cost != nil {
		cost := *estimates.Cost
		estimates.Cost = &cost
	}
	if estimates.Quality != nil {
		quality := *estimates.Quality
		estimates.Quality = &quality
	}
	return estimates
}

func validTaskQuality(quality *Quality, now time.Time, task string) bool {
	return task != "" && validQuality(quality, now) && quality.Task == task
}
