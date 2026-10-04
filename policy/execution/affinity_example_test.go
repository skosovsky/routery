package execution_test

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/skosovsky/routery"
	"github.com/skosovsky/routery/policy"
	"github.com/skosovsky/routery/policy/attempt"
	"github.com/skosovsky/routery/policy/execution"
)

type continuationRequest struct {
	Tenant string
	State  string
}

type continuationEndpoint struct {
	Tenant  string
	Enabled bool
}

type continuationCandidate = policy.Candidate[string, string, continuationEndpoint]
type continuationAffinity = policy.Affinity[string, string, continuationEndpoint]
type continuationSelector = policy.Selector[continuationRequest, string, string, continuationEndpoint, bool]
type continuationSelection = policy.Selection[string, string, continuationEndpoint, bool]

func requiredContinuation(endpoint, state string) continuationAffinity {
	return continuationAffinity{Strength: policy.Required, Key: endpoint,
		Scope: "trusted", TrustedScope: "trusted", ScopePresent: true,
		Fingerprint: "compatibility-" + endpoint, StateFingerprint: state,
		Compatible: func(candidate continuationCandidate) bool {
			return candidate.Key == endpoint && candidate.Descriptor.Tenant == "trusted"
		}}
}

func continuationPolicy() continuationSelector {
	return continuationSelector{
		Freeze: func(facts continuationEndpoint) continuationEndpoint { return facts },
		Eligible: func(evaluation policy.Evaluation[continuationRequest], candidate continuationCandidate) (policy.Eligibility[bool], error) {
			allowed := candidate.Descriptor.Enabled && candidate.Descriptor.Tenant == evaluation.Input.Tenant
			return policy.Eligibility[bool]{Allowed: allowed, Reason: allowed}, nil
		},
		Rank: func(policy.Evaluation[continuationRequest], continuationCandidate) (float64, error) { return 1, nil },
	}
}

// authorizeContinuation is host application code, not a library rebuild service.
// Nil approval cannot silently convert required state into a stateless request.
func authorizeContinuation(ctx context.Context, original continuationAffinity,
	authorize func(context.Context) (continuationRequest, continuationAffinity, error),
) (continuationRequest, continuationAffinity, error) {
	if !original.ScopePresent || original.Scope != original.TrustedScope {
		return continuationRequest{}, continuationAffinity{}, policy.ErrAffinityScope
	}
	if authorize == nil {
		return continuationRequest{}, continuationAffinity{}, execution.ErrInvalidBoundary
	}
	return authorize(ctx)
}

func dispatchContinuation(ctx context.Context, coordinator *attempt.Coordinator, selector continuationSelector,
	evaluation policy.Evaluation[continuationRequest], selected continuationSelection,
	candidate continuationCandidate, affinity continuationAffinity, identity attempt.Identity,
	usage map[attempt.Identity]uint64, measured uint64,
) error {
	boundary := execution.Boundary[continuationRequest, routery.BasicKind, routery.BasicReason, string]{
		Fresh: func(ctx context.Context, _ continuationRequest) error {
			// The host would load current descriptor/clock here in production.
			return selector.ValidatePinned(ctx, evaluation, selected, candidate, affinity)
		},
		Dispatch: func(call routery.RouteCall[continuationRequest], receipt *execution.Receipt) (routery.BasicRouteResult[string], error) {
			event, _, err := receipt.Snapshot()
			if err != nil {
				return routery.BasicRouteResult[string]{}, err
			}
			// Controlled provider fixture explicitly reports completion and usage.
			// Production must obtain these facts from its provider, not a nil error.
			usage[identity] = measured
			event.Phase, event.Outcome = attempt.Terminal, attempt.Completed
			return routery.BasicHandled(call.Request.State), receipt.Record(event)
		},
	}
	result, err := boundary.Run(routery.NewRouteCall(ctx, evaluation.Input), coordinator, identity)
	if err != nil || !result.Started {
		return errors.Join(err, execution.ErrInvalidBoundary)
	}
	return nil
}

