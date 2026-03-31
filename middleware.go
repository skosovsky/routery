package routery

// Apply applies middlewares to base in reverse order.
//
// The first middleware in the argument list becomes the outer wrapper.
// For example:
//
//	Apply(base, RetryIf(...), Timeout(...))
//
// produces RetryIf(Timeout(base)), while:
//
//	Apply(base, Timeout(...), RetryIf(...))
//
// produces Timeout(RetryIf(base)).
func Apply[Req any, Res any](base Executor[Req, Res], mws ...Middleware[Req, Res]) Executor[Req, Res] {
	if base == nil {
		return invalidExecutor[Req, Res](configError("base executor is nil"))
	}

	for index := len(mws) - 1; index >= 0; index-- {
		middleware := mws[index]
		if middleware == nil {
			continue
		}

		base = middleware(base)
		if base == nil {
			return invalidExecutor[Req, Res](configError("middleware returned nil executor"))
		}
	}

	return base
}
