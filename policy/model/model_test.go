package model

import (
	"errors"
	"testing"
	"time"

	"github.com/skosovsky/routery/policy"
)

type capability uint8

const schema capability = 1

func TestModelHardConstraintsAndQualityNeverUseDefaults(t *testing.T) {
	for _, scenario := range []struct {
		name    string
		schema  bool
		retains *bool
		quality *Quality
		want    Reason
	}{
		{name: "unknown schema", want: CapabilityMissing},
		{name: "unknown retention", schema: true, want: DataPolicyMismatch},
		{name: "missing quality", schema: true, retains: new(false), want: QualityMissing},
		{name: "wrong task quality", schema: true, retains: new(false), quality: &Quality{Task: "other", Score: 0.9}, want: QualityMissing},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			// Arrange.
			now := time.Unix(100, 0)
			evaluation := policy.Evaluation[Request[capability]]{
				Input: Request[capability]{
					Policy:         "declared",
					Task:           "schema-task",
					Required:       []capability{schema},
					MinimumQuality: new(0.8),
				},
				Now:        now,
				References: policy.References{Input: "input", Candidates: "candidates", Policy: "declared"},
			}
			descriptor := Descriptor[capability]{
				Capabilities: map[capability]bool{schema: scenario.schema},
				RetainsData:  scenario.retains,
				FreshUntil:   now.Add(time.Hour),
				Estimates:    Estimates{Quality: scenario.quality},
			}
			selector := Selector(
				Config{
					Policy:   "declared",
					Optional: DefaultOptional,
					Defaults: Estimates{Quality: &Quality{Task: "schema-task", Score: 1}},
				},
				func(policy.Evaluation[Request[capability]], policy.Candidate[string, string, Descriptor[capability]]) (float64, error) {
					return 1, nil
				},
			)
			// Act.
			result, err := selector.Select(
				t.Context(),
				evaluation,
				[]policy.Candidate[string, string, Descriptor[capability]]{
					{
						Key:         "candidate",
						Scope:       "tenant",
						Route:       "candidate",
						Fingerprint: "facts",
						Descriptor:  descriptor,
					},
				},
				policy.Affinity[string, string, Descriptor[capability]]{},
			)
			// Assert.
			if err != nil || result.Status != policy.NoEligible || result.Explanation[0].Reason != scenario.want {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
}

func TestOptionalEstimatesPreserveMeasurementSemantics(t *testing.T) {
	// Arrange.
	now := time.Unix(100, 0)
	measurement := Measurement{
		Identity:   "measurement",
		Source:     "caller",
		ObservedAt: now.Add(-time.Minute),
		ValidUntil: now.Add(time.Minute),
		Window:     time.Minute,
		Samples:    10,
	}
	performance := Performance{
		Value:       100,
		Unit:        "units/s",
		Percentile:  95,
		CacheRegime: "warm",
		Measurement: measurement,
	}
	defaults := Estimates{
		Cost:        &Cost{Amount: 2, Unit: "request", Currency: "caller-currency", Measurement: measurement},
		Performance: make(map[Metric]Performance),
	}
	defaults.Performance[Throughput] = performance
	config := Config{
		Policy:             "declared",
		Optional:           DefaultOptional,
		RequireCost:        true,
		RequirePerformance: []Metric{Throughput},
		Defaults:           defaults,
	}
	// Act.
	effective, err := effectiveEstimates(Estimates{}, config, now, "")
	config.Optional = RejectOptional
	_, rejected := effectiveEstimates(Estimates{}, config, now, "")
	config.Optional = IgnoreOptional
	ignored, ignoredErr := effectiveEstimates(defaults, config, now.Add(time.Hour), "")
	// Assert.
	if err != nil || effective.Cost.Amount != 2 || effective.Performance[Throughput] != performance {
		t.Fatal("estimate metadata lost")
	}
	if !errors.Is(rejected, ErrEstimateUnavailable) || ignoredErr != nil || ignored.Cost != nil ||
		len(ignored.Performance) != 0 {
		t.Fatal("stale estimates passed")
	}
}