func ExampleBoundary_authorizedAffinityRebuild() {
	// Arrange: host-owned opaque state, history, trusted scope and separate usage.
	ctx := context.Background()
	selector := continuationPolicy()
	initialID := attempt.Identity{Operation: "continuation-operation", Attempt: "physical-A"}
	coordinator, err := attempt.NewCoordinator(initialID.Operation, 2)
	if err != nil {
		fmt.Println(err)
		return
	}
	evaluation := policy.Evaluation[continuationRequest]{
		Input: continuationRequest{Tenant: "trusted", State: "opaque-A"},
		Now: time.Unix(
			100,
			0,
		),
		References: policy.References{Input: "state-A", Candidates: "endpoints-A", Policy: "hard-policy"},
	}
	affinity := requiredContinuation("A", "state-A")
	endpoint := continuationCandidate{Key: "A", Scope: "trusted", Route: "A", Fingerprint: "A-facts",
		Descriptor: continuationEndpoint{Tenant: "trusted", Enabled: true}}
	selected, err := selector.Select(ctx, evaluation, []continuationCandidate{endpoint}, affinity)
	if err != nil || selected.Status != policy.Selected {
		fmt.Println("initial selection unavailable")
		return
	}
	usage := make(map[attempt.Identity]uint64)
	if err = dispatchContinuation(
		ctx,
		coordinator,
		selector,
		evaluation,
		selected,
		endpoint,
		affinity,
		initialID,
		usage,
		3,
	); err != nil {
		fmt.Println(err)
		return
	}
	// A is now unavailable. Matching a healthy alternative does not make A's state portable.
	endpoint.Key, endpoint.Route, endpoint.Fingerprint = "B", "B", "B-facts"
	evaluation.References.Candidates = "endpoints-B"
	unavailable, err := selector.Select(ctx, evaluation, []continuationCandidate{endpoint}, affinity)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("required unavailable", unavailable.Status == policy.AffinityUnavailable, "calls", len(usage))
	_, _, denied := authorizeContinuation(ctx, affinity, nil)
	fmt.Println("unauthorized", errors.Is(denied, execution.ErrInvalidBoundary), "calls", len(usage))
	// Act: a host explicitly approves reconstruction from its own retained history.
	rebuiltRequest, rebuiltAffinity, err := authorizeContinuation(ctx, affinity,
		func(ctx context.Context) (continuationRequest, continuationAffinity, error) {
			return continuationRequest{Tenant: "trusted", State: "host-history-reconstructed-for-B"},
				requiredContinuation("B", "rebuilt-state-B"), ctx.Err()
		})
	if err != nil {
		fmt.Println(err)
		return
	}
	evaluation.Input, evaluation.References.Input = rebuiltRequest, rebuiltAffinity.StateFingerprint
	selected, err = selector.Select(ctx, evaluation, []continuationCandidate{endpoint}, rebuiltAffinity)
	if err != nil || selected.Status != policy.Selected {
		fmt.Println("rebuilt selection unavailable")
		return
	}
	rebuiltID := attempt.Identity{Operation: initialID.Operation, Attempt: "physical-B"}
	lineage := map[attempt.Identity]attempt.Identity{rebuiltID: initialID} // Host-owned, never inferred by core.
	if err = dispatchContinuation(
		ctx,
		coordinator,
		selector,
		evaluation,
		selected,
		endpoint,
		rebuiltAffinity,
		rebuiltID,
		usage,
		5,
	); err != nil {
		fmt.Println(err)
		return
	}
	// Assert: original accounting remains; rebuilt execution has its own identity and charge.
	fmt.Println("rebuilt", selected.Binding.Branch, "parent", lineage[rebuiltID].Attempt,
		"calls", len(usage), "usage", usage[initialID]+usage[rebuiltID])
	// Output:
	// required unavailable true calls 1
	// unauthorized true calls 1
	// rebuilt B parent physical-A calls 2 usage 8
}
