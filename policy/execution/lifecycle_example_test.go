package execution_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/skosovsky/routery"
	"github.com/skosovsky/routery/policy"
	"github.com/skosovsky/routery/policy/attempt"
	"github.com/skosovsky/routery/policy/execution"
	"github.com/skosovsky/routery/policy/model"
	"github.com/skosovsky/routery/policy/quota"
)

type analysisRequest struct{ Text string }
type analysisKind uint8
type analysisReason uint8
type analysisOutput struct{ Characters int }

func ExampleBoundary_modelValueLifecycle() {
	// Arrange: minimal model projection, independent caller types, no storage/SDK/OTel/stream.
	now := time.Unix(100, 0)
	evaluation := policy.Evaluation[model.Request[string]]{
		Input:      model.Request[string]{Policy: "analysis", Task: "count", Required: []string{"text"}},
		Now:        now,
		References: policy.References{Input: "facts", Candidates: "endpoints", Policy: "analysis"},
	}
	candidate := policy.Candidate[string, string, model.Descriptor[string]]{
		Key:         "trusted-endpoint",
		Scope:       "account",
		Route:       "analysis",
		Fingerprint: "revision",
		Descriptor: model.Descriptor[string]{
			Capabilities: map[string]bool{"text": true},
			RetainsData:  new(false),
			FreshUntil:   now.Add(time.Hour),
		},
	}
	selector := model.Selector(
		model.Config{Policy: "analysis", Optional: model.IgnoreOptional},
		func(policy.Evaluation[model.Request[string]], policy.Candidate[string, string, model.Descriptor[string]]) (float64, error) {
			return 1, nil
		},
	)
	affinity := policy.Affinity[string, string, model.Descriptor[string]]{}
	selected, selectErr := selector.Select(
		context.Background(),
		evaluation,
		[]policy.Candidate[string, string, model.Descriptor[string]]{candidate},
		affinity,
	)
	if selectErr != nil || selected.Status != policy.Selected {
		fmt.Println("selection unavailable")
		return
	}
	boundary := execution.Boundary[analysisRequest, analysisKind, analysisReason, analysisOutput]{
		Fresh: func(ctx context.Context, _ analysisRequest) error {
			return selector.ValidatePinned(ctx, evaluation, selected, candidate, affinity)
		},
		Dispatch: func(call routery.RouteCall[analysisRequest], receipt *execution.Receipt) (routery.RouteResult[analysisKind, analysisReason, analysisOutput], error) {
			event, _, err := receipt.Snapshot()
			if err != nil {
				return routery.RouteResult[analysisKind, analysisReason, analysisOutput]{}, err
			}
			// Explicit terminal fixture report, not a deduction from nil error.
			event.Phase, event.Outcome = attempt.Terminal, attempt.Completed
			return routery.Handled(
					analysisKind(1),
					analysisReason(1),
					analysisOutput{Characters: len(call.Request.Text)},
				), receipt.Record(
					event,
				)
		},
	}
	coordinator, _ := attempt.NewCoordinator("analysis", 1)
	// Act.
	result, err := boundary.Run(
		routery.NewRouteCall(context.Background(), analysisRequest{Text: "inventory"}),
		coordinator,
		attempt.Identity{Operation: "analysis", Attempt: "one"},
	)
	closeErr := result.Route.Lifetime.Close() // Canonical owner, also on error (nil for values).
	if err != nil || closeErr != nil {
		fmt.Println(errors.Join(err, closeErr))
		return
	}
	event, _, receiptErr := result.Receipt.Snapshot()
	// Assert.
	fmt.Println(
		"characters",
		result.Route.Payload.Characters,
		"completed",
		event.Outcome == attempt.Completed,
		"settlement",
		receiptErr,
	)
	// Output: characters 9 completed true settlement <nil>
}

type assetRequest struct{ Path string }
type assetFacts struct{ Enabled bool }
type assetKind string
type assetReason string
type assetStream struct {
	io.Reader

	closes int
}

func (stream *assetStream) Close() error { stream.closes++; return nil }

type assetFailure uint8

const assetTemporary assetFailure = 1

