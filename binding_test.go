package routery

import (
	"errors"
	"testing"
)

type testBranch string

const (
	testBranchPrimary testBranch = "primary"
)

type bindingPayload struct {
	Value string
}

func TestRouteBindingCapturesRouteSnapshotAndFreshnessData(t *testing.T) {
	// Arrange.
	match := RouteMatch{
		RouteID:           "bind",
		Path:              []RouteID{"outer", "bind"},
		Priority:          2,
		Depth:             1,
		Kind:              MatchKindDecision,
		Key:               "primary",
		DecisionReason:    testReasonClassified,
		HasDecisionReason: true,
	}

	// Act.
	binding := NewRouteBinding(
		testBranchPrimary,
		bindingPayload{Value: "payload"},
		match,
		"input-fingerprint",
		"revision-1",
	)
	match.Path[0] = "mutated"

	// Assert.
	if binding.Branch != testBranchPrimary || binding.Binding.Value != "payload" {
		t.Fatalf("binding = %#v, want typed branch and payload", binding)
	}
	if binding.Snapshot.State.Match.RouteID != "bind" || binding.Snapshot.State.Match.Path[0] != "outer" {
		t.Fatalf("snapshot match = %#v, want cloned route metadata", binding.Snapshot.State.Match)
	}
	if binding.Snapshot.State.InputFingerprint != "input-fingerprint" ||
		binding.Snapshot.State.Revision != "revision-1" {
		t.Fatalf("snapshot state = %#v, want caller freshness data", binding.Snapshot.State)
	}
	if binding.Snapshot.Fingerprint == "" {
		t.Fatal("Snapshot.Fingerprint is empty")
	}
}

func TestRouteBindingFingerprintIncludesDecisionMetadata(t *testing.T) {
	// Arrange.
	matchA := RouteMatch{
		RouteID:           "bind",
		Kind:              MatchKindDecision,
		Key:               "primary",
		DecisionReason:    testReasonClassified,
		HasDecisionReason: true,
	}
	matchB := matchA
	matchB.DecisionReason = testReasonDelegate

	// Act.
	bindingA := NewRouteBinding(testBranchPrimary, bindingPayload{Value: "payload"}, matchA, "input", "revision")
	bindingB := NewRouteBinding(testBranchPrimary, bindingPayload{Value: "payload"}, matchB, "input", "revision")

	// Assert.
	if bindingA.Snapshot.Fingerprint == bindingB.Snapshot.Fingerprint {
		t.Fatalf(
			"fingerprints are equal for different decision reasons: %q",
			bindingA.Snapshot.Fingerprint,
		)
	}
}

func TestRouteBindingFingerprintIncludesPriorityAndDepth(t *testing.T) {
	// Arrange.
	matchA := RouteMatch{
		RouteID:  "bind",
		Priority: 1,
		Depth:    1,
		Kind:     MatchKindPrefix,
		Key:      "/a/b",
		Prefix:   "/a",
	}
	matchB := matchA
	matchB.Priority = 2
	matchB.Depth = 3

	// Act.
	bindingA := NewRouteBinding(testBranchPrimary, bindingPayload{Value: "payload"}, matchA, "input", "revision")
	bindingB := NewRouteBinding(testBranchPrimary, bindingPayload{Value: "payload"}, matchB, "input", "revision")

	// Assert.
	if bindingA.Snapshot.Fingerprint == bindingB.Snapshot.Fingerprint {
		t.Fatalf(
			"fingerprints are equal for different priority/depth: %q",
			bindingA.Snapshot.Fingerprint,
		)
	}
}

func TestHandledBindingReturnsTypedBindingPayload(t *testing.T) {
	// Arrange.
	binding := NewRouteBinding(
		testBranchPrimary,
		bindingPayload{Value: "payload"},
		RouteMatch{RouteID: "bind"},
		"input-fingerprint",
		"revision-1",
	)

	// Act.
	result := HandledBinding(testKindHandled, testReasonHandled, binding)

	// Assert.
	if result.Action != ActionStop || !result.HasPayload {
		t.Fatalf("result = %#v, want terminal binding payload", result)
	}
	if result.Payload.Branch != testBranchPrimary || result.Payload.Binding.Value != "payload" {
		t.Fatalf("Payload = %#v, want typed route binding", result.Payload)
	}
}

func TestSnapshotFreshnessPolicyRejectsStaleBinding(t *testing.T) {
	// Arrange.
	binding := NewRouteBinding(
		testBranchPrimary,
		bindingPayload{Value: "payload"},
		RouteMatch{RouteID: "bind"},
		"input-fingerprint",
		"revision-1",
	)
	policy := SnapshotFreshnessPolicyFunc[RouteBindingSnapshot, string](
		func(snapshot RouteSnapshot[RouteBindingSnapshot], currentRevision string) error {
			if snapshot.State.Revision != currentRevision {
				return ErrStaleSnapshot
			}

			return nil
		},
	)

	// Act.
	err := ValidateSnapshotFreshness(binding.Snapshot, "revision-2", policy)

	// Assert.
	if !errors.Is(err, ErrStaleSnapshot) {
		t.Fatalf("ValidateSnapshotFreshness() error = %v, want ErrStaleSnapshot", err)
	}
}
