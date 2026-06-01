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
func Apply[TReq any, TRes any](base Handler[TReq, TRes], mws ...HandlerMiddleware[TReq, TRes]) Handler[TReq, TRes] {
	if base == nil {
		return invalidHandler[TReq, TRes](configError("base handler is nil"))
	}

	for index := len(mws) - 1; index >= 0; index-- {
		middleware := mws[index]
		if middleware == nil {
			continue
		}

		base = middleware(base)
		if base == nil {
			return invalidHandler[TReq, TRes](configError("middleware returned nil handler"))
		}
	}

	return base
}
