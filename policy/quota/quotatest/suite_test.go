package quotatest_test

import (
	"context"
	"errors"
	"io"
	"maps"
	"sync"
	"testing"

	"github.com/skosovsky/routery/policy/attempt"
	"github.com/skosovsky/routery/policy/quota"
	"github.com/skosovsky/routery/policy/quota/quotatest"
)

// Host-owned types intentionally differ from the runtime quota test fixtures.
type tenant struct{ Account int }
type resource uint8

const cpu resource = 1

type ticket struct{ Number uint64 }
type refusal struct{ Code uint8 }
type identity struct {
	scope              tenant
	operation, attempt string
}
type entry struct {
	request                     quota.ReserveRequest[tenant, resource]
	reservation                 quota.Reservation[ticket, refusal]
	state                       quota.State
	actual                      map[resource]uint64
	applications, finalizations uint64
	id                          string
	proof                       quota.ReleaseProof[ticket, refusal]
	expired                     bool
}
type ledger struct {
	mu                      sync.Mutex
	rows                    map[identity]*entry
	handles                 map[ticket]*entry
	next                    uint64
	credits                 map[tenant]uint64
	loseReserve, loseCommit bool
	broken                  bool
	refundAck               bool
}
type client struct {
	store  *ledger
	cached map[ticket]quota.State
}

func key(request quota.ReserveRequest[tenant, resource]) identity {
	return identity{scope: request.Scope, operation: request.Identity.Operation, attempt: request.Identity.Attempt}
}

