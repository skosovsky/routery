package execution

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/skosovsky/routery"
	"github.com/skosovsky/routery/policy"
	"github.com/skosovsky/routery/policy/attempt"
	"github.com/skosovsky/routery/policy/model"
)

type fallbackModelCandidate = policy.Candidate[string, string, model.Descriptor[string]]
type fallbackModelSelection = policy.Selection[string, string, model.Descriptor[string], model.Reason]
type fallbackModelAffinity = policy.Affinity[string, string, model.Descriptor[string]]

type selectedModelCall struct {
	Evaluation policy.Evaluation[model.Request[string]]
	Selection  fallbackModelSelection
	Candidate  fallbackModelCandidate
	Affinity   fallbackModelAffinity
}

func TestSequenceFallbackReselectsWithoutWeakeningHardConstraints(t *testing.T) {
	for _, name := range []string{"capability", "residency"} {
		t.Run(name, func(t *testing.T) { checkModelFallback(t, name) })
	}
}

func checkModelFallback(t *testing.T, name string) {
	t.Helper()
	// Arrange: B has favorable rank and cache affinity, but fails a hard constraint.
	now := time.Unix(100, 0)
	evaluation := policy.Evaluation[model.Request[string]]{
		Input: model.Request[string]{Policy: "hard-policy", Tokens: 10,
			Required: []string{"schema"}, Residencies: []string{"allowed"}},
		Now: now, References: policy.References{Input: "request", Candidates: "initial", Policy: "hard-policy"},
	}
	initial, bad, good := fallbackCandidate("A", now), fallbackCandidate("B", now), fallbackCandidate("C", now)
	if name == "capability" {
		bad.Descriptor.Capabilities["schema"] = false
	} else {
		bad.Descriptor.Residency = "forbidden"
	}
	ranked := make(map[string]int)
	selector := model.Selector(model.Config{Policy: "hard-policy", Optional: model.IgnoreOptional},
		func(_ policy.Evaluation[model.Request[string]], candidate fallbackModelCandidate) (float64, error) {
			ranked[candidate.Key]++
			if candidate.Key == "B" {
				return 1000, nil
			}
			return 1, nil
		})
	affinity := fallbackModelAffinity{}
	selected, err := selector.Select(t.Context(), evaluation, []fallbackModelCandidate{initial}, affinity)
	if err != nil || selected.Status != policy.Selected {
		t.Fatalf("initial selection: %v", err)
	}
	coordinator, id := setup(t)
	calls := make(map[string]int)
	providerErr := errors.New("provider verified not executed")
	sequence := Sequence[selectedModelCall, failureClass, routery.BasicKind, routery.BasicReason, string]{
		Boundary: Boundary[selectedModelCall, routery.BasicKind, routery.BasicReason, string]{
			Fresh: func(ctx context.Context, request selectedModelCall) error {
				current := request.Evaluation
				current.Now = now
				return selector.ValidatePinned(ctx, current, request.Selection, request.Candidate, request.Affinity)
			},
			Dispatch: func(call routery.RouteCall[selectedModelCall], receipt *Receipt) (routery.BasicRouteResult[string], error) {
				endpoint := call.Request.Selection.Binding.Branch
				calls[endpoint]++
				event, _, snapshotErr := receipt.Snapshot()
				if snapshotErr != nil {
					return routery.BasicRouteResult[string]{}, snapshotErr
				}
				event.Phase, event.Outcome = attempt.Terminal, attempt.Completed
				if endpoint == "A" {
					event.Outcome = attempt.NotExecuted
					return routery.BasicRouteResult[string]{}, errors.Join(providerErr, receipt.Record(event))
				}
				return routery.BasicHandled(endpoint), receipt.Record(event)
			},
		},
		Classify: func(Result[routery.BasicKind, routery.BasicReason, string], error) (failureClass, error) {
			return transientFailure, nil
		},
		Replay: func(Failure[failureClass]) (attempt.Replay, error) {
			return attempt.Replay{Retryable: true, Replayable: true, UseFallback: true}, nil
		},
		Schedule: func(Failure[failureClass], attempt.Decision) (attempt.ScheduleInput, error) {
			return attempt.ScheduleInput{}, nil
		},
		Next: func(ctx context.Context, previous Step[selectedModelCall], decision attempt.Decision) (Step[selectedModelCall], error) {
			if decision.Action != attempt.Fallback {
				t.Error("host fallback callback invoked without fallback decision")
			}
			return selectModelFallback(ctx, previous, selector, bad, good)
		},
		Now:                 func() time.Time { return now },
		NestedAttemptsKnown: true,
	}
	// Act: Next explicitly reselects; Boundary validates the new pinned candidate again.
	result, err := sequence.Run(t.Context(), coordinator, Step[selectedModelCall]{
		Identity: id,
		Request: selectedModelCall{
			Evaluation: evaluation,
			Selection:  selected,
			Candidate:  initial,
			Affinity:   affinity,
		},
	})
	// Assert: neither preference nor lower cost/greater rank bypasses hard eligibility.
	if err != nil || result.Last.Route.Payload != "C" || calls["A"] != 1 || calls["B"] != 0 || calls["C"] != 1 ||
		ranked["B"] != 0 || len(result.Trace) != 2 || result.Trace[0].Identity == result.Trace[1].Identity {
		t.Fatalf("calls=%v ranked=%v payload=%s err=%v", calls, ranked, result.Last.Route.Payload, err)
	}
}

func fallbackCandidate(key string, now time.Time) fallbackModelCandidate {
	return fallbackModelCandidate{Key: key, Scope: "trusted", Route: routery.RouteID(key), Fingerprint: key + "-facts",
		Descriptor: model.Descriptor[string]{Capabilities: map[string]bool{"schema": true},
			ContextWindow: 100, Residency: "allowed", RetainsData: new(false), FreshUntil: now.Add(time.Hour)}}
}

func selectModelFallback(
	ctx context.Context, previous Step[selectedModelCall],
	selector policy.Selector[model.Request[string], string, string, model.Descriptor[string], model.Reason],
	bad, good fallbackModelCandidate,
) (Step[selectedModelCall], error) {
	evaluation := previous.Request.Evaluation
	evaluation.References.Candidates = "fallback-current"
	affinity := fallbackModelAffinity{Strength: policy.Preferred, Key: bad.Key,
		Scope: "trusted", TrustedScope: "trusted", ScopePresent: true}
	selected, err := selector.Select(ctx, evaluation, []fallbackModelCandidate{bad, good}, affinity)
	if err != nil {
		return Step[selectedModelCall]{}, err
	}
	if selected.Status != policy.Selected || selected.Affinity != policy.PreferenceBypassed {
		return Step[selectedModelCall]{}, policy.ErrInvalidSelection
	}
	return Step[selectedModelCall]{Identity: attempt.Identity{
		Operation: previous.Identity.Operation, Attempt: "fallback-physical",
	}, Request: selectedModelCall{Evaluation: evaluation, Selection: selected, Candidate: good, Affinity: affinity}}, nil
}
