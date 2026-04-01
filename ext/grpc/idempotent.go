package routerygrpc

// IdempotentMarker marks a request as safe to retry when the server reports
// [google.golang.org/grpc/codes.DeadlineExceeded].
type IdempotentMarker interface {
	GRPCIdempotent() bool
}

// WithIdempotent wraps an arbitrary request with an idempotency flag for
// [DefaultRetryPolicy] without changing generated protobuf types.
type WithIdempotent[Req any] struct {
	Req        Req
	Idempotent bool
}

// GRPCIdempotent implements [IdempotentMarker].
func (w WithIdempotent[Req]) GRPCIdempotent() bool {
	return w.Idempotent
}

func idempotentFromAny(req any) bool {
	if req == nil {
		return false
	}
	m, ok := req.(IdempotentMarker)
	return ok && m.GRPCIdempotent()
}
