package execution

import (
	"context"
	"errors"
	"testing"

	"github.com/skosovsky/routery"
	"github.com/skosovsky/routery/policy/attempt"
	"github.com/skosovsky/routery/policy/quota"
)

type unavailableQuota struct {
	*quotaFixture

	err      error
	reserves int
}

func (backend *unavailableQuota) Reserve(
	_ context.Context, _ quota.ReserveRequest[string, string],
) (quota.Reservation[string, string], error) {
	backend.reserves++
	return quota.Reservation[string, string]{}, backend.err
}

func TestBoundaryQuotaFailurePolicyControlsDispatch(t *testing.T) {
	for _, scenario := range []struct {
		name    string
		policy  quota.FailurePolicy
		unknown bool
		calls   int
	}{
		{name: "fail closed", policy: quota.FailClosed},
		{name: "explicit fail open", policy: quota.FailOpen, calls: 1},
		{name: "unknown ack overrides fail open", policy: quota.FailOpen, unknown: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			// Arrange: the failure policy is declared before any physical dispatch.
			coordinator, id := setup(t)
			backend := &unavailableQuota{quotaFixture: &quotaFixture{states: make(map[string]quota.State)},
				err: &quota.ReserveError{Err: quota.ErrBackendUnavailable, UnknownAck: scenario.unknown}}
			calls := 0
			boundary := testBoundary{
				Fresh: func(context.Context, string) error { return nil },
				Admit: func(ctx context.Context, _ string, identity attempt.Identity) (Admission, error) {
					reservation, _, err := quota.Admit(ctx, backend, quota.ReserveRequest[string, string]{
						Scope:       "trusted",
						Identity:    identity,
						Estimated:   map[string]uint64{"units": 1},
						Fingerprint: "quota-policy",
					}, scenario.policy)
					return Admission{Status: reservation.Admission}, err
				},
				Dispatch: func(routery.RouteCall[string], *Receipt) (routery.BasicRouteResult[string], error) {
					calls++
					return routery.BasicHandled("unreserved"), nil
				},
				CleanupContext: cleanupContext,
			}
			// Act.
			result, err := boundary.Run(routery.NewRouteCall(t.Context(), "request"), coordinator, id)
			// Assert: unknown acknowledgement retains identity and cannot dispatch fail-open.
			if calls != scenario.calls || backend.reserves != 1 || result.Receipt == nil {
				t.Fatalf("calls=%d reserves=%d err=%v", calls, backend.reserves, err)
			}
			if scenario.calls == 1 {
				if err != nil || result.Admission != quota.Unreserved || !result.Started {
					t.Fatal("fail-open did not explicitly lose its quota guarantee")
				}
			} else if !errors.Is(err, quota.ErrBackendUnavailable) || result.Started {
				t.Fatal("failed/uncertain admission started dispatch")
			}
			event, remaining, snapshotErr := result.Receipt.Snapshot()
			if snapshotErr != nil || event.Identity != id || remaining != 1 ||
				event.Outcome != attempt.Unknown && result.Started {
				t.Fatal("identity/budget or remote uncertainty was lost")
			}
		})
	}
}
