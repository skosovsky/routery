package policy

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/skosovsky/routery"
)

type requestFacts struct {
	Schema       bool
	QualityFloor bool
}

func TestSelectionExplanationsAreDeterministicAndExcludeDescriptors(t *testing.T) {
	// Arrange.
	evaluation := fixtureEvaluation()
	selector := fixtureSelector()
	candidates := fixtureCandidates()
	affinity := Affinity[string, string, descriptorFacts]{}
	// Act.
	first, err := selector.Select(t.Context(), evaluation, candidates, affinity)
	if err != nil {
		t.Fatal(err)
	}
	second, err := selector.Select(t.Context(), evaluation, candidates, affinity)
	if err != nil {
		t.Fatal(err)
	}
	diagnostics := fmt.Sprint(first.Explanation)
	// Assert: only explicit safe identities and bounded reasons are exposed here.
	if !reflect.DeepEqual(first.Explanation, second.Explanation) ||
		first.Binding.Branch != second.Binding.Branch || first.Seed != second.Seed ||
		first.SelectionID != second.SelectionID || strings.Contains(diagnostics, "credential") ||
		strings.Contains(diagnostics, "Capabilities") {
		t.Fatalf("nondeterministic or unsafe explanations: %s", diagnostics)
	}
	if (&PinnedError[string]{Reason: "credential"}).Error() != "routery/policy: pinned candidate ineligible" {
		t.Fatal("arbitrary reason was automatically formatted")
	}
}

