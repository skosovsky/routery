package execution

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/skosovsky/routery"
	"github.com/skosovsky/routery/policy"
	"github.com/skosovsky/routery/policy/attempt"
	"github.com/skosovsky/routery/policy/quota"
)

type rebuildFacts struct {
	Tenant  string
	Enabled bool
}

type rebuildSelection = policy.Selection[string, string, rebuildFacts, bool]
type rebuildAffinity = policy.Affinity[string, string, rebuildFacts]
type rebuildCandidate = policy.Candidate[string, string, rebuildFacts]

// This is host application code, not a routery reconstruction service.
// A missing authorization callback must never turn required state into stateless input.
func hostRebuild(
	ctx context.Context, original rebuildAffinity,
	authorize func(context.Context) (rebuildAffinity, error),
) (rebuildAffinity, error) {
	if original.Scope != original.TrustedScope || !original.ScopePresent {
		return rebuildAffinity{}, policy.ErrAffinityScope
	}
	if authorize == nil {
		return rebuildAffinity{}, ErrInvalidBoundary
	}
	return authorize(ctx)
}

func rebuildSelector() policy.Selector[string, string, string, rebuildFacts, bool] {
	return policy.Selector[string, string, string, rebuildFacts, bool]{
		Freeze: func(facts rebuildFacts) rebuildFacts { return facts },
		Eligible: func(evaluation policy.Evaluation[string], candidate rebuildCandidate) (policy.Eligibility[bool], error) {
			allowed := candidate.Descriptor.Enabled && candidate.Descriptor.Tenant == evaluation.Input
			return policy.Eligibility[bool]{Allowed: allowed, Reason: allowed}, nil
		},
		Rank: func(policy.Evaluation[string], rebuildCandidate) (float64, error) { return 1, nil },
	}
}

func TestHostRebuildRequiresAuthorizationFreshEligibilityAndSeparateAccounting(t *testing.T) {
	for _, name := range []string{"authorized", "denied", "foreign scope", "ineligible"} {
		t.Run(name, func(t *testing.T) { checkHostRebuild(t, name) })
	}
}

