package routery

import (
	"errors"
	"testing"
)

func TestDispatchAndProjectPreservesCanonicalOwnership(t *testing.T) {
	for _, name := range []string{"success", "dispatch error", "nil projector", "typed nil projector", "projection error", "mapped dispatch", "mapped projection", "mapping error"} {
		t.Run(name, func(t *testing.T) { checkProjectionOwnership(t, name) })
	}
}

func checkProjectionOwnership(t *testing.T, name string) {
	t.Helper()
	// Arrange.
	closes := 0
	cleanupErr := errors.New("cleanup")
	failure := errors.New("boundary failure")
	life := NewLifetime(func() error { closes++; return cleanupErr })
	t.Cleanup(func() { _ = life.Close() })
	router, err := NewBasicRouteTable[int, int]().Route("owned", 1, nil,
		func(RouteCall[int]) (BasicRouteResult[int], error) {
			result := BasicHandled(42)
			result.Lifetime = life
			if name == "dispatch error" || name == "mapped dispatch" {
				return result, failure
			}
			return result, nil
		}).Build()
	if err != nil {
		t.Fatal(err)
	}
	projector := ownershipProjector(name, failure)
	policy := ownershipErrorPolicy(t, name, life, failure)
	// Act.
	value, meta, projectErr := DispatchAndProject(t.Context(), router, 0, projector, policy)
	// Assert.
	if meta.Lifetime != life || closes != 0 {
		t.Fatal("projection lost or prematurely closed the canonical resource")
	}
	wantSuccess := name == "success" || name == "mapped dispatch" || name == "mapped projection"
	if wantSuccess && (projectErr != nil || value != 42) {
		t.Fatalf("value=%d error=%v", value, projectErr)
	}
	if !wantSuccess && projectErr == nil {
		t.Fatal("boundary error disappeared")
	}
	firstCloseErr := meta.Lifetime.Close()
	secondCloseErr := meta.Lifetime.Close()
	if !errors.Is(firstCloseErr, cleanupErr) || !errors.Is(secondCloseErr, cleanupErr) || closes != 1 {
		t.Fatal("projected ownership did not retain exactly-once cleanup and its error")
	}
}

func ownershipProjector(name string, failure error) OutcomeProjector[BasicKind, BasicReason, int, int] {
	if name == "nil projector" {
		return nil
	}
	var fn OutcomeProjectorFunc[BasicKind, BasicReason, int, int]
	if name == "typed nil projector" {
		return fn
	}
	return OutcomeProjectorFunc[BasicKind, BasicReason, int, int](
		func(result BasicRouteResult[int]) (int, ProjectionMeta[BasicKind, BasicReason], error) {
			// Deliberately omit the owner from custom metadata.
			if name == "projection error" || name == "mapped projection" || name == "mapping error" {
				return 0, ProjectionMeta[BasicKind, BasicReason]{}, failure
			}
			return result.Payload, ProjectionMeta[BasicKind, BasicReason]{}, nil
		})
}

func ownershipErrorPolicy(
	t *testing.T,
	name string,
	life *Lifetime,
	failure error,
) ErrorPolicy[BasicKind, BasicReason, int, int] {
	t.Helper()
	if name != "mapped dispatch" && name != "mapped projection" && name != "mapping error" {
		return nil
	}
	return ErrorPolicyFunc[BasicKind, BasicReason, int, int](
		func(routeErr RouteError[BasicKind, BasicReason, int]) (int, error) {
			if routeErr.Result.Lifetime != life || !errors.Is(routeErr.Err, failure) {
				t.Fatal("error policy lost canonical owned partial result")
			}
			if name == "mapping error" {
				return 0, failure
			}
			return 42, nil
		})
}