func TestDispatchRejectsUnknownSelectionStatus(t *testing.T) {
	// Arrange.
	evaluation := fixtureEvaluation()
	selection := Selection[string, string, descriptorFacts, decisionReason]{Status: SelectionStatus(255)}
	calls := 0
	// Act.
	_, err := Dispatch(
		t.Context(),
		evaluation.Input,
		selection,
		evaluation.References,
		nil,
		func(context.Context, requestFacts, routery.RouteBinding[string, Candidate[string, string, descriptorFacts]]) (int, error) {
			calls++
			return 1, nil
		},
	)
	// Assert: malformed metadata must not impersonate an expected no-eligible outcome.
	if !errors.Is(err, ErrInvalidSelection) || calls != 0 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

type descriptorFacts struct {
	Capabilities map[string]bool
	Quality      *float64
	Fresh        bool
	Residency    string
	Rank         float64
	Secret       string
}

type decisionReason uint8

const (
	compatibleReason decisionReason = iota
	unsupportedReason
	staleReason
	qualityReason
)

func fixtureSelector() Selector[requestFacts, string, string, descriptorFacts, decisionReason] {
	return Selector[requestFacts, string, string, descriptorFacts, decisionReason]{
		Freeze: func(descriptor descriptorFacts) descriptorFacts {
			descriptor.Capabilities = maps.Clone(descriptor.Capabilities)
			if descriptor.Quality != nil {
				score := *descriptor.Quality
				descriptor.Quality = &score
			}
			return descriptor
		},
		Eligible: func(evaluation Evaluation[requestFacts], candidate Candidate[string, string, descriptorFacts]) (Eligibility[decisionReason], error) {
			descriptor := candidate.Descriptor
			if !descriptor.Fresh {
				return Eligibility[decisionReason]{Reason: staleReason}, routery.ErrStaleSnapshot
			}
			if evaluation.Input.Schema && !descriptor.Capabilities["schema"] {
				return Eligibility[decisionReason]{Reason: unsupportedReason}, nil
			}
			if evaluation.Input.QualityFloor && (descriptor.Quality == nil || *descriptor.Quality < 0.8) {
				return Eligibility[decisionReason]{Reason: qualityReason}, nil
			}
			if descriptor.Residency != "allowed" {
				return Eligibility[decisionReason]{Reason: unsupportedReason}, nil
			}
			return Eligibility[decisionReason]{Allowed: true, Reason: compatibleReason}, nil
		},
		Rank: func(_ Evaluation[requestFacts], candidate Candidate[string, string, descriptorFacts]) (float64, error) {
			return candidate.Descriptor.Rank, nil
		},
	}
}

func fixtureEvaluation() Evaluation[requestFacts] {
	return Evaluation[requestFacts]{
		Input:       requestFacts{Schema: true},
		Now:         time.Unix(100, 0),
		References:  References{Input: "input", Candidates: "descriptors", Policy: "policy", Estimates: "measurements"},
		Seed:        42,
		SelectionID: "selection",
	}
}

func fixtureCandidates() []Candidate[string, string, descriptorFacts] {
	return []Candidate[string, string, descriptorFacts]{
		{
			Key:         "cheap",
			Scope:       "A",
			Route:       "cheap",
			Fingerprint: "cheap-facts",
			Descriptor: descriptorFacts{
				Capabilities: map[string]bool{},
				Fresh:        true,
				Residency:    "allowed",
				Rank:         10,
				Secret:       "credential",
			},
		},
		{
			Key:         "compatible",
			Scope:       "A",
			Route:       "compatible",
			Fingerprint: "compatible-facts",
			Descriptor: descriptorFacts{
				Capabilities: map[string]bool{"schema": true},
				Fresh:        true,
				Residency:    "allowed",
				Rank:         1,
				Secret:       "credential",
			},
		},
	}
}

func TestSelectionHardEligibilityAndFreshDispatch(t *testing.T) {
	// Arrange.
	candidates := fixtureCandidates()
	evaluation := fixtureEvaluation()
	selector := fixtureSelector()
	// Act.
	selected, err := selector.Select(t.Context(), evaluation, candidates, Affinity[string, string, descriptorFacts]{})
	if err != nil {
		t.Fatal(err)
	}
	candidates[1].Descriptor.Capabilities["schema"] = false
	repeated, err := selector.Select(
		t.Context(),
		evaluation,
		fixtureCandidates(),
		Affinity[string, string, descriptorFacts]{},
	)
	calls := 0
	dispatch := func(context.Context, requestFacts, routery.RouteBinding[string, Candidate[string, string, descriptorFacts]]) (int, error) {
		calls++
		return 1, nil
	}
	validate := func(ctx context.Context, selection Selection[string, string, descriptorFacts, decisionReason]) error {
		return selector.ValidatePinned(
			ctx,
			evaluation,
			selection,
			fixtureCandidates()[1],
			Affinity[string, string, descriptorFacts]{},
		)
	}
	_, dispatchErr := Dispatch(t.Context(), evaluation.Input, selected, evaluation.References, validate, dispatch)
	current := evaluation.References
	current.Candidates = "changed"
	_, staleErr := Dispatch(t.Context(), evaluation.Input, selected, current, validate, dispatch)
	// Assert.
	if err != nil || selected.Status != Selected || selected.Binding.Branch != "compatible" ||
		selected.Explanation[0].Eligible {
		t.Fatal("hard filter failed")
	}
	if selected.Explanation[0].Reason != unsupportedReason ||
		selected.Binding.Snapshot.Fingerprint != repeated.Binding.Snapshot.Fingerprint {
		t.Fatal("explanation/determinism")
	}
	if !selected.Binding.Binding.Descriptor.Capabilities["schema"] {
		t.Fatal("descriptor was not frozen")
	}
	if calls != 1 || dispatchErr != nil || !errors.Is(staleErr, routery.ErrStaleSnapshot) {
		t.Fatal("fresh dispatch contract")
	}
}

func TestNoEligibleQualityAndStaleDescriptor(t *testing.T) {
	// Arrange.
	evaluation := fixtureEvaluation()
	evaluation.Input.QualityFloor = true
	candidates := fixtureCandidates()
	// Act.
	selected, err := fixtureSelector().Select(t.Context(), evaluation, candidates, Affinity[string, string, descriptorFacts]{})
	calls := 0
	_, dispatchErr := Dispatch(
		t.Context(),
		evaluation.Input,
		selected,
		evaluation.References,
		nil,
		func(context.Context, requestFacts, routery.RouteBinding[string, Candidate[string, string, descriptorFacts]]) (int, error) {
			calls++
			return 1, nil
		},
	)
	candidates[1].Descriptor.Fresh = false
	_, staleErr := fixtureSelector().Select(t.Context(), evaluation, candidates, Affinity[string, string, descriptorFacts]{})
	// Assert.
	if err != nil || dispatchErr != nil || selected.Status != NoEligible || calls != 0 {
		t.Fatal("no-eligible must not dispatch")
	}
	if !errors.Is(staleErr, routery.ErrStaleSnapshot) {
		t.Fatal("stale mandatory descriptor accepted")
	}
}

func TestRequiredAndPreferredAffinityRemainWithinHardConstraints(t *testing.T) {
	for _, test := range []struct {
		name     string
		strength Strength
		key      string
		status   SelectionStatus
		affinity AffinityResult
	}{
		{name: "unavailable required endpoint", strength: Required, key: "unavailable", status: AffinityUnavailable, affinity: RequiredUnavailable},
		{name: "unavailable preference", strength: Preferred, key: "unavailable", status: Selected, affinity: PreferenceBypassed},
		{name: "ineligible preference", strength: Preferred, key: "cheap", status: Selected, affinity: PreferenceBypassed},
		{name: "required preserved", strength: Required, key: "compatible", status: Selected, affinity: Preserved},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Arrange.
			affinity := Affinity[string, string, descriptorFacts]{
				Strength:         test.strength,
				Key:              test.key,
				Scope:            "A",
				TrustedScope:     "A",
				ScopePresent:     true,
				Fingerprint:      "affinity",
				StateFingerprint: "state",
				Compatible:       func(Candidate[string, string, descriptorFacts]) bool { return true },
			}
			// Act.
			selected, err := fixtureSelector().Select(t.Context(), fixtureEvaluation(), fixtureCandidates(), affinity)
			// Assert.
			if err != nil || selected.Status != test.status || selected.Affinity != test.affinity {
				t.Fatalf("selected=%+v err=%v", selected, err)
			}
		})
	}
}

