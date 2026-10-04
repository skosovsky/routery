package routery

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"
)

//nolint:gocognit // Keep the two independent completion orders and channel barriers together.
func TestCircuitOldCompletionCannotOwnNewProbe(t *testing.T) {
	t.Parallel()
	for _, staleFailure := range []bool{false, true} {
		for _, oldFirst := range []bool{false, true} {
			t.Run(
				map[bool]string{false: "success", true: "failure"}[staleFailure]+"/"+map[bool]string{false: "probe-first", true: "old-first"}[oldFirst],
				func(t *testing.T) {
					t.Parallel()
					// Arrange: A admitted closed, B opens, C alone owns half-open.
					oldStarted, probeStarted := make(chan struct{}), make(chan struct{})
					releaseOld, releaseProbe := make(chan struct{}), make(chan struct{})
					oldDone, probeDone := make(chan error, 1), make(chan error, 1)
					base := BasicRouteHandler[string, string](
						func(call RouteCall[string]) (BasicRouteResult[string], error) {
							switch call.Request {
							case "A":
								close(oldStarted)
								<-releaseOld
								if staleFailure {
									return BasicRouteResult[string]{}, io.EOF
								}
							case "B":
								return BasicRouteResult[string]{}, io.EOF
							case "C":
								close(probeStarted)
								<-releaseProbe
							}
							return BasicHandled(call.Request), nil
						},
					)
					handler := ApplyRoute(base, CircuitBreaker[string, BasicKind, BasicReason, string](1, 0, nil))
					go func() { _, err := InvokeRouteHandler(context.Background(), "A", handler); oldDone <- err }()
					<-oldStarted
					_, openErr := InvokeRouteHandler(context.Background(), "B", handler)
					if !errors.Is(openErr, io.EOF) {
						t.Fatal(openErr)
					}
					go func() { _, err := InvokeRouteHandler(context.Background(), "C", handler); probeDone <- err }()
					<-probeStarted
					// Act/Assert: D cannot run until the genuine probe completes.
					_, blocked := InvokeRouteHandler(context.Background(), "D", handler)
					if !errors.Is(blocked, ErrCircuitOpen) {
						t.Fatal(blocked)
					}
					if oldFirst {
						close(releaseOld)
						<-oldDone
						_, stillBlocked := InvokeRouteHandler(context.Background(), "D", handler)
						if !errors.Is(stillBlocked, ErrCircuitOpen) {
							t.Fatal("old completion changed probe", stillBlocked)
						}
						close(releaseProbe)
						if err := <-probeDone; err != nil {
							t.Fatal(err)
						}
					} else {
						close(releaseProbe)
						if err := <-probeDone; err != nil {
							t.Fatal(err)
						}
						close(releaseOld)
						<-oldDone
					}
					result, finalErr := InvokeRouteHandler(context.Background(), "D", handler)
					if finalErr != nil || result.Payload != "D" {
						t.Fatal(result, finalErr)
					}
				},
			)
		}
	}
}
func TestCircuitExcludedProbeReleasesAdmission(t *testing.T) {
	t.Parallel()
	for _, excluded := range []error{context.Canceled, context.DeadlineExceeded, io.ErrNoProgress} {
		// Arrange.
		calls := 0
		base := BasicRouteHandler[int, int](func(RouteCall[int]) (BasicRouteResult[int], error) {
			calls++
			switch calls {
			case 1:
				return BasicRouteResult[int]{}, io.EOF
			case 2:
				return BasicRouteResult[int]{}, excluded
			}
			return BasicHandled(calls), nil
		})
		handler := ApplyRoute(
			base,
			CircuitBreaker[int, BasicKind, BasicReason, int](
				1,
				0,
				func(err error) bool { return errors.Is(err, io.EOF) },
			),
		)
		// Act.
		_, _ = InvokeRouteHandler(context.Background(), 0, handler)
		_, _ = InvokeRouteHandler(context.Background(), 0, handler)
		result, err := InvokeRouteHandler(context.Background(), 0, handler)
		// Assert.
		if err != nil || result.Payload != 3 {
			t.Fatal(result, err)
		}
	}
}
func TestCircuitClassifierRunsOnceOutsideLock(t *testing.T) {
	t.Parallel()
	// Arrange: classifier re-enters the same breaker through a successful inner call.
	var handler BasicRouteHandler[string, string]
	classified := 0
	base := BasicRouteHandler[string, string](func(call RouteCall[string]) (BasicRouteResult[string], error) {
		if call.Request == "outer" {
			return BasicRouteResult[string]{}, io.EOF
		}
		return BasicHandled("inner"), nil
	})
	classifier := func(error) bool {
		classified++
		_, err := InvokeRouteHandler(context.Background(), "inner", handler)
		return err == nil
	}
	handler = ApplyRoute(base, CircuitBreaker[string, BasicKind, BasicReason, string](1, 0, classifier))
	done := make(chan error, 1)
	// Act.
	go func() { _, err := InvokeRouteHandler(context.Background(), "outer", handler); done <- err }()
	// Assert.
	select {
	case err := <-done:
		if !errors.Is(err, io.EOF) || classified != 1 {
			t.Fatal(err, classified)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("classifier held internal mutex")
	}
}
