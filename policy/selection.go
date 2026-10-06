package policy

import (
	"context"
	"errors"
	"math"
	"slices"
	"time"

	"github.com/skosovsky/routery"
)

// ErrInvalidSelection indicates invalid input, snapshot references or ranking.
var ErrInvalidSelection = errors.New("routery/policy: invalid selection")

// ErrNoSelection indicates dispatch was requested without a selected candidate.
var ErrNoSelection = errors.New("routery/policy: no selected candidate")

// References identifies the frozen inputs used for selection and dispatch freshness.
// Values are caller-supplied opaque fingerprints, not topology-only hashes.
type References struct {
	Input      string
	Candidates string
	Policy     string
	Estimates  string
}

// Fingerprint combines independently owned inputs without serializing request data.
func (refs References) Fingerprint() string {
	return routery.FingerprintSHA256(
		[]byte(refs.Input),
		[]byte(refs.Candidates),
		[]byte(refs.Policy),
		[]byte(refs.Estimates),
	)
}

// Candidate is a route identity plus a caller-owned immutable descriptor.
type Candidate[Key comparable, Scope comparable, Descriptor any] struct {
	Key         Key
	Scope       Scope
	Route       routery.RouteID
	Fingerprint string
	Descriptor  Descriptor
}

// Evaluation is fixed input to selection; randomness is caller-owned and seedable.
// Select and ValidatePinned normalize Deadline to the earliest nonzero explicit/context deadline.
type Evaluation[Input any] struct {
	Input       Input
	Now         time.Time
	Deadline    time.Time
	References  References
	Seed        uint64
	SelectionID string
}

// Eligibility is a hard gate with a bounded caller-owned reason.
type Eligibility[Reason comparable] struct {
	Allowed bool
	Reason  Reason
}

// Explanation describes a decision without exposing candidate descriptors or request data.
// Reason describes domain eligibility; Rejection separately identifies affinity exclusion.
type Explanation[Key comparable, Reason comparable] struct {
	Key            Key
	Fingerprint    string
	Eligible       bool
	DomainEligible bool
	Rejection      RejectionStage
	Reason         Reason
}

// RejectionStage identifies the policy gate that excluded a candidate.
type RejectionStage uint8

const (
	NotRejected RejectionStage = iota
	DomainRejected
	AffinityRejected
)

// SelectionStatus distinguishes expected absence from an invalid policy.
type SelectionStatus uint8

const (
	NoEligible SelectionStatus = iota
	Selected
	AffinityUnavailable
)

// Selection returns the canonical binding and safe snapshot references.
type Selection[Key comparable, Scope comparable, Descriptor any, Reason comparable] struct {
	Status      SelectionStatus
	Binding     routery.RouteBinding[Key, Candidate[Key, Scope, Descriptor]]
	References  References
	Explanation []Explanation[Key, Reason]
	Affinity    AffinityResult
	Seed        uint64
	SelectionID string
	affinity    Affinity[Key, Scope, Descriptor]
}

// Selector declares hard eligibility followed by caller-owned ranking.
// Freeze must detach mutable descriptor storage, or explicitly return immutable data.
// Higher scores win; ties preserve declaration order. Callbacks must be deterministic
// for the declared input/time/seed when deterministic behavior is required.
type Selector[Input any, Key comparable, Scope comparable, Descriptor any, Reason comparable] struct {
	Validate func(Evaluation[Input]) error
	Freeze   func(Descriptor) Descriptor
	Eligible func(Evaluation[Input], Candidate[Key, Scope, Descriptor]) (Eligibility[Reason], error)
	Rank     func(Evaluation[Input], Candidate[Key, Scope, Descriptor]) (float64, error)
}

