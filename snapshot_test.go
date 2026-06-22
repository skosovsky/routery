package routery

import "testing"

func TestFingerprintSHA256FramesParts(t *testing.T) {
	// Arrange.
	left := [][]byte{[]byte("ab"), []byte("c")}
	right := [][]byte{[]byte("a"), []byte("bc")}

	// Act.
	leftFingerprint := FingerprintSHA256(left...)
	rightFingerprint := FingerprintSHA256(right...)

	// Assert.
	if leftFingerprint == rightFingerprint {
		t.Fatalf("fingerprints are equal for differently framed parts: %q", leftFingerprint)
	}
}
