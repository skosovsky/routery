package execution

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/skosovsky/routery"
	"github.com/skosovsky/routery/policy/attempt"
	"github.com/skosovsky/routery/policy/quota"
)

//nolint:gocognit // The barrier assertions preserve the exact settlement/cancellation ordering.
func TestAuthorizationRechecksAfterSettlementWait(t *testing.T) {
	for _, cancelContext := range []bool{false, true} {
		t.Run(map[bool]string{false: "clock advanced", true: "context canceled"}[cancelContext], func(t *testing.T) {
			// Arrange: Replay starts reconciliation; authorization must wait for its result.
			coordinator, id := setup(t)
			var offset atomic.Int64
			now := time.Unix(100, 0)
			settling, release, reconciled := make(chan struct{}), make(chan struct{}), make(chan error, 1)
			checked := make(chan struct{}, 1)
			finishCalls := 0
			boundary := testBoundary{
				Fresh: func(context.Context, string) error { return nil },
				Admit: func(context.Context, string, attempt.Identity) (Admission, error) {
					return Admission{Status: quota.Admitted, Finish: func(context.Context, attempt.Event) error {
						finishCalls++
						if finishCalls == 2 {
							close(settling)
							<-release
						}
						return nil
					}}, nil
				},
				Dispatch: func(routery.RouteCall[string], *Receipt) (routery.BasicRouteResult[string], error) {
					return routery.BasicHandled("prior"), nil
				},
				CleanupContext: cleanupContext,
			}
			prior, err := boundary.Run(routery.NewRouteCall(t.Context(), "request"), coordinator, id)
			if err != nil {
				t.Fatal(err)
			}
			next := attempt.Identity{Operation: id.Operation, Attempt: "next"}
			if _, err = coordinator.Begin(next); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			sequence := sequenceFixture(t, &now)
			sequence.Deadline = now.Add(time.Second)
			sequence.Now = func() time.Time {
				at := now.Add(time.Duration(offset.Load()) * time.Second)
				select {
				case checked <- struct{}{}:
				default:
				}
				return at
			}
			sequence.Replay = func(Failure[failureClass]) (attempt.Replay, error) {
				go func() { reconciled <- prior.Receipt.Reconcile() }()
				<-settling
				return attempt.Replay{Retryable: true, Replayable: true, SafeDuplicate: true}, nil
			}
			decision := attempt.Decision{Action: attempt.Retry}
			failure := Failure[failureClass]{}
			completed := make(chan error, 1)
			// Act.
			go func() {
				allowed, authErr := sequence.authorize(ctx, prior.Receipt, next, &failure, &decision)
				if allowed {
					completed <- errors.New("authorized after expiry")
				} else {
					completed <- authErr
				}
			}()
			<-checked
			offset.Store(2)
			if cancelContext {
				cancel()
			}
			close(release)
			authErr := <-completed
			if err = <-reconciled; err != nil {
				t.Fatal(err)
			}
			// Assert.
			want := context.DeadlineExceeded
			if cancelContext {
				want = context.Canceled
			}
			event, _, snapshotErr := coordinator.Snapshot(next)
			if !errors.Is(authErr, want) || snapshotErr != nil || event.Phase != attempt.BeforeDispatch {
				t.Fatalf("event=%+v err=%v/%v", event, authErr, snapshotErr)
			}
		})
	}
}