func ExampleSequence_externalResourceLifecycle() {
	// Arrange: unrelated external-read types and explicit host-owned quota evidence.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request := assetRequest{Path: "private-path"}
	evaluation := policy.Evaluation[assetRequest]{
		Input:      request,
		Now:        time.Unix(100, 0),
		References: policy.References{Input: "request-facts", Candidates: "storage-facts", Policy: "read"},
	}
	candidate := policy.Candidate[int, int, assetFacts]{
		Key:         7,
		Scope:       42,
		Route:       "read",
		Fingerprint: "storage-revision",
		Descriptor:  assetFacts{Enabled: true},
	}
	selector := policy.Selector[assetRequest, int, int, assetFacts, bool]{
		Freeze: func(facts assetFacts) assetFacts { return facts },
		Eligible: func(_ policy.Evaluation[assetRequest], candidate policy.Candidate[int, int, assetFacts]) (policy.Eligibility[bool], error) {
			return policy.Eligibility[bool]{Allowed: candidate.Descriptor.Enabled, Reason: true}, nil
		},
		Rank: func(policy.Evaluation[assetRequest], policy.Candidate[int, int, assetFacts]) (float64, error) {
			return 1, nil
		},
	}
	affinity := policy.Affinity[int, int, assetFacts]{}
	selected, selectionErr := selector.Select(
		ctx,
		evaluation,
		[]policy.Candidate[int, int, assetFacts]{candidate},
		affinity,
	)
	if selectionErr != nil || selected.Status != policy.Selected {
		fmt.Println("selection unavailable")
		return
	}
	id := attempt.Identity{Operation: "asset-read", Attempt: "asset-1"}
	reservationRequest := quota.ReserveRequest[string, string]{
		Scope:       "trusted",
		Identity:    id,
		Estimated:   map[string]uint64{"bytes": 2},
		Fingerprint: "asset-quota",
	}
	backend := &singleReservationBackend{request: reservationRequest}
	var session *quota.Session[string, string, string, string]
	actual, complete := uint64(2), false
	loseAck := true
	ackFailure := errors.New("lost settlement acknowledgment")
	cleanupBounded := false
	base := routery.RouteHandler[assetRequest, assetKind, assetReason, *assetStream](
		func(routery.RouteCall[assetRequest]) (routery.RouteResult[assetKind, assetReason, *assetStream], error) {
			body := &assetStream{Reader: strings.NewReader("partial")}
			result := routery.Handled(assetKind("opened"), assetReason("buffered"), body)
			result.Lifetime = routery.NewLifetime(body.Close)
			return result, io.ErrUnexpectedEOF
		},
	)
	limited := routery.ApplyRoute(base, routery.Bulkhead[assetRequest, assetKind, assetReason, *assetStream](1))
	boundary := execution.Boundary[assetRequest, assetKind, assetReason, *assetStream]{
		Fresh: func(ctx context.Context, _ assetRequest) error {
			return selector.ValidatePinned(ctx, evaluation, selected, candidate, affinity)
		},
		Admit: func(ctx context.Context, _ assetRequest, identity attempt.Identity) (execution.Admission, error) {
			reservationRequest.Identity = identity
			reservation, owned, err := quota.Admit(ctx, backend, reservationRequest, quota.FailClosed)
			session = owned
			admission := execution.Admission{Status: reservation.Admission, RetryAt: reservation.RetryAt}
			if err == nil && session != nil {
				admission.Finish = func(ctx context.Context, _ attempt.Event) error {
					_, bounded := ctx.Deadline()
					cleanupBounded = bounded && ctx.Err() == nil
					settleErr := session.Settle(ctx, "asset-usage", map[string]uint64{"bytes": actual}, complete)
					if loseAck {
						loseAck = false
						return errors.Join(settleErr, ackFailure)
					}
					return settleErr
				}
			}
			return admission, err
		},
		CleanupContext: func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), time.Second)
		},
		Dispatch: func(call routery.RouteCall[assetRequest], receipt *execution.Receipt) (routery.RouteResult[assetKind, assetReason, *assetStream], error) {
			result, err := limited(call)
			event, _, snapshotErr := receipt.Snapshot()
			if snapshotErr != nil {
				return result, errors.Join(err, snapshotErr)
			}
			event.Phase = attempt.StreamOpened
			return result, errors.Join(err, receipt.Record(event))
		},
	}
	sequence := execution.Sequence[assetRequest, assetFailure, assetKind, assetReason, *assetStream]{
		Boundary: boundary,
		Classify: func(execution.Result[assetKind, assetReason, *assetStream], error) (assetFailure, error) {
			return assetTemporary, nil
		},
		Replay: func(execution.Failure[assetFailure]) (attempt.Replay, error) {
			return attempt.Replay{Retryable: true, Replayable: true, CanReconcile: true}, nil
		},
		Schedule: func(execution.Failure[assetFailure], attempt.Decision) (attempt.ScheduleInput, error) {
			return attempt.ScheduleInput{Now: time.Now()}, nil
		},
		Next: func(_ context.Context, previous execution.Step[assetRequest], _ attempt.Decision) (execution.Step[assetRequest], error) {
			previous.Identity.Attempt += "/next"
			return previous, nil
		},
		Now:                 time.Now,
		NestedAttemptsKnown: false, // Host has not instrumented provider internals: invocation budget, not a wire-attempt guarantee.
	}
	coordinator, _ := attempt.NewCoordinator(id.Operation, 2)
	// Act: unknown outcome cannot repeat; partial owner and original error are retained.
	result, runErr := sequence.Run(ctx, coordinator, execution.Step[assetRequest]{Request: request, Identity: id})
	if runErr != nil {
		fmt.Println(errors.Join(runErr, result.Last.Route.Lifetime.Close()))
		return
	}
	_, blockedErr := routery.InvokeRouteHandler(context.Background(), request, limited)
	event, _, snapshotErr := result.Last.Receipt.Snapshot()
	if snapshotErr != nil {
		fmt.Println(errors.Join(snapshotErr, result.Last.Route.Lifetime.Close()))
		return
	}
	// Caller publishes the partial data explicitly; a new repeat would require reset evidence.
	event.Committed = true
	if recordErr := result.Last.Receipt.Record(event); recordErr != nil {
		fmt.Println(errors.Join(recordErr, result.Last.Route.Lifetime.Close()))
		return
	}
	cancel() // Cleanup still uses a separate bounded context, not this canceled attempt.
	closeErr := result.Last.Route.Lifetime.Close()
	_, _, settlementErr := result.Last.Receipt.Snapshot()
	fmt.Println(
		"partial-error",
		errors.Is(result.Failure.Err, io.ErrUnexpectedEOF),
		"reconcile",
		result.Decision.Action == attempt.Reconcile,
		"permit-held",
		errors.Is(blockedErr, routery.ErrBulkheadFull),
	)
	fmt.Println(
		"close-error", closeErr,
		"settlement-ack-lost",
		errors.Is(settlementErr, ackFailure),
		"pending",
		session.State() == quota.Pending,
		"bounded-cleanup",
		cleanupBounded,
	)
	// Application retries the stable settlement identity; it does not call Lifetime.Close to retry cleanup.
	if reconcileErr := result.Last.Receipt.Reconcile(); reconcileErr != nil {
		fmt.Println(reconcileErr)
		return
	}
	actual, complete = 7, true // A later authoritative usage report, not inferred from Close or cancellation.
	event.Phase, event.Outcome = attempt.Terminal, attempt.Completed
	if recordErr := result.Last.Receipt.Record(event); recordErr != nil {
		fmt.Println(recordErr)
		return
	}
	retryCloseErr := result.Last.Route.Lifetime.Close() // Close is cached; settlement errors are separately retained in Receipt.
	next, nextErr := routery.InvokeRouteHandler(context.Background(), request, limited)
	nextCloseErr := next.Lifetime.Close()
	// Assert: late completion settles full usage, original owner closes once, permit is reusable.
	fmt.Println(
		"completed",
		session.State() == quota.Committed,
		"usage",
		backend.settlement.Actual["bytes"],
		"closes",
		result.Last.Route.Payload.closes,
	)
	fmt.Println(
		"permit-reusable",
		errors.Is(nextErr, io.ErrUnexpectedEOF),
		"next-close",
		nextCloseErr,
		"repeat-close",
		retryCloseErr,
		"nested-accounted",
		result.NestedAttemptsKnown,
	)
	// Output:
	// partial-error true reconcile true permit-held true
	// close-error <nil> settlement-ack-lost true pending true bounded-cleanup true
	// completed true usage 7 closes 1
	// permit-reusable true next-close <nil> repeat-close <nil> nested-accounted false
}
