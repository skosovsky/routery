package policy

import (
	"errors"
	"time"
)

// ErrAffinityScope indicates missing, mismatched or stale required continuation metadata.
var ErrAffinityScope = errors.New("routery/policy: invalid affinity scope")

// Strength distinguishes required correctness constraints from cache preference.
type Strength uint8

const (
	None Strength = iota
	Preferred
	Required
)

// AffinityResult is a bounded generic routing explanation, never a cache-hit guarantee.
type AffinityResult uint8

const (
	NoAffinity AffinityResult = iota
	Preserved
	PreferenceBypassed
	RequiredUnavailable
)

// Affinity carries caller-owned continuation constraints without storing opaque state.
// Compatible is authoritative; matching key alone never proves state portability.
// For required affinity the caller must pass a trusted scope independent of provider state.
type Affinity[Key comparable, Scope comparable, Descriptor any] struct {
	Strength         Strength
	Key              Key
	Scope            Scope
	TrustedScope     Scope
	ScopePresent     bool
	Fingerprint      string
	StateFingerprint string
	Expires          time.Time
	Compatible       func(Candidate[Key, Scope, Descriptor]) bool
}

func (affinity Affinity[Key, Scope, Descriptor]) sameStamp(current Affinity[Key, Scope, Descriptor]) bool {
	return affinity.Strength == current.Strength && affinity.Key == current.Key &&
		affinity.Scope == current.Scope && affinity.TrustedScope == current.TrustedScope &&
		affinity.ScopePresent == current.ScopePresent && affinity.Fingerprint == current.Fingerprint &&
		affinity.StateFingerprint == current.StateFingerprint && affinity.Expires.Equal(current.Expires)
}

func (affinity Affinity[Key, Scope, Descriptor]) validate(now time.Time) error {
	if affinity.Strength > Required {
		return ErrAffinityScope
	}
	if affinity.Strength == None {
		return nil
	}
	if !affinity.ScopePresent || affinity.Scope != affinity.TrustedScope {
		return ErrAffinityScope
	}
	if affinity.Strength == Required && (affinity.Fingerprint == "" || affinity.StateFingerprint == "" ||
		affinity.Compatible == nil || !affinity.active(now)) {
		return ErrAffinityScope
	}
	return nil
}

func (affinity Affinity[Key, Scope, Descriptor]) active(now time.Time) bool {
	return affinity.Expires.IsZero() || now.Before(affinity.Expires)
}

func (affinity Affinity[Key, Scope, Descriptor]) matches(candidate Candidate[Key, Scope, Descriptor]) bool {
	if candidate.Key != affinity.Key || candidate.Scope != affinity.Scope {
		return false
	}
	return affinity.Compatible == nil || affinity.Compatible(candidate)
}

func (affinity Affinity[Key, Scope, Descriptor]) allows(candidate Candidate[Key, Scope, Descriptor]) bool {
	return affinity.Strength != Required || affinity.matches(candidate)
}

func (affinity Affinity[Key, Scope, Descriptor]) preferred(
	candidate Candidate[Key, Scope, Descriptor],
	now time.Time,
) bool {
	return affinity.Strength == Preferred && affinity.matches(candidate) && affinity.active(now)
}

func (affinity Affinity[Key, Scope, Descriptor]) result(
	candidate Candidate[Key, Scope, Descriptor],
	now time.Time,
) AffinityResult {
	if affinity.Strength == None {
		return NoAffinity
	}
	if affinity.active(now) && affinity.matches(candidate) {
		return Preserved
	}
	return PreferenceBypassed
}
