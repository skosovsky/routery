package routery

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestRouteRegistryDispatchesExactAndLongestPrefixSnapshots(t *testing.T) {
	// Arrange.
	exactRegistry := NewRouteRegistry[routeRequest, testKind, testReason, string]()
	if err := exactRegistry.Register(ExactRouteSpec[routeRequest, testKind, testReason, string, int](
		"two",
		10,
		func(req routeRequest) (int, bool) { return req.Number, true },
		2,
		registryTestHandler("two"),
	)); err != nil {
		t.Fatalf("Register(exact) error = %v", err)
	}
	prefixRegistry := NewRouteRegistry[routeRequest, testKind, testReason, string]()
	if err := prefixRegistry.Register(LongestPrefixRouteSpec[routeRequest, testKind, testReason, string, string](
		"short",
		100,
		func(req routeRequest) (string, bool) { return req.Key, true },
		"/a",
		registryTestHandler("short"),
	)); err != nil {
		t.Fatalf("Register(short) error = %v", err)
	}
	if err := prefixRegistry.Register(LongestPrefixRouteSpec[routeRequest, testKind, testReason, string, string](
		"long",
		1,
		func(req routeRequest) (string, bool) { return req.Key, true },
		"/a/b",
		registryTestHandler("long"),
	)); err != nil {
		t.Fatalf("Register(long) error = %v", err)
	}
	exactSnapshot := exactRegistry.Snapshot()
	prefixSnapshot := prefixRegistry.Snapshot()

	// Act.
	exact, exactErr := exactSnapshot.Dispatch(t.Context(), routeRequest{Number: 2})
	prefix, prefixErr := prefixSnapshot.Dispatch(t.Context(), routeRequest{Key: "/a/b/c"})

	// Assert.
	if exactErr != nil {
		t.Fatalf("Dispatch(exact) error = %v", exactErr)
	}
	if exact.Payload != "two" || exact.Match.Kind != MatchKindExact {
		t.Fatalf("exact result = %#v, want exact two", exact)
	}
	if prefixErr != nil {
		t.Fatalf("Dispatch(prefix) error = %v", prefixErr)
	}
	if prefix.Payload != "long" || prefix.Match.Prefix != "/a/b" {
		t.Fatalf("prefix result = %#v, want longest prefix long", prefix)
	}
	if len(exactSnapshot.RouteIDs) != 1 || len(prefixSnapshot.RouteIDs) != 2 {
		t.Fatalf(
			"RouteIDs len = (%d, %d), want (1, 2)",
			len(exactSnapshot.RouteIDs),
			len(prefixSnapshot.RouteIDs),
		)
	}
}

func TestRouteRegistryValidatesRouteIDsAndDuplicates(t *testing.T) {
	// Arrange.
	registry := NewRouteRegistry[routeRequest, testKind, testReason, string]()

	// Act.
	emptyErr := registry.Register(NewRouteSpec[routeRequest, testKind, testReason, string](
		"  ",
		1,
		nil,
		registryTestHandler("empty"),
	))
	firstErr := registry.Register(NewRouteSpec[routeRequest, testKind, testReason, string](
		" duplicate ",
		1,
		nil,
		registryTestHandler("first"),
	))
	duplicateErr := registry.Register(NewRouteSpec[routeRequest, testKind, testReason, string](
		"duplicate",
		2,
		nil,
		registryTestHandler("second"),
	))

	// Assert.
	if !errors.Is(emptyErr, ErrInvalidRouteID) {
		t.Fatalf("empty Register() error = %v, want ErrInvalidRouteID", emptyErr)
	}
	if firstErr != nil {
		t.Fatalf("first Register() error = %v", firstErr)
	}
	if !errors.Is(duplicateErr, ErrDuplicateRoute) {
		t.Fatalf("duplicate Register() error = %v, want ErrDuplicateRoute", duplicateErr)
	}
}