func (c *client) Reserve(
	ctx context.Context,
	request quota.ReserveRequest[tenant, resource],
) (quota.Reservation[ticket, refusal], error) {
	if err := ctx.Err(); err != nil {
		return quota.Reservation[ticket, refusal]{}, err
	}
	c.store.mu.Lock()
	defer c.store.mu.Unlock()
	if previous := c.store.rows[key(request)]; previous != nil {
		if previous.request.Fingerprint != request.Fingerprint ||
			!maps.Equal(previous.request.Estimated, request.Estimated) ||
			!previous.request.Deadline.Equal(request.Deadline) {
			return quota.Reservation[ticket, refusal]{}, quota.ErrConflict
		}
		c.cached[previous.reservation.Handle] = previous.state
		return previous.reservation, nil
	}
	available, known := c.store.credits[request.Scope]
	if !known {
		available = 4
		c.store.credits[request.Scope] = available
	}
	if request.Estimated[cpu] > available {
		return quota.Reservation[ticket, refusal]{Admission: quota.Denied}, nil
	}
	c.store.credits[request.Scope] -= request.Estimated[cpu]
	c.store.next++
	request.Estimated = maps.Clone(request.Estimated)
	reservation := quota.Reservation[ticket, refusal]{Admission: quota.Admitted, Handle: ticket{Number: c.store.next}}
	row := &entry{request: request, reservation: reservation, state: quota.Reserved, applications: 1}
	c.store.rows[key(request)] = row
	c.store.handles[reservation.Handle] = row
	c.cached[reservation.Handle] = quota.Reserved
	if c.store.loseReserve {
		c.store.loseReserve = false
		if c.store.refundAck {
			c.store.credits[row.request.Scope] += row.request.Estimated[cpu]
		}
		return quota.Reservation[ticket, refusal]{}, &quota.ReserveError{
			Err:        quota.ErrBackendUnavailable,
			UnknownAck: true,
		}
	}
	return reservation, nil
}
func (c *client) Commit(ctx context.Context, settlement quota.Settlement[ticket, resource]) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.store.mu.Lock()
	defer c.store.mu.Unlock()
	row := c.store.handles[settlement.Handle]
	if row == nil || row.expired {
		return quota.ErrUnknownHandle
	}
	if !settlement.Complete {
		return quota.ErrConflict
	}
	// Defective variant trusts its local cache instead of backend arbitration.
	if !c.store.broken || c.cached[settlement.Handle] != quota.Reserved {
		apply, err := validateCommit(row, settlement)
		if err != nil || !apply {
			return err
		}
	}
	charge(c.store, row, settlement.Actual[cpu])
	row.state = quota.Committed
	row.id = settlement.ID
	row.actual = maps.Clone(settlement.Actual)
	row.finalizations++
	if c.store.loseCommit {
		c.store.loseCommit = false
		if c.store.refundAck {
			c.store.credits[row.request.Scope] += row.request.Estimated[cpu]
		}
		return io.ErrUnexpectedEOF
	}
	return nil
}
func (c *client) Release(ctx context.Context, proof quota.ReleaseProof[ticket, refusal]) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.store.mu.Lock()
	defer c.store.mu.Unlock()
	row := c.store.handles[proof.Handle]
	if row == nil || row.expired {
		return quota.ErrUnknownHandle
	}
	if !proof.NotExecuted {
		return quota.ErrConflict
	}
	if !c.store.broken || c.cached[proof.Handle] != quota.Reserved {
		if row.state == quota.Committed {
			return quota.ErrConflict
		}
		if row.state == quota.Released {
			if row.proof != proof {
				return quota.ErrConflict
			}
			return nil
		}
	}
	c.store.credits[row.request.Scope] += row.request.Estimated[cpu]
	row.state = quota.Released
	row.proof = proof
	row.finalizations++
	return nil
}
func (c *client) Pending(ctx context.Context, settlement quota.Settlement[ticket, resource]) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.store.mu.Lock()
	defer c.store.mu.Unlock()
	row := c.store.handles[settlement.Handle]
	if row == nil || row.expired {
		return quota.ErrUnknownHandle
	}
	if row.state == quota.Committed || row.state == quota.Released || settlement.Complete ||
		(row.id != "" && row.id != settlement.ID) {
		return quota.ErrConflict
	}
	row.state = quota.Pending
	row.id = settlement.ID
	row.actual = maps.Clone(settlement.Actual)
	return nil
}
func factory(broken, capabilities bool) quotatest.Factory[tenant, resource, ticket, refusal] {
	return func(ctx context.Context) (quotatest.Fixture[tenant, resource, ticket, refusal], error) {
		if err := ctx.Err(); err != nil {
			return quotatest.Fixture[tenant, resource, ticket, refusal]{}, err
		}
		store := &ledger{
			rows:    make(map[identity]*entry),
			handles: make(map[ticket]*entry),
			broken:  broken,
			credits: make(map[tenant]uint64),
		}
		fixture := quotatest.Fixture[tenant, resource, ticket, refusal]{
			Request: quota.ReserveRequest[tenant, resource]{
				Scope:       tenant{Account: 1},
				Identity:    attempt.Identity{Operation: "job", Attempt: "initial"},
				Estimated:   map[resource]uint64{cpu: 1},
				Fingerprint: "policy",
			},
			OtherScope: tenant{Account: 2},
			Unit:       cpu,
			Client: func() quota.Backend[tenant, resource, ticket, refusal] {
				return &client{store: store, cached: make(map[ticket]quota.State)}
			},
			Inspect: func(ctx context.Context, request quota.ReserveRequest[tenant, resource]) (quotatest.Record[tenant, resource, ticket, refusal], error) {
				return inspectLedger(ctx, store, request)
			},
			Close: func(ctx context.Context) error { return ctx.Err() },
		}
		if capabilities {
			fixture.LoseReserveAck = func(ctx context.Context) error {
				store.mu.Lock()
				defer store.mu.Unlock()
				store.loseReserve = true
				return ctx.Err()
			}
			fixture.LoseCommitAck = func(ctx context.Context) error {
				store.mu.Lock()
				defer store.mu.Unlock()
				store.loseCommit = true
				return ctx.Err()
			}
			fixture.Expire = func(ctx context.Context, handle ticket) error {
				store.mu.Lock()
				defer store.mu.Unlock()
				if row := store.handles[handle]; row != nil {
					row.expired = true
				}
				return ctx.Err()
			}
			fixture.Restart = func(ctx context.Context) error {
				recovered, err := checkpoint(ctx, store)
				if err != nil {
					return err
				}
				store = recovered // New clients bind to recreated state; old client caches are discarded.
				return nil
			} // Simulated persisted-state recovery, not a real storage/process durability test.
		}
		return fixture, nil
	}
}
func TestHostBackendConformance(t *testing.T) {
	t.Parallel()
	// Arrange/Act: public suite from an external package, with unrelated host types.
	report := quotatest.Run(t, factory(false, true))
	// Assert.
	if !report.Complete() || !report.RequiredPassed() {
		t.Fatal(report)
	}
}
func TestBrokenIndependentClientFinalizationIsDetected(t *testing.T) {
	t.Parallel()
	// Arrange.
	broken := factory(true, true)
	// Act: Check returns evidence without intentionally failing the enclosing test.
	report := quotatest.Check(t.Context(), broken)
	// Assert: deterministic cache bug, no data race or competing test-local locks.
	found := false
	for _, result := range report.Results {
		if result.Name == "concurrent-finalization" && result.Status == quotatest.Failed && result.Err != nil {
			found = true
		}
	}
	if report.RequiredPassed() || report.Complete() || !found {
		t.Fatal("broken backend passed", report)
	}
}
func TestUnsupportedFaultCapabilitiesAreNotPassed(t *testing.T) {
	t.Parallel()
	// Arrange/Act.
	report := quotatest.Check(t.Context(), factory(false, false))
	// Assert.
	unsupported := 0
	for _, result := range report.Results {
		if result.Status == quotatest.Unsupported {
			unsupported++
		}
	}
	if !report.RequiredPassed() || report.Complete() || unsupported != 4 {
		t.Fatal(report)
	}
}
func TestFixtureErrorsAndCleanupAreVisible(t *testing.T) {
	t.Parallel()
	// Arrange.
	cleanupFailure := errors.New("cleanup failed")
	base := factory(false, false)
	failing := quotatest.Factory[tenant, resource, ticket, refusal](
		func(ctx context.Context) (quotatest.Fixture[tenant, resource, ticket, refusal], error) {
			fixture, err := base(ctx)
			fixture.Close = func(context.Context) error { return cleanupFailure }
			return fixture, err
		},
	)
	// Act.
	report := quotatest.Check(t.Context(), failing)
	// Assert.
	for _, result := range report.Results {
		if result.Status != quotatest.Failed || !errors.Is(result.Err, cleanupFailure) {
			t.Fatal(result)
		}
	}
}

