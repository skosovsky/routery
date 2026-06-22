package routery

import (
	"errors"
	"testing"
)

type projectedResult struct {
	Action      RouteAction
	RouteID     RouteID
	PayloadType string
	FailedStage ErrorStage
}

func TestOutcomeProjectorReceivesCanonicalRouteResult(t *testing.T) {
	// Arrange.
	router, err := NewRouteTable[routeRequest, testKind, testReason, string]().
		Route("projected", 1, nil, func(RouteCall[routeRequest]) (RouteResult[testKind, testReason, string], error) {
			return Handled(testKindHandled, testReasonHandled, "ok"), nil
		}).
		Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	projector := OutcomeProjectorFunc[testKind, testReason, string, projectedResult](
		func(result RouteResult[testKind, testReason, string]) (
			projectedResult,
			ProjectionMeta[testKind, testReason],
			error,
		) {
			meta := DefaultProjectionMeta(result)
			return projectedResult{
				Action:      meta.Action,
				RouteID:     meta.Match.RouteID,
				PayloadType: meta.Payload.Type,
			}, meta, nil
		},
	)

	// Act.
	projection, meta, err := DispatchAndProject(t.Context(), router, routeRequest{}, projector, nil)

	// Assert.
	if err != nil {
		t.Fatalf("DispatchAndProject() error = %v", err)
	}
	if projection.Action != ActionStop || projection.RouteID != "projected" {
		t.Fatalf("projection = %#v, want stop projected", projection)
	}
	if projection.PayloadType != "string" {
		t.Fatalf("PayloadType = %q, want string", projection.PayloadType)
	}
	if meta.Kind != testKindHandled || meta.Reason != testReasonHandled || !meta.Payload.HasPayload {
		t.Fatalf("meta = %#v, want typed result metadata", meta)
	}
}

func TestErrorPolicyMapsProjectionError(t *testing.T) {
	// Arrange.
	projectErr := errors.New("project")
	router, err := NewRouteTable[routeRequest, testKind, testReason, string]().
		Route("projected", 1, nil, func(RouteCall[routeRequest]) (RouteResult[testKind, testReason, string], error) {
			return Handled(testKindHandled, testReasonHandled, "ok"), nil
		}).
		Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	projector := OutcomeProjectorFunc[testKind, testReason, string, projectedResult](
		func(result RouteResult[testKind, testReason, string]) (
			projectedResult,
			ProjectionMeta[testKind, testReason],
			error,
		) {
			return projectedResult{}, DefaultProjectionMeta(result), projectErr
		},
	)
	policy := ErrorPolicyFunc[testKind, testReason, string, projectedResult](
		func(routeErr RouteError[testKind, testReason, string]) (projectedResult, error) {
			if !errors.Is(routeErr.Err, projectErr) {
				t.Fatalf("routeErr.Err = %v, want %v", routeErr.Err, projectErr)
			}
			return projectedResult{
				Action:      routeErr.Result.Action,
				RouteID:     routeErr.Match.RouteID,
				FailedStage: routeErr.Stage,
			}, nil
		},
	)

	// Act.
	projection, meta, err := DispatchAndProject(t.Context(), router, routeRequest{}, projector, policy)

	// Assert.
	if err != nil {
		t.Fatalf("DispatchAndProject() error = %v", err)
	}
	if projection.FailedStage != ErrorStageProjection || projection.RouteID != "projected" {
		t.Fatalf("projection = %#v, want projection-stage mapping", projection)
	}
	if meta.Match.RouteID != "projected" {
		t.Fatalf("meta.Match.RouteID = %q, want projected", meta.Match.RouteID)
	}
}

func TestErrorPolicyMapsDispatchError(t *testing.T) {
	// Arrange.
	dispatchErr := errors.New("dispatch")
	router, err := NewRouteTable[routeRequest, testKind, testReason, string]().
		Route("broken", 1, nil, func(RouteCall[routeRequest]) (RouteResult[testKind, testReason, string], error) {
			return AbortResult[testKind, testReason, string](), dispatchErr
		}).
		Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	projector := OutcomeProjectorFunc[testKind, testReason, string, projectedResult](
		func(result RouteResult[testKind, testReason, string]) (
			projectedResult,
			ProjectionMeta[testKind, testReason],
			error,
		) {
			t.Fatalf("projector should not run after dispatch error: %#v", result)
			return projectedResult{}, ProjectionMeta[testKind, testReason]{}, nil
		},
	)
	policy := ErrorPolicyFunc[testKind, testReason, string, projectedResult](
		func(routeErr RouteError[testKind, testReason, string]) (projectedResult, error) {
			return projectedResult{
				Action:      routeErr.Result.Action,
				RouteID:     routeErr.Match.RouteID,
				FailedStage: routeErr.Stage,
			}, nil
		},
	)

	// Act.
	projection, _, err := DispatchAndProject(t.Context(), router, routeRequest{}, projector, policy)

	// Assert.
	if err != nil {
		t.Fatalf("DispatchAndProject() error = %v", err)
	}
	if projection.Action != ActionAbort ||
		projection.RouteID != "broken" ||
		projection.FailedStage != ErrorStageDispatch {
		t.Fatalf("projection = %#v, want dispatch abort mapping", projection)
	}
}