// Select applies hard constraints and required affinity before preference or ranking.
func (selector Selector[Input, Key, Scope, Descriptor, Reason]) Select(
	ctx context.Context,
	evaluation Evaluation[Input],
	candidates []Candidate[Key, Scope, Descriptor],
	affinity Affinity[Key, Scope, Descriptor],
) (Selection[Key, Scope, Descriptor, Reason], error) {
	evaluation = effectiveEvaluation(ctx, evaluation)
	var binding routery.RouteBinding[Key, Candidate[Key, Scope, Descriptor]]
	result := Selection[Key, Scope, Descriptor, Reason]{
		Status: NoEligible, Binding: binding,
		References: evaluation.References, Explanation: nil, Affinity: NoAffinity,
		Seed: evaluation.Seed, SelectionID: evaluation.SelectionID,
		affinity: affinity,
	}
	if err := selector.validate(ctx, evaluation, affinity); err != nil {
		return result, err
	}
	frozen, err := selector.freezeCandidates(candidates)
	if err != nil {
		return result, err
	}
	best := -1
	bestScore := math.Inf(-1)
	bestPreferred := false
	for index, candidate := range frozen {
		if err = ctx.Err(); err != nil {
			return result, err
		}
		eligible, eligibilityErr := selector.Eligible(evaluation, candidate)
		if eligibilityErr != nil {
			return result, eligibilityErr
		}
		allowed := eligible.Allowed && affinity.allows(candidate)
		stage := NotRejected
		if !eligible.Allowed {
			stage = DomainRejected
		} else if !allowed {
			stage = AffinityRejected
		}
		result.Explanation = append(result.Explanation, Explanation[Key, Reason]{
			Key:            candidate.Key,
			Fingerprint:    candidate.Fingerprint,
			Eligible:       allowed,
			DomainEligible: eligible.Allowed,
			Rejection:      stage,
			Reason:         eligible.Reason,
		})
		if !allowed {
			continue
		}
		score, rankErr := selector.rank(evaluation, candidate)
		if rankErr != nil {
			return result, rankErr
		}
		preferred := affinity.preferred(candidate, evaluation.Now)
		if betterCandidate(best, score, bestScore, preferred, bestPreferred) {
			best, bestScore, bestPreferred = index, score, preferred
		}
	}
	if best < 0 {
		if affinity.Strength == Required {
			result.Status, result.Affinity = AffinityUnavailable, RequiredUnavailable
		}
		return result, nil
	}
	chosen := frozen[best]
	match := routery.RouteMatch{
		RouteID:  chosen.Route,
		Path:     []routery.RouteID{chosen.Route},
		Kind:     routery.MatchKindDecision,
		Priority: 0, Depth: 0, Key: "", Prefix: "", Remainder: "", DecisionReason: nil, HasDecisionReason: false,
	}
	result.Status = Selected
	result.Binding = routery.NewRouteBinding(
		chosen.Key,
		chosen,
		match,
		evaluation.References.Input,
		evaluation.References.Fingerprint(),
	)
	result.Affinity = affinity.result(chosen, evaluation.Now)
	return result, nil
}

func betterCandidate(best int, score, bestScore float64, preferred, bestPreferred bool) bool {
	return best < 0 || (preferred && !bestPreferred) || (preferred == bestPreferred && score > bestScore)
}

func (selector Selector[Input, Key, Scope, Descriptor, Reason]) rank(
	evaluation Evaluation[Input],
	candidate Candidate[Key, Scope, Descriptor],
) (float64, error) {
	score, err := selector.Rank(evaluation, candidate)
	if err != nil {
		return 0, err
	}
	if math.IsNaN(score) || math.IsInf(score, 0) {
		return 0, ErrInvalidSelection
	}
	return score, nil
}

// ValidateFreshness checks epochs only. Use ValidatePinned for temporal dispatch validation.
func (selection Selection[Key, Scope, Descriptor, Reason]) ValidateFreshness(current References) error {
	if selection.Status != Selected || selection.References != current {
		return routery.ErrStaleSnapshot
	}
	return routery.ValidateSnapshotFreshness(selection.Binding.Snapshot, current,
		routery.SnapshotFreshnessPolicyFunc[routery.RouteBindingSnapshot, References](
			func(snapshot routery.RouteSnapshot[routery.RouteBindingSnapshot], refs References) error {
				if snapshot.State.Revision != refs.Fingerprint() {
					return routery.ErrStaleSnapshot
				}
				return nil
			}))
}

// Dispatch validates selection freshness and cancellation before invoking the caller's route.
// A no-eligible or affinity-unavailable selection returns ErrNoSelection without dispatch.
// Callers should handle selection.Status explicitly before requesting execution.
func Dispatch[Input any, Key comparable, Scope comparable, Descriptor any, Reason comparable, Result any](
	ctx context.Context,
	input Input,
	selection Selection[Key, Scope, Descriptor, Reason],
	current References,
	validate func(context.Context, Selection[Key, Scope, Descriptor, Reason]) error,
	dispatch func(context.Context, Input, routery.RouteBinding[Key, Candidate[Key, Scope, Descriptor]]) (Result, error),
) (Result, error) {
	var result Result
	if err := ctx.Err(); err != nil {
		return result, err
	}
	switch selection.Status {
	case NoEligible, AffinityUnavailable:
		return result, ErrNoSelection
	case Selected:
	default:
		return result, ErrInvalidSelection
	}
	if dispatch == nil || validate == nil {
		return result, ErrInvalidSelection
	}
	if err := selection.ValidateFreshness(current); err != nil {
		return result, err
	}
	if err := validate(ctx, selection); err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	return dispatch(ctx, input, selection.Binding)
}

