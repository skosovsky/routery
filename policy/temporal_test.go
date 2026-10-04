package policy

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/skosovsky/routery"
)

func TestPinnedSelectionRejectsAffinityAndIdentityChanges(t *testing.T) {
	for _, name := range []string{"expiry", "weakened", "scope", "fingerprint", "compatibility"} {
		t.Run(name, func(t *testing.T) {
			// Arrange.
			evaluation := fixtureEvaluation()
			selector := fixtureSelector()
			candidate := fixtureCandidates()[1]
			affinity := Affinity[string, string, descriptorFacts]{
				Strength: Required, Key: candidate.Key, Scope: candidate.Scope,
				TrustedScope: candidate.Scope, ScopePresent: true,
				Fingerprint: "affinity", StateFingerprint: "state",
				Expires:    evaluation.Now.Add(time.Minute),
				Compatible: func(Candidate[string, string, descriptorFacts]) bool { return true },
			}
			selected, err := selector.Select(t.Context(), evaluation,
				[]Candidate[string, string, descriptorFacts]{candidate}, affinity)
			if err != nil {
				t.Fatal(err)
			}
			switch name {
			case "expiry":
				evaluation.Now = affinity.Expires
			case "weakened":
				affinity.Strength = None
			case "scope":
				candidate.Scope = "other"
			case "fingerprint":
				candidate.Fingerprint = "changed"
			case "compatibility":
				affinity.Compatible = func(Candidate[string, string, descriptorFacts]) bool { return false }
			}
			calls := 0
			// Act.
			_, dispatchErr := Dispatch(
				t.Context(),
				evaluation.Input,
				selected,
				evaluation.References,
				func(ctx context.Context, selection Selection[string, string, descriptorFacts, decisionReason]) error {
					return selector.ValidatePinned(ctx, evaluation, selection, candidate, affinity)
				},
				func(context.Context, requestFacts, routery.RouteBinding[string, Candidate[string, string, descriptorFacts]]) (int, error) {
					calls++
					return 1, nil
				},
			)
			// Assert.
			if calls != 0 || (!errors.Is(dispatchErr, routery.ErrStaleSnapshot) &&
				!errors.Is(dispatchErr, ErrAffinityScope)) {
				t.Fatalf("calls=%d err=%v", calls, dispatchErr)
			}
		})
	}
}

func TestDispatchRequiresValidatorAndRechecksCancellation(t *testing.T) {
	// Arrange.
	evaluation := fixtureEvaluation()
	selected, err := fixtureSelector().Select(t.Context(), evaluation, fixtureCandidates(),
		Affinity[string, string, descriptorFacts]{})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	dispatch := func(context.Context, requestFacts, routery.RouteBinding[string, Candidate[string, string, descriptorFacts]]) (int, error) {
		calls++
		return 1, nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	// Act.
	_, missing := Dispatch(ctx, evaluation.Input, selected, evaluation.References, nil, dispatch)
	_, cancelled := Dispatch(ctx, evaluation.Input, selected, evaluation.References,
		func(context.Context, Selection[string, string, descriptorFacts, decisionReason]) error {
			cancel()
			return nil
		}, dispatch)
	// Assert.
	if calls != 0 || !errors.Is(missing, ErrInvalidSelection) || !errors.Is(cancelled, context.Canceled) {
		t.Fatalf("calls=%d missing=%v cancelled=%v", calls, missing, cancelled)
	}
}
