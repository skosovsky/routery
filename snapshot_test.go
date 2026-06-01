package routery

import (
	"strings"
	"testing"
)

func TestFingerprintSHA256DeterministicAndOrderSensitive(t *testing.T) {
	t.Parallel()

	a := FingerprintSHA256([]byte("alpha"), []byte("beta"))
	b := FingerprintSHA256([]byte("alpha"), []byte("beta"))
	c := FingerprintSHA256([]byte("beta"), []byte("alpha"))

	if a != b {
		t.Fatalf("expected deterministic digest, got %q and %q", a, b)
	}
	if a == c {
		t.Fatal("expected different digests for different part order")
	}
	if len(a) != 64 {
		t.Fatalf("expected 64 hex chars, got %d", len(a))
	}
}

func TestFingerprintSHA256SkipsEmptyParts(t *testing.T) {
	t.Parallel()

	withEmpty := FingerprintSHA256(nil, []byte("only"), []byte{})
	withoutEmpty := FingerprintSHA256([]byte("only"))

	if withEmpty != withoutEmpty {
		t.Fatalf("empty parts should be ignored: %q vs %q", withEmpty, withoutEmpty)
	}
}

func TestRouteSnapshotIsStale(t *testing.T) {
	t.Parallel()

	current := FingerprintSHA256([]byte("v2"))
	snapshot := &RouteSnapshot[string]{
		Fingerprint: FingerprintSHA256([]byte("v1")),
		State:       "state",
	}

	if !snapshot.IsStale(current) {
		t.Fatal("expected snapshot to be stale")
	}
	if snapshot.IsStale(snapshot.Fingerprint) {
		t.Fatal("expected snapshot to be fresh for matching fingerprint")
	}
}

func TestRouteSnapshotNilIsAlwaysStale(t *testing.T) {
	t.Parallel()

	var snapshot *RouteSnapshot[int]
	if !snapshot.IsStale("anything") {
		t.Fatal("nil snapshot should always be stale")
	}
}

func TestFingerprintReaderMatchesSHA256(t *testing.T) {
	t.Parallel()

	fromReader, err := FingerprintReader(strings.NewReader("routery"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := FingerprintSHA256([]byte("routery"))
	if fromReader != want {
		t.Fatalf("got %q, want %q", fromReader, want)
	}
}
