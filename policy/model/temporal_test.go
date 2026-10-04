package model

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/skosovsky/routery"
	"github.com/skosovsky/routery/policy"
)

func TestPinnedModelRequiresCurrentDescriptorAndQualityWindows(t *testing.T) {
	for _, name := range []string{"descriptor", "quality"} {
		t.Run(name, func(t *testing.T) {
			// Arrange.
			now := time.Unix(100, 0)
			evaluation := policy.Evaluation[Request[capability]]{
				Input: Request[capability]{Policy: "declared", Task: "task", MinimumQuality: new(0.8)},
				Now:   now,
				References: policy.References{
					Input:      "input",
					Candidates: "facts",
					Policy:     "declared",
					Estimates:  "quality",
				},
			}
			candidate := policy.Candidate[string, string, Descriptor[capability]]{
				Key: "endpoint", Scope: "tenant", Route: "endpoint", Fingerprint: "facts",
				Descriptor: Descriptor[capability]{
					RetainsData: new(false), FreshUntil: now.Add(time.Hour),
					Estimates: Estimates{Quality: &Quality{Task: "task", Score: 0.9,
						Measurement: Measurement{Identity: "quality", Source: "host", ObservedAt: now.Add(-time.Minute),
							ValidUntil: now.Add(time.Hour), Samples: 10}}},
				},
			}
			if name == "descriptor" {
				candidate.Descriptor.FreshUntil = now.Add(time.Minute)
			} else {
				candidate.Descriptor.Estimates.Quality.Measurement.ValidUntil = now.Add(time.Minute)
			}
			selector := Selector(
				Config{Policy: "declared", Optional: IgnoreOptional},
				func(policy.Evaluation[Request[capability]], policy.Candidate[string, string, Descriptor[capability]]) (float64, error) {
					return 1, nil
				},
			)
			affinity := policy.Affinity[string, string, Descriptor[capability]]{}
			selected, err := selector.Select(t.Context(), evaluation,
				[]policy.Candidate[string, string, Descriptor[capability]]{candidate}, affinity)
			if err != nil || selected.Status != policy.Selected {
				t.Fatalf("selection=%v err=%v", selected.Status, err)
			}
			evaluation.Now = now.Add(time.Minute)
			calls := 0
			// Act.
			_, dispatchErr := policy.Dispatch(
				t.Context(),
				evaluation.Input,
				selected,
				evaluation.References,
				func(ctx context.Context, selection policy.Selection[string, string, Descriptor[capability], Reason]) error {
					return selector.ValidatePinned(ctx, evaluation, selection, candidate, affinity)
				},
				func(context.Context, Request[capability], routery.RouteBinding[string, policy.Candidate[string, string, Descriptor[capability]]]) (int, error) {
					calls++
					return 1, nil
				},
			)
			// Assert.
			if calls != 0 {
				t.Fatal("expired pinned model dispatched")
			}
			assertExpiredPinnedModel(t, name, dispatchErr)
		})
	}
}

func assertExpiredPinnedModel(t *testing.T, name string, err error) {
	t.Helper()
	if name == "descriptor" {
		if !errors.Is(err, ErrStaleDescriptor) {
			t.Fatal(err)
		}
		return
	}
	var rejected *policy.PinnedError[Reason]
	if !errors.As(err, &rejected) || rejected.Reason != QualityMissing {
		t.Fatal(err)
	}
}
