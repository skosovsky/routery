package routery

import (
	"context"
	"reflect"
)

// PayloadMeta is a safe payload description for projection and telemetry contracts.
//
// It intentionally records only presence and Go type shape. It does not serialize the
// payload value, so callers can decide how much data is safe to expose.
type PayloadMeta struct {
	HasPayload bool
	Type       string
}

// ProjectionMeta describes the canonical routing result that produced a projection.
type ProjectionMeta[Kind comparable, Reason comparable] struct {
	Action  RouteAction
	Kind    Kind
	Reason  Reason
	Match   RouteMatch
	Payload PayloadMeta
}

// OutcomeProjector converts a canonical RouteResult into a caller-owned projection.
type OutcomeProjector[Kind comparable, Reason comparable, Payload any, Projection any] interface {
	Project(RouteResult[Kind, Reason, Payload]) (Projection, ProjectionMeta[Kind, Reason], error)
}

// OutcomeProjectorFunc adapts a function to OutcomeProjector.
type OutcomeProjectorFunc[Kind comparable, Reason comparable, Payload any, Projection any] func(
	RouteResult[Kind, Reason, Payload],
) (Projection, ProjectionMeta[Kind, Reason], error)

// Project implements OutcomeProjector.
func (fn OutcomeProjectorFunc[Kind, Reason, Payload, Projection]) Project(
	result RouteResult[Kind, Reason, Payload],
) (Projection, ProjectionMeta[Kind, Reason], error) {
	if fn == nil {
		var projection Projection
		return projection, ProjectionMeta[Kind, Reason]{}, configError("outcome projector is nil")
	}

	return fn(result)
}

// ErrorStage describes where a route projection flow failed.
type ErrorStage string

const (
	ErrorStageDispatch   ErrorStage = "dispatch"
	ErrorStageProjection ErrorStage = "projection"
	ErrorStageSystem     ErrorStage = "system"
)

// RouteError carries typed routing context into caller-owned error policies.
type RouteError[Kind comparable, Reason comparable, Payload any] struct {
	Stage  ErrorStage
	Err    error
	Result RouteResult[Kind, Reason, Payload]
	Match  RouteMatch
}

// ErrorPolicy maps dispatch/projection/system errors into a caller-owned projection.
type ErrorPolicy[Kind comparable, Reason comparable, Payload any, Projection any] interface {
	HandleRouteError(RouteError[Kind, Reason, Payload]) (Projection, error)
}

// ErrorPolicyFunc adapts a function to ErrorPolicy.
type ErrorPolicyFunc[Kind comparable, Reason comparable, Payload any, Projection any] func(
	RouteError[Kind, Reason, Payload],
) (Projection, error)

// HandleRouteError implements ErrorPolicy.
func (fn ErrorPolicyFunc[Kind, Reason, Payload, Projection]) HandleRouteError(
	routeErr RouteError[Kind, Reason, Payload],
) (Projection, error) {
	if fn == nil {
		var projection Projection
		return projection, configError("error policy is nil")
	}

	return fn(routeErr)
}

// DefaultProjectionMeta builds projection metadata from a canonical route result.
func DefaultProjectionMeta[Kind comparable, Reason comparable, Payload any](
	result RouteResult[Kind, Reason, Payload],
) ProjectionMeta[Kind, Reason] {
	return ProjectionMeta[Kind, Reason]{
		Action: result.Action,
		Kind:   result.Kind,
		Reason: result.Reason,
		Match:  result.Match,
		Payload: PayloadMeta{
			HasPayload: result.HasPayload,
			Type:       payloadType(result.Payload, result.HasPayload),
		},
	}
}

// ProjectRouteResult converts a canonical result through projector.
func ProjectRouteResult[Kind comparable, Reason comparable, Payload any, Projection any](
	result RouteResult[Kind, Reason, Payload],
	projector OutcomeProjector[Kind, Reason, Payload, Projection],
) (Projection, ProjectionMeta[Kind, Reason], error) {
	if projector == nil {
		var projection Projection
		return projection, ProjectionMeta[Kind, Reason]{}, configError("outcome projector is nil")
	}

	return projector.Project(result)
}

// DispatchAndProject dispatches a request, projects the canonical result, and delegates
// dispatch/projection failures to policy when one is provided.
func DispatchAndProject[Req any, Kind comparable, Reason comparable, Payload any, Projection any](
	ctx context.Context,
	router Router[Req, Kind, Reason, Payload],
	req Req,
	projector OutcomeProjector[Kind, Reason, Payload, Projection],
	policy ErrorPolicy[Kind, Reason, Payload, Projection],
) (Projection, ProjectionMeta[Kind, Reason], error) {
	var zero Projection
	if router == nil {
		err := configError("router is nil")
		return handleProjectionError(zeroRouteError[Kind, Reason, Payload](ErrorStageSystem, err), policy)
	}

	result, dispatchErr := router.Dispatch(ctx, req)
	if dispatchErr != nil {
		return handleProjectionError(RouteError[Kind, Reason, Payload]{
			Stage:  ErrorStageDispatch,
			Err:    dispatchErr,
			Result: result,
			Match:  result.Match,
		}, policy)
	}

	projection, meta, projectErr := ProjectRouteResult(result, projector)
	if projectErr != nil {
		routeErr := RouteError[Kind, Reason, Payload]{
			Stage:  ErrorStageProjection,
			Err:    projectErr,
			Result: result,
			Match:  result.Match,
		}
		if policy == nil {
			return zero, meta, projectErr
		}

		mapped, err := policy.HandleRouteError(routeErr)
		return mapped, meta, err
	}

	return projection, meta, nil
}

func handleProjectionError[Kind comparable, Reason comparable, Payload any, Projection any](
	routeErr RouteError[Kind, Reason, Payload],
	policy ErrorPolicy[Kind, Reason, Payload, Projection],
) (Projection, ProjectionMeta[Kind, Reason], error) {
	var projection Projection
	meta := DefaultProjectionMeta(routeErr.Result)
	if policy == nil {
		return projection, meta, routeErr.Err
	}

	mapped, err := policy.HandleRouteError(routeErr)
	return mapped, meta, err
}

func zeroRouteError[Kind comparable, Reason comparable, Payload any](
	stage ErrorStage,
	err error,
) RouteError[Kind, Reason, Payload] {
	result := AbortResult[Kind, Reason, Payload]()
	return RouteError[Kind, Reason, Payload]{
		Stage:  stage,
		Err:    err,
		Result: result,
		Match:  result.Match,
	}
}

func payloadType(payload any, hasPayload bool) string {
	if !hasPayload {
		return ""
	}
	if payload == nil {
		return "<nil>"
	}

	return reflect.TypeOf(payload).String()
}
