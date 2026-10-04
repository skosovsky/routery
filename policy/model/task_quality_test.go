package model

import (
	"testing"
	"time"

	"github.com/skosovsky/routery/policy"
)

func TestOptionalAndDefaultQualityAreTaskScopedAtRank(t *testing.T) {
	t.Parallel()
	now := time.Unix(100, 0)
	measurement := Measurement{
		Identity:   "sample",
		Source:     "eval",
		ObservedAt: now.Add(-time.Minute),
		ValidUntil: now.Add(time.Minute),
		Samples:    10,
	}
	for _, test := range []struct {
		name, task, observedTask, defaultTask string
		want                                  float64
		wantQuality                           bool
	}{
		{"current", "current", "current", "other", 0.8, true},
		{"wrong", "current", "other", "other", 0, false},
		{"missing-task", "", "current", "current", 0, false},
		{"current-default", "current", "other", "current", 0.9, true},
		{"absent", "current", "", "other", 0, false},
		{"expired", "current", "current", "other", 0, false},
		{"invalid-provenance", "current", "current", "other", 0, false},
		{"future", "current", "current", "other", 0, false},
		{"invalid-default", "current", "other", "current", 0, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// Arrange.
			qualityMeasurement, defaultMeasurement := measurement, measurement
			switch test.name {
			case "expired":
				qualityMeasurement.ValidUntil = now
			case "invalid-provenance":
				qualityMeasurement.Source = ""
			case "future":
				qualityMeasurement.ObservedAt = now.Add(time.Minute)
			case "invalid-default":
				defaultMeasurement.Source = ""
			}
			descriptor := Descriptor[string]{
				RetainsData: new(false),
				FreshUntil:  now.Add(time.Hour),
				Estimates: Estimates{
					Quality: &Quality{Task: test.observedTask, Score: 0.8, Measurement: qualityMeasurement},
				},
			}
			config := Config{
				Policy:   "policy",
				Optional: DefaultOptional,
				Defaults: Estimates{
					Quality: &Quality{Task: test.defaultTask, Score: 0.9, Measurement: defaultMeasurement},
				},
			}
			ranked := false
			selector := Selector(
				config,
				func(_ policy.Evaluation[Request[string]], candidate policy.Candidate[string, string, Descriptor[string]]) (float64, error) {
					ranked = true
					quality := candidate.Descriptor.Estimates.Quality
					if test.wantQuality {
						if quality == nil || quality.Task != test.task || quality.Score != test.want {
							t.Fatalf("quality=%+v", quality)
						}
					} else if quality != nil {
						t.Fatalf("wrong-task quality leaked: %+v", quality)
					}
					return 1, nil
				},
			)
			evaluation := policy.Evaluation[Request[string]]{
				Input:      Request[string]{Policy: "policy", Task: test.task},
				Now:        now,
				References: policy.References{Input: "i", Candidates: "c", Policy: "p"},
			}
			candidate := policy.Candidate[string, string, Descriptor[string]]{
				Key:         "candidate",
				Scope:       "scope",
				Route:       "route",
				Fingerprint: "facts",
				Descriptor:  descriptor,
			}
			// Act.
			result, err := selector.Select(
				t.Context(),
				evaluation,
				[]policy.Candidate[string, string, Descriptor[string]]{candidate},
				policy.Affinity[string, string, Descriptor[string]]{},
			)
			// Assert.
			if err != nil || !ranked || result.Status != policy.Selected {
				t.Fatal(result, err, ranked)
			}
		})
	}
}