func TestRouteRegistryRejectsInvalidMatcherAndUnknownPrefixPolicy(t *testing.T) {
	// Arrange.
	registry := NewRouteRegistry[routeRequest, testKind, testReason, string]()
	invalidMatcher := RouteSpec[routeRequest, testKind, testReason, string]{
		id:           "invalid-matcher",
		priority:     1,
		handler:      registryTestHandler("invalid"),
		matcher:      routeMatcher[routeRequest]{},
		prefixPolicy: PrefixPolicyNone,
	}
	unknownPrefixPolicy := NewRouteSpec[routeRequest, testKind, testReason, string](
		"unknown-policy",
		1,
		nil,
		registryTestHandler("unknown"),
	)
	unknownPrefixPolicy.prefixPolicy = PrefixPolicy("unknown")

	// Act.
	invalidMatcherErr := registry.Register(invalidMatcher)
	unknownPrefixPolicyErr := registry.Register(unknownPrefixPolicy)

	// Assert.
	if !errors.Is(invalidMatcherErr, ErrInvalidConfig) {
		t.Fatalf("Register(invalid matcher) error = %v, want ErrInvalidConfig", invalidMatcherErr)
	}
	if !errors.Is(unknownPrefixPolicyErr, ErrConflictingPrefixPolicy) {
		t.Fatalf(
			"Register(unknown prefix policy) error = %v, want ErrConflictingPrefixPolicy",
			unknownPrefixPolicyErr,
		)
	}
}

func TestRouteRegistryRejectsConflictingPrefixPolicyWithoutPublishing(t *testing.T) {
	// Arrange.
	registry := NewRouteRegistry[routeRequest, testKind, testReason, string]()
	if err := registry.Register(PrefixRouteSpec[routeRequest, testKind, testReason, string, string](
		"priority-prefix",
		1,
		func(req routeRequest) (string, bool) { return req.Key, true },
		"/a",
		registryTestHandler("priority"),
	)); err != nil {
		t.Fatalf("Register(priority prefix) error = %v", err)
	}
	before := registry.Snapshot()

	// Act.
	err := registry.Register(LongestPrefixRouteSpec[routeRequest, testKind, testReason, string, string](
		"longest-prefix",
		1,
		func(req routeRequest) (string, bool) { return req.Key, true },
		"/a/b",
		registryTestHandler("longest"),
	))
	after := registry.Snapshot()

	// Assert.
	if !errors.Is(err, ErrConflictingPrefixPolicy) {
		t.Fatalf("Register(longest prefix) error = %v, want ErrConflictingPrefixPolicy", err)
	}
	if after.Fingerprint != before.Fingerprint || len(after.RouteIDs) != len(before.RouteIDs) {
		t.Fatalf("snapshot changed after rejected register: before=%#v after=%#v", before, after)
	}
	result, dispatchErr := after.Dispatch(t.Context(), routeRequest{Key: "/a/b"})
	if dispatchErr != nil {
		t.Fatalf("Dispatch() error = %v", dispatchErr)
	}
	if result.Payload != "priority" {
		t.Fatalf("Payload = %q, want priority", result.Payload)
	}
}

