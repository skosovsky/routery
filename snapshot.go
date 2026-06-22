package routery

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
)

// RouteSnapshot captures immutable routing state together with a fingerprint.
type RouteSnapshot[TState any] struct {
	Fingerprint string
	State       TState
}

// IsStale reports whether the snapshot fingerprint differs from currentFingerprint.
func (snapshot *RouteSnapshot[TState]) IsStale(currentFingerprint string) bool {
	if snapshot == nil {
		return true
	}

	return snapshot.Fingerprint != currentFingerprint
}

// FingerprintSHA256 returns a deterministic hex-encoded SHA-256 digest of parts.
func FingerprintSHA256(parts ...[]byte) string {
	hasher := sha256.New()
	for _, part := range parts {
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(len(part)))
		_, _ = hasher.Write(size[:])
		_, _ = hasher.Write(part)
	}

	return hex.EncodeToString(hasher.Sum(nil))
}

// FingerprintReader returns a deterministic hex-encoded SHA-256 digest of r.
func FingerprintReader(r io.Reader) (string, error) {
	hasher := sha256.New()
	if _, err := io.Copy(hasher, r); err != nil {
		return "", err
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}