// PinnedError exposes why a previously selected candidate is no longer eligible.
type PinnedError[Reason comparable] struct {
	Reason Reason
}

// Error avoids publishing arbitrary caller reason values automatically.
func (*PinnedError[Reason]) Error() string { return "routery/policy: pinned candidate ineligible" }

// Unwrap makes an expired/ineligible pinned selection a stale-snapshot rejection.
func (*PinnedError[Reason]) Unwrap() error { return routery.ErrStaleSnapshot }

// ValidatePinned revalidates one selected identity without ranking other candidates.
// The current descriptor must be detached by Freeze or already immutable.
func (selector Selector[Input, Key, Scope, Descriptor, Reason]) ValidatePinned(
	ctx context.Context, evaluation Evaluation[Input],
	selection Selection[Key, Scope, Descriptor, Reason],
	current Candidate[Key, Scope, Descriptor], affinity Affinity[Key, Scope, Descriptor],
) error {
	evaluation = effectiveEvaluation(ctx, evaluation)
	if err := selection.ValidateFreshness(evaluation.References); err != nil {
		return err
	}
	if !selection.affinity.sameStamp(affinity) {
		return routery.ErrStaleSnapshot
	}
	if err := selector.validate(ctx, evaluation, affinity); err != nil {
		return err
	}
	pinned := selection.Binding.Binding
	if selection.Binding.Branch != pinned.Key || current.Key != pinned.Key ||
		current.Scope != pinned.Scope || current.Route != pinned.Route || current.Fingerprint != pinned.Fingerprint {
		return routery.ErrStaleSnapshot
	}
	current.Descriptor = selector.Freeze(current.Descriptor)
	eligible, err := selector.Eligible(evaluation, current)
	if err != nil {
		return err
	}
	if !eligible.Allowed {
		return &PinnedError[Reason]{Reason: eligible.Reason}
	}
	if !selection.affinity.allows(current) || !affinity.allows(current) {
		return routery.ErrStaleSnapshot
	}
	if _, err = selector.rank(evaluation, current); err != nil {
		return err
	}
	return ctx.Err()
}

func (selector Selector[Input, Key, Scope, Descriptor, Reason]) validate(
	ctx context.Context,
	evaluation Evaluation[Input],
	affinity Affinity[Key, Scope, Descriptor],
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if selector.Freeze == nil || selector.Eligible == nil || selector.Rank == nil ||
		evaluation.Now.IsZero() || evaluation.References.Input == "" ||
		evaluation.References.Candidates == "" || evaluation.References.Policy == "" {
		return ErrInvalidSelection
	}
	if deadline, ok := ctx.Deadline(); ok && (evaluation.Deadline.IsZero() || deadline.Before(evaluation.Deadline)) {
		if !evaluation.Now.Before(deadline) {
			return context.DeadlineExceeded
		}
	}
	if !evaluation.Deadline.IsZero() && !evaluation.Now.Before(evaluation.Deadline) {
		return context.DeadlineExceeded
	}
	if selector.Validate != nil {
		if err := selector.Validate(evaluation); err != nil {
			return err
		}
	}
	return affinity.validate(evaluation.Now)
}

func (selector Selector[Input, Key, Scope, Descriptor, Reason]) freezeCandidates(
	candidates []Candidate[Key, Scope, Descriptor],
) ([]Candidate[Key, Scope, Descriptor], error) {
	frozen := slices.Clone(candidates)
	keys := make(map[Key]struct{}, len(frozen))
	for index, candidate := range frozen {
		if _, duplicate := keys[candidate.Key]; duplicate || candidate.Fingerprint == "" || candidate.Route == "" {
			return nil, ErrInvalidSelection
		}
		keys[candidate.Key] = struct{}{}
		frozen[index].Descriptor = selector.Freeze(candidate.Descriptor)
	}
	return frozen, nil
}

func effectiveEvaluation[Input any](ctx context.Context, evaluation Evaluation[Input]) Evaluation[Input] {
	if deadline, ok := ctx.Deadline(); ok && (evaluation.Deadline.IsZero() || deadline.Before(evaluation.Deadline)) {
		evaluation.Deadline = deadline
	}
	return evaluation
}