func checkHostRebuild(t *testing.T, name string) {
	t.Helper()
	// Arrange: the original completed attempt has already incurred five measured units.
	coordinator, initialID := setup(t)
	store := &quotaFixture{states: make(map[string]quota.State)}
	dispatches := 0
	boundary := testBoundary{
		Fresh: func(context.Context, string) error { return nil },
		Admit: store.admit, CleanupContext: cleanupContext,
		Dispatch: func(call routery.RouteCall[string], receipt *Receipt) (routery.BasicRouteResult[string], error) {
			dispatches++
			event, _, err := receipt.Snapshot()
			if err != nil {
				return routery.BasicRouteResult[string]{}, err
			}
			event.Phase, event.Outcome = attempt.Terminal, attempt.Completed
			return routery.BasicHandled(call.Request), receipt.Record(event)
		},
	}
	if _, err := boundary.Run(routery.NewRouteCall(t.Context(), "A"), coordinator, initialID); err != nil {
		t.Fatal(err)
	}
	evaluation := policy.Evaluation[string]{Input: "trusted", Now: time.Unix(100, 0),
		References: policy.References{Input: "state-A", Candidates: "current", Policy: "hard-policy"}}
	candidate := rebuildCandidate{Key: "B", Scope: "trusted", Route: "B", Fingerprint: "B-facts",
		Descriptor: rebuildFacts{Tenant: "trusted", Enabled: name != "ineligible"}}
	original := rebuildAffinity{Strength: policy.Required, Key: "A", Scope: "trusted", TrustedScope: "trusted",
		ScopePresent: true, Fingerprint: "affinity-A", StateFingerprint: "state-A",
		Compatible: func(candidate rebuildCandidate) bool { return candidate.Key == "A" }}
	selector := rebuildSelector()
	unavailable, err := selector.Select(t.Context(), evaluation, []rebuildCandidate{candidate}, original)
	if err != nil || unavailable.Status != policy.AffinityUnavailable {
		t.Fatalf("required state silently moved: selection=%v err=%v", unavailable.Status, err)
	}
	transforms := 0
	rebuiltState := ""
	authorize := func(ctx context.Context) (rebuildAffinity, error) {
		transforms++
		// Controlled host transformation from its own history; core sees only the request.
		rebuiltState = "host-reconstructed-history-for-B"
		return rebuildAffinity{Strength: policy.Required, Key: "B", Scope: "trusted", TrustedScope: "trusted",
			ScopePresent: true, Fingerprint: "affinity-B", StateFingerprint: "rebuilt-state-B",
			Compatible: func(candidate rebuildCandidate) bool {
				return candidate.Key == "B" && candidate.Scope == "trusted"
			}}, ctx.Err()
	}
	if name == "denied" {
		authorize = nil
	}
	if name == "foreign scope" {
		original.TrustedScope = "other"
	}
	// Act: only the host performs the authorized transformation and declares lineage.
	rebuilt, rebuildErr := hostRebuild(t.Context(), original, authorize)
	if rebuildErr != nil {
		assertHostRebuildDenied(t, name, rebuildErr, transforms, dispatches, store)
		return
	}
	evaluation.References.Input = rebuilt.StateFingerprint
	selected, err := selector.Select(t.Context(), evaluation, []rebuildCandidate{candidate}, rebuilt)
	if err != nil {
		t.Fatal(err)
	}
	if name == "ineligible" {
		if selected.Status != policy.AffinityUnavailable || dispatches != 1 || store.actual != 5 {
			t.Fatal("rebuild bypassed eligibility")
		}
		return
	}
	checkRebuiltDispatch(
		t,
		coordinator,
		initialID,
		boundary,
		selector,
		evaluation,
		selected,
		candidate,
		rebuilt,
		rebuiltState,
		store,
	)
	// Assert: original accounting is retained; rebuilt execution is separately charged.
	if transforms != 1 || dispatches != 2 || store.actual != 10 || len(store.states) != 2 {
		t.Fatalf("transforms=%d dispatches=%d usage=%d", transforms, dispatches, store.actual)
	}
}

func assertHostRebuildDenied(t *testing.T, name string, err error, transforms, dispatches int, store *quotaFixture) {
	t.Helper()
	want := ErrInvalidBoundary
	if name == "foreign scope" {
		want = policy.ErrAffinityScope
	}
	if !errors.Is(err, want) || transforms != 0 || dispatches != 1 || store.actual != 5 {
		t.Fatalf("unauthorized transformation: err=%v transforms=%d dispatches=%d", err, transforms, dispatches)
	}
}

func checkRebuiltDispatch(
	t *testing.T, coordinator *attempt.Coordinator, parent attempt.Identity, boundary testBoundary,
	selector policy.Selector[string, string, string, rebuildFacts, bool], evaluation policy.Evaluation[string],
	selected rebuildSelection, candidate rebuildCandidate, affinity rebuildAffinity, state string, store *quotaFixture,
) {
	t.Helper()
	// Host-owned lineage; neither state reconstruction nor this mapping lives in core.
	id := attempt.Identity{Operation: parent.Operation, Attempt: "rebuilt-physical"}
	lineage := map[attempt.Identity]attempt.Identity{id: parent}
	boundary.Fresh = func(ctx context.Context, _ string) error {
		return selector.ValidatePinned(ctx, evaluation, selected, candidate, affinity)
	}
	result, err := boundary.Run(routery.NewRouteCall(t.Context(), state), coordinator, id)
	if err != nil || !result.Started || state == "" || result.Route.Payload != state ||
		selected.Binding.Branch != "B" || lineage[id] != parent ||
		id == parent || evaluation.References.Input == "state-A" || store.actual != 10 {
		t.Fatalf("rebuild identity/freshness/accounting: err=%v started=%v", err, result.Started)
	}
}
