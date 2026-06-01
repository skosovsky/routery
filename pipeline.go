package routery

import "context"

// Guard decides whether a pipeline rule should run for a request.
type Guard[TReq any] func(ctx context.Context, req TReq) bool

// Rule binds an optional guard to a handler inside a pipeline.
type Rule[TReq any, TRes any] struct {
	Guard   Guard[TReq]
	Handler Handler[TReq, TRes]
}

// PipelineRouter routes requests through an ordered list of rules.
type PipelineRouter[TReq any, TRes any] interface {
	Route(ctx context.Context, req TReq) (RouteResult[TRes], error)
}

// PipelineBuilder constructs a declarative routing pipeline.
type PipelineBuilder[TReq any, TRes any] struct {
	rules []Rule[TReq, TRes]
}

// NewPipeline starts building a routing pipeline.
func NewPipeline[TReq any, TRes any]() *PipelineBuilder[TReq, TRes] {
	return &PipelineBuilder[TReq, TRes]{}
}

// Match adds a guarded rule to the pipeline.
func (builder *PipelineBuilder[TReq, TRes]) Match(
	guard Guard[TReq],
	handler Handler[TReq, TRes],
) *PipelineBuilder[TReq, TRes] {
	builder.rules = append(builder.rules, Rule[TReq, TRes]{
		Guard:   guard,
		Handler: handler,
	})
	return builder
}

// WithFallback sets the handler invoked when no rule terminates the pipeline.
//
// A nil fallback handler is a configuration error: Route returns [ErrInvalidConfig].
func (builder *PipelineBuilder[TReq, TRes]) WithFallback(handler Handler[TReq, TRes]) PipelineRouter[TReq, TRes] {
	if handler == nil {
		return newInvalidPipelineRouter[TReq, TRes](configError("pipeline fallback handler is nil"))
	}

	return &pipelineRouter[TReq, TRes]{
		rules:    append([]Rule[TReq, TRes](nil), builder.rules...),
		fallback: handler,
	}
}

// Build returns a pipeline without a fallback handler.
//
// When no rule matches or all matched handlers return StatusNext, Route returns
// StatusIgnored with reason code "no_match".
func (builder *PipelineBuilder[TReq, TRes]) Build() PipelineRouter[TReq, TRes] {
	//nolint:exhaustruct // Build intentionally omits fallback.
	return &pipelineRouter[TReq, TRes]{
		rules: append([]Rule[TReq, TRes](nil), builder.rules...),
	}
}

type pipelineRouter[TReq any, TRes any] struct {
	rules    []Rule[TReq, TRes]
	fallback Handler[TReq, TRes]
}

// Route executes pipeline rules in order until one returns a terminal status.
func (router *pipelineRouter[TReq, TRes]) Route(ctx context.Context, req TReq) (RouteResult[TRes], error) {
	for _, rule := range router.rules {
		if rule.Guard != nil && !rule.Guard(ctx, req) {
			continue
		}
		if rule.Handler == nil {
			return zeroRouteResult[TRes](), configError("pipeline rule handler is nil")
		}

		result, err := rule.Handler.Handle(ctx, req)
		if err != nil {
			return zeroRouteResult[TRes](), err
		}
		if result.Status == StatusNext {
			continue
		}

		return result, nil
	}

	if router.fallback != nil {
		return router.fallback.Handle(ctx, req)
	}

	return Ignored[TRes]("no_match"), nil
}

type invalidPipelineRouter[TReq any, TRes any] struct {
	err error
}

func newInvalidPipelineRouter[TReq any, TRes any](err error) PipelineRouter[TReq, TRes] {
	return &invalidPipelineRouter[TReq, TRes]{err: err}
}

func (router *invalidPipelineRouter[TReq, TRes]) Route(context.Context, TReq) (RouteResult[TRes], error) {
	return zeroRouteResult[TRes](), router.err
}