func TestAffinityScopeExpiryAndPortabilityAreExplicit(t *testing.T) {
	// Arrange.
	evaluation := fixtureEvaluation()
	affinity := Affinity[string, string, descriptorFacts]{
		Strength:         Required,
		Key:              "compatible",
		Scope:            "A",
		TrustedScope:     "B",
		ScopePresent:     true,
		Fingerprint:      "affinity",
		StateFingerprint: "state",
		Compatible:       func(Candidate[string, string, descriptorFacts]) bool { return true },
	}
	// Act.
	_, scopeErr := fixtureSelector().Select(t.Context(), evaluation, fixtureCandidates(), affinity)
	affinity.TrustedScope = "A"
	affinity.Expires = evaluation.Now
	_, expiryErr := fixtureSelector().Select(t.Context(), evaluation, fixtureCandidates(), affinity)
	affinity.Strength = Preferred
	preferred, preferredErr := fixtureSelector().Select(t.Context(), evaluation, fixtureCandidates(), affinity)
	affinity.Strength, affinity.Expires, affinity.Compatible = Required, time.Time{}, nil
	_, unknownErr := fixtureSelector().Select(t.Context(), evaluation, fixtureCandidates(), affinity)
	// Assert.
	if !errors.Is(scopeErr, ErrAffinityScope) || !errors.Is(expiryErr, ErrAffinityScope) ||
		!errors.Is(unknownErr, ErrAffinityScope) {
		t.Fatal("required scope/expiry/portability bypass")
	}
	if preferredErr != nil || preferred.Status != Selected || preferred.Affinity != PreferenceBypassed {
		t.Fatal("expired preference was treated as required")
	}
}