func validateCommit(row *entry, settlement quota.Settlement[ticket, resource]) (bool, error) {
	if row.state == quota.Released {
		return false, quota.ErrConflict
	}
	if row.state == quota.Committed {
		if row.id != settlement.ID || !maps.Equal(row.actual, settlement.Actual) {
			return false, quota.ErrConflict
		}
		return false, nil
	}
	if row.id != "" && row.id != settlement.ID {
		return false, quota.ErrConflict
	}
	return true, nil
}
func charge(store *ledger, row *entry, actual uint64) {
	estimated := row.request.Estimated[cpu]
	if actual <= estimated {
		store.credits[row.request.Scope] += estimated - actual
		return
	}
	extra := actual - estimated
	available := store.credits[row.request.Scope]
	if extra > available {
		store.credits[row.request.Scope] = 0
		return
	}
	store.credits[row.request.Scope] -= extra
}
func checkpoint(ctx context.Context, store *ledger) (*ledger, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	recovered := &ledger{
		rows:    make(map[identity]*entry),
		handles: make(map[ticket]*entry),
		next:    store.next,
		credits: maps.Clone(store.credits),
		broken:  store.broken,
	}
	for key, row := range store.rows {
		copied := *row
		copied.request.Estimated = maps.Clone(row.request.Estimated)
		copied.actual = maps.Clone(row.actual)
		recovered.rows[key] = &copied
		recovered.handles[copied.reservation.Handle] = &copied
	}
	return recovered, nil
}

func inspectLedger(
	ctx context.Context,
	store *ledger,
	request quota.ReserveRequest[tenant, resource],
) (quotatest.Record[tenant, resource, ticket, refusal], error) {
	if err := ctx.Err(); err != nil {
		return quotatest.Record[tenant, resource, ticket, refusal]{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	row := store.rows[key(request)]
	if row == nil {
		available, known := store.credits[request.Scope]
		if !known {
			available = 4
		}
		return quotatest.Record[tenant, resource, ticket, refusal]{
			Available: map[resource]uint64{cpu: available},
		}, nil
	}
	requestCopy := row.request
	requestCopy.Estimated = maps.Clone(requestCopy.Estimated)
	return quotatest.Record[tenant, resource, ticket, refusal]{
		Found:         true,
		Request:       requestCopy,
		Reservation:   row.reservation,
		State:         row.state,
		Actual:        maps.Clone(row.actual),
		Available:     map[resource]uint64{cpu: store.credits[row.request.Scope]},
		Applications:  row.applications,
		Finalizations: row.finalizations,
		Retained:      row.state == quota.Pending || row.state == quota.Reserved,
	}, nil
}

func TestUnknownAcknowledgmentRefundIsDetected(t *testing.T) {
	t.Parallel()
	// Arrange: a host backend applies writes but wrongly refunds credits on lost ack.
	base := factory(false, true)
	broken := quotatest.Factory[tenant, resource, ticket, refusal](
		func(ctx context.Context) (quotatest.Fixture[tenant, resource, ticket, refusal], error) {
			fixture, err := base(ctx)
			if err != nil {
				return fixture, err
			}
			fixture.Client().(*client).store.refundAck = true
			return fixture, nil
		},
	)
	// Act.
	report := quotatest.Check(t.Context(), broken)
	// Assert: each supported unknown-ack path rejects the refund.
	rejected := 0
	for _, result := range report.Results {
		switch result.Name {
		case "lost-reserve-ack", "lost-commit-ack", "restart-recovery":
			if result.Status != quotatest.Failed || result.Err == nil {
				t.Fatal(result)
			}
			rejected++
		}
	}
	if report.Complete() || rejected != 3 {
		t.Fatal(report)
	}
}
