package model

import (
	"errors"
	"testing"
	"time"

	"github.com/skosovsky/routery/policy"
)

func TestHardModelConstraintsRunBeforeRanking(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		change func(*Descriptor[capability])
		reason Reason
	}{
		{name: "context window", change: func(d *Descriptor[capability]) { d.ContextWindow = 1 }, reason: ContextTooSmall},
		{name: "residency", change: func(d *Descriptor[capability]) { d.Residency = "disallowed" }, reason: DataPolicyMismatch},
		{name: "retention", change: func(d *Descriptor[capability]) { d.RetainsData = new(true) }, reason: DataPolicyMismatch},
		{name: "quality floor", change: func(d *Descriptor[capability]) { d.Estimates.Quality.Score = 0.7 }, reason: QualityTooLow},
		{name: "stale quality", change: func(d *Descriptor[capability]) { d.Estimates.Quality.Measurement.ValidUntil = time.Unix(100, 0) }, reason: QualityMissing},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			// Arrange: the forbidden candidate would be ranked highest if ranking ran.
			evaluation, candidate := measuredModelFixture()
			scenario.change(&candidate.Descriptor)
			ranks := 0
			selector := Selector(
				Config{Policy: "declared", Optional: IgnoreOptional},
				func(policy.Evaluation[Request[capability]], policy.Candidate[string, string, Descriptor[capability]]) (float64, error) {
					ranks++
					return 1000, nil
				},
			)
			// Act.
			selected, err := selector.Select(t.Context(), evaluation,
				[]policy.Candidate[string, string, Descriptor[capability]]{candidate},
				policy.Affinity[string, string, Descriptor[capability]]{})
			// Assert.
			if err != nil || selected.Status != policy.NoEligible || ranks != 0 ||
				len(selected.Explanation) != 1 || selected.Explanation[0].Reason != scenario.reason {
				t.Fatalf("status=%v ranks=%d err=%v", selected.Status, ranks, err)
			}
		})
	}
}

func measuredModelFixture() (policy.Evaluation[Request[capability]], policy.Candidate[string, string, Descriptor[capability]]) {
	now := time.Unix(100, 0)
	evaluation := policy.Evaluation[Request[capability]]{
		Input: Request[capability]{Policy: "declared", Task: "task", Tokens: 10,
			Required: []capability{schema}, Residencies: []string{"allowed"}, MinimumQuality: new(0.8)},
		Now: now, References: policy.References{Input: "facts", Candidates: "endpoints", Policy: "declared"},
	}
	candidate := policy.Candidate[string, string, Descriptor[capability]]{
		Key: "endpoint", Scope: "trusted", Route: "route", Fingerprint: "descriptor",
		Descriptor: Descriptor[capability]{Capabilities: map[capability]bool{schema: true},
			ContextWindow: 100, Residency: "allowed", RetainsData: new(false), FreshUntil: now.Add(time.Hour),
			Estimates: Estimates{Quality: &Quality{Task: "task", Score: 0.9,
				Measurement: Measurement{Identity: "quality", Source: "host", ObservedAt: now.Add(-time.Minute),
					ValidUntil: now.Add(time.Hour), Samples: 10}}},
		},
	}
	return evaluation, candidate
}

func TestModelInvalidPolicyAndRequiredEstimatesReject(t *testing.T) {
	for _, name := range []string{"unsupported policy", "invalid floor", "stale descriptor", "missing cost"} {
		t.Run(name, func(t *testing.T) {
			// Arrange.
			evaluation, candidate := measuredModelFixture()
			config := Config{Policy: "declared", Optional: RejectOptional}
			want := ErrInvalidConstraints
			switch name {
			case "unsupported policy":
				evaluation.Input.Policy = "unsupported"
			case "invalid floor":
				evaluation.Input.MinimumQuality = new(1.1)
			case "stale descriptor":
				candidate.Descriptor.FreshUntil = evaluation.Now
				want = ErrStaleDescriptor
			case "missing cost":
				config.RequireCost = true
				want = ErrEstimateUnavailable
			}
			ranks := 0
			selector := Selector(
				config,
				func(policy.Evaluation[Request[capability]], policy.Candidate[string, string, Descriptor[capability]]) (float64, error) {
					ranks++
					return 1, nil
				},
			)
			// Act.
			_, err := selector.Select(t.Context(), evaluation,
				[]policy.Candidate[string, string, Descriptor[capability]]{candidate},
				policy.Affinity[string, string, Descriptor[capability]]{})
			// Assert.
			if !errors.Is(err, want) || ranks != 0 {
				t.Fatalf("ranks=%d err=%v want=%v", ranks, err, want)
			}
		})
	}
}
