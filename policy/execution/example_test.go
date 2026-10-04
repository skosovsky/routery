package execution_test

import (
	"context"
	"fmt"
	"time"

	"github.com/skosovsky/routery"
	"github.com/skosovsky/routery/policy"
	"github.com/skosovsky/routery/policy/attempt"
	"github.com/skosovsky/routery/policy/execution"
	"github.com/skosovsky/routery/policy/model"
)

type inferenceJob struct {
	Text string
}

func ExampleBoundary_modelExecution() {
	// Arrange: host-owned payload, capability facts and a deterministic clock.
	ctx := context.Background()
	now := time.Unix(100, 0)
	job := inferenceJob{Text: "private input"}
	evaluation := policy.Evaluation[model.Request[string]]{
		Input:      model.Request[string]{Policy: "schema-policy", Required: []string{"schema"}},
		Now:        now,
		References: policy.References{Input: "job-facts", Candidates: "endpoint-facts", Policy: "schema-policy"},
	}
	candidate := policy.Candidate[string, string, model.Descriptor[string]]{
		Key: "schema-endpoint", Scope: "trusted-account", Route: "inference", Fingerprint: "schema-facts",
		Descriptor: model.Descriptor[string]{Capabilities: map[string]bool{"schema": true},
			RetainsData: new(false), FreshUntil: now.Add(time.Hour)},
	}
	selector := model.Selector(
		model.Config{Policy: "schema-policy", Optional: model.IgnoreOptional},
		func(policy.Evaluation[model.Request[string]], policy.Candidate[string, string, model.Descriptor[string]]) (float64, error) {
			return 1, nil // Host ranking, not a measured quality claim.
		},
	)
	affinity := policy.Affinity[string, string, model.Descriptor[string]]{}
	selected, err := selector.Select(ctx, evaluation,
		[]policy.Candidate[string, string, model.Descriptor[string]]{candidate}, affinity)
	if err != nil || selected.Status != policy.Selected {
		fmt.Println("selection unavailable")
		return
	}
	coordinator, err := attempt.NewCoordinator("inference-operation", 1)
	if err != nil {
		fmt.Println(err)
		return
	}
	boundary := execution.Boundary[inferenceJob, routery.BasicKind, routery.BasicReason, string]{
		Fresh: func(ctx context.Context, _ inferenceJob) error {
			// Production host obtains current facts/clock here; never silently reselect.
			evaluation.Now = now
			return selector.ValidatePinned(ctx, evaluation, selected, candidate, affinity)
		},
		Dispatch: func(call routery.RouteCall[inferenceJob], receipt *execution.Receipt) (routery.BasicRouteResult[string], error) {
			event, _, snapshotErr := receipt.Snapshot()
			if snapshotErr != nil {
				return routery.BasicRouteResult[string]{}, snapshotErr
			}
			// Controlled provider fixture: a terminal report explicitly proves completion.
			// Real adapters must not infer this fact from HTTP headers or nil error.
			event.Phase, event.Outcome = attempt.Terminal, attempt.Completed
			return routery.BasicHandled(fmt.Sprintf("%s: %d bytes", selected.Binding.Branch, len(call.Request.Text))),
				receipt.Record(event)
		},
	}
	// Act: no SDK, quota store, transcript, telemetry or streaming is required.
	result, err := boundary.Run(routery.NewRouteCall(ctx, job), coordinator,
		attempt.Identity{Operation: "inference-operation", Attempt: "inference-1"})
	// Assert.
	fmt.Println(result.Route.Payload, err)
	// Output: schema-endpoint: 13 bytes <nil>
}

type stockQuery struct {
	Warehouse int
	SKU       int
}

type warehouseFacts struct {
	Warehouse int
	Healthy   bool
}

type warehouseReason uint8

const warehouseAvailable warehouseReason = 1

func ExampleBoundary_ordinaryOperation() {
	// Arrange: unrelated caller types and no model adapter.
	ctx := context.Background()
	request := stockQuery{Warehouse: 7, SKU: 42}
	evaluation := policy.Evaluation[stockQuery]{Input: request, Now: time.Unix(100, 0),
		References: policy.References{Input: "stock-query", Candidates: "warehouse-facts", Policy: "read-policy"}}
	candidate := policy.Candidate[int, int, warehouseFacts]{Key: 7, Scope: 7, Route: "stock-read",
		Fingerprint: "warehouse-7", Descriptor: warehouseFacts{Warehouse: 7, Healthy: true}}
	selector := policy.Selector[stockQuery, int, int, warehouseFacts, warehouseReason]{
		Freeze: func(facts warehouseFacts) warehouseFacts { return facts }, // Value-only immutable facts.
		Eligible: func(evaluation policy.Evaluation[stockQuery], candidate policy.Candidate[int, int, warehouseFacts]) (policy.Eligibility[warehouseReason], error) {
			return policy.Eligibility[warehouseReason]{
				Allowed: candidate.Descriptor.Healthy && candidate.Descriptor.Warehouse == evaluation.Input.Warehouse,
				Reason:  warehouseAvailable,
			}, nil
		},
		Rank: func(policy.Evaluation[stockQuery], policy.Candidate[int, int, warehouseFacts]) (float64, error) {
			return 1, nil
		},
	}
	affinity := policy.Affinity[int, int, warehouseFacts]{}
	selected, err := selector.Select(ctx, evaluation, []policy.Candidate[int, int, warehouseFacts]{candidate}, affinity)
	if err != nil || selected.Status != policy.Selected {
		fmt.Println("selection unavailable")
		return
	}
	coordinator, err := attempt.NewCoordinator("stock-operation", 1)
	if err != nil {
		fmt.Println(err)
		return
	}
	boundary := execution.Boundary[stockQuery, routery.BasicKind, routery.BasicReason, int]{
		Fresh: func(ctx context.Context, _ stockQuery) error {
			return selector.ValidatePinned(ctx, evaluation, selected, candidate, affinity)
		},
		Dispatch: func(call routery.RouteCall[stockQuery], receipt *execution.Receipt) (routery.BasicRouteResult[int], error) {
			event, _, snapshotErr := receipt.Snapshot()
			if snapshotErr != nil {
				return routery.BasicRouteResult[int]{}, snapshotErr
			}
			// Controlled external-read fixture with explicit terminal proof.
			event.Phase, event.Outcome = attempt.Terminal, attempt.Completed
			return routery.BasicHandled(call.Request.SKU + selected.Binding.Branch), receipt.Record(event)
		},
	}
	// Act.
	result, err := boundary.Run(routery.NewRouteCall(ctx, request), coordinator,
		attempt.Identity{Operation: "stock-operation", Attempt: "stock-read-1"})
	// Assert.
	fmt.Println(result.Route.Payload, err)
	// Output: 49 <nil>
}