func TestRouteRegistryUnregistersWithoutMutatingPreviousSnapshot(t *testing.T) {
	// Arrange.
	registry := NewRouteRegistry[routeRequest, testKind, testReason, string]()
	if err := registry.Register(NewRouteSpec[routeRequest, testKind, testReason, string](
		"base",
		1,
		nil,
		registryTestHandler("base"),
	)); err != nil {
		t.Fatalf("Register(base) error = %v", err)
	}
	if err := registry.Register(NewRouteSpec[routeRequest, testKind, testReason, string](
		"dynamic",
		10,
		nil,
		registryTestHandler("dynamic"),
	)); err != nil {
		t.Fatalf("Register(dynamic) error = %v", err)
	}
	before := registry.Snapshot()

	// Act.
	removeErr := registry.Unregister("dynamic")
	missingErr := registry.Unregister("dynamic")
	after := registry.Snapshot()
	beforeResult, beforeDispatchErr := before.Dispatch(t.Context(), routeRequest{})
	afterResult, afterDispatchErr := after.Dispatch(t.Context(), routeRequest{})

	// Assert.
	if removeErr != nil {
		t.Fatalf("Unregister(dynamic) error = %v", removeErr)
	}
	if !errors.Is(missingErr, ErrRouteNotFound) {
		t.Fatalf("Unregister(missing) error = %v, want ErrRouteNotFound", missingErr)
	}
	if beforeDispatchErr != nil {
		t.Fatalf("before Dispatch() error = %v", beforeDispatchErr)
	}
	if afterDispatchErr != nil {
		t.Fatalf("after Dispatch() error = %v", afterDispatchErr)
	}
	if beforeResult.Payload != "dynamic" {
		t.Fatalf("before Payload = %q, want immutable dynamic snapshot", beforeResult.Payload)
	}
	if afterResult.Payload != "base" {
		t.Fatalf("after Payload = %q, want base after unregister", afterResult.Payload)
	}
}

func TestRouteRegistryNilExtractorReturnsConfigErrorAtDispatch(t *testing.T) {
	// Arrange.
	registry := NewRouteRegistry[routeRequest, testKind, testReason, string]()
	if err := registry.Register(LongestPrefixRouteSpec[routeRequest, testKind, testReason, string, string](
		"nil-extractor",
		1,
		nil,
		"/a",
		registryTestHandler("nil"),
	)); err != nil {
		t.Fatalf("Register(nil extractor) error = %v", err)
	}

	// Act.
	result, err := registry.Snapshot().Dispatch(t.Context(), routeRequest{Key: "/a"})

	// Assert.
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("Dispatch() error = %v, want ErrInvalidConfig", err)
	}
	if result.Action != ActionAbort || result.Match.RouteID != "nil-extractor" {
		t.Fatalf("result = %#v, want abort for nil-extractor route", result)
	}
}

func TestRouteRegistryPublishesAtomicSnapshotsDuringConcurrentDispatch(t *testing.T) {
	// Arrange.
	registry := NewRouteRegistry[routeRequest, testKind, testReason, string]()
	if err := registry.Register(NewRouteSpec[routeRequest, testKind, testReason, string](
		"base",
		1,
		nil,
		registryTestHandler("base"),
	)); err != nil {
		t.Fatalf("Register(base) error = %v", err)
	}
	dynamic := NewRouteSpec[routeRequest, testKind, testReason, string](
		"dynamic",
		10,
		nil,
		registryTestHandler("dynamic"),
	)
	errs := startRegistryDispatchReaders(t.Context(), registry)

	// Act.
	for range 100 {
		if err := registry.Register(dynamic); err != nil && !errors.Is(err, ErrDuplicateRoute) {
			t.Fatalf("Register(dynamic) error = %v", err)
		}
		if err := registry.Unregister("dynamic"); err != nil && !errors.Is(err, ErrRouteNotFound) {
			t.Fatalf("Unregister(dynamic) error = %v", err)
		}
	}

	// Assert.
	for err := range errs {
		t.Fatalf("concurrent dispatch error = %v", err)
	}
}

func startRegistryDispatchReaders(
	ctx context.Context,
	registry *MutableRouteRegistry[routeRequest, testKind, testReason, string],
) <-chan error {
	errs := make(chan error, 64)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 200 {
				result, err := registry.Snapshot().Dispatch(ctx, routeRequest{})
				if err != nil {
					errs <- err
					return
				}
				if result.Payload != "base" && result.Payload != "dynamic" {
					errs <- errors.New("unexpected payload: " + result.Payload)
					return
				}
			}
		})
	}
	go func() {
		wg.Wait()
		close(errs)
	}()

	return errs
}

func registryTestHandler(payload string) RouteHandler[routeRequest, testKind, testReason, string] {
	return func(RouteCall[routeRequest]) (RouteResult[testKind, testReason, string], error) {
		return Handled(testKindHandled, testReasonHandled, payload), nil
	}
}
