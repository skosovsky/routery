//go:build !integration && !e2e

package infratest_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseExactCandidateTagsAndManifestAllowlist(t *testing.T) {
	// Arrange.
	fixture := newReleaseFixture(t)
	// Act.
	if out, err := fixture.invoke(t, "patch", fixture.source); err != nil {
		t.Fatalf("release: %v %s", err, out)
	}
	// Assert: both exact tags share the prepared commit; main retains source.
	candidate := strings.TrimSpace(
		string(read(t, filepath.Join(fixture.repo, ".git/library-releases/active/candidate"))),
	)
	if candidate == fixture.source {
		t.Fatal("candidate did not prepare development manifests")
	}
	for _, tag := range []string{"v0.0.1", "packages/test/v0.0.1"} {
		if got := command(
			t,
			fixture.repo,
			nil,
			"git",
			"--git-dir="+fixture.remote,
			"rev-parse",
			tag,
		); got != candidate {
			t.Fatalf("tag %s = %s, want %s", tag, got, candidate)
		}
	}
	changed := command(
		t,
		fixture.repo,
		nil,
		"git",
		"--git-dir="+fixture.remote,
		"diff",
		"--name-only",
		fixture.source,
		candidate,
	)
	if changed != "packages/test/go.mod" {
		t.Fatalf("unexpected candidate changes: %s", changed)
	}
}

func TestReleaseRejectsModulePathMismatch(t *testing.T) {
	// Arrange: module path does not match its directory.
	fixture := newReleaseFixture(t)
	write(t, filepath.Join(fixture.repo, "packages/test/go.mod"), []byte("module example.invalid/wrong\n\ngo 1.27.2\n"))
	command(t, fixture.repo, nil, "git", "add", "packages/test/go.mod")
	command(t, fixture.repo, nil, "git", "commit", "--quiet", "-m", "wrong module")
	fixture.source = command(t, fixture.repo, nil, "git", "rev-parse", "HEAD")
	// Act.
	out, err := fixture.invoke(t, "patch", fixture.source)
	// Assert.
	if err == nil || !strings.Contains(out, "unexpected module path") {
		t.Fatalf("mismatch accepted: %v %s", err, out)
	}
	if refs := command(
		t,
		fixture.repo,
		nil,
		"git",
		"ls-remote",
		"--refs",
		fixture.remote,
	); refs != fixture.initialRefs {
		t.Fatal("module mismatch published refs")
	}
}

func TestReleaseDeclinedConfirmationRetainsCandidate(t *testing.T) {
	// Arrange: EOF declines publication after preparation.
	fixture := newReleaseFixture(t)
	cmd := exec.CommandContext(t.Context(), "bash", "scripts/release.sh", "patch", fixture.source)
	cmd.Dir = fixture.repo
	// Act.
	out, err := cmd.CombinedOutput()
	// Assert: neither source nor tags were delivered, and finish cannot discard the candidate.
	if err == nil {
		t.Fatalf("confirmation bypassed: %s", out)
	}
	record := filepath.Join(fixture.repo, ".git/library-releases/active")
	candidate := string(read(t, filepath.Join(record, "candidate")))
	version := string(read(t, filepath.Join(record, "version")))
	if refs := command(
		t,
		fixture.repo,
		nil,
		"git",
		"ls-remote",
		"--refs",
		fixture.remote,
	); refs != fixture.initialRefs {
		t.Fatal("declined release published refs")
	}
	if output, finishErr := fixture.invoke(t, "finish"); finishErr == nil {
		t.Fatalf("unfinished candidate archived: %s", output)
	}
	if output, resumeErr := fixture.invoke(t, "resume"); resumeErr != nil {
		t.Fatalf("resume: %v %s", resumeErr, output)
	}
	if string(read(t, filepath.Join(record, "candidate"))) != candidate ||
		string(read(t, filepath.Join(record, "version"))) != version {
		t.Fatal("recovery changed candidate or version")
	}
}

func TestReleaseEachSourceGatePreventsPublication(t *testing.T) {
	for _, gate := range []string{"lint", "test", "test-integration", "test-e2e"} {
		t.Run(gate, func(t *testing.T) {
			// Arrange: fail one selected gate in the committed source.
			fixture := newReleaseFixture(t)
			makefile := "lint test test-integration test-e2e:\n\t@test \"$(MAKECMDGOALS)\" != \"" + gate + "\"\nmodules:\n\t@printf '.\\npackages/test\\n'\n"
			write(t, filepath.Join(fixture.repo, "Makefile"), []byte(makefile))
			command(t, fixture.repo, nil, "git", "add", "Makefile")
			command(t, fixture.repo, nil, "git", "commit", "--quiet", "-m", "reject selected gate")
			fixture.source = command(t, fixture.repo, nil, "git", "rev-parse", "HEAD")
			// Act.
			out, err := fixture.invoke(t, "patch", fixture.source)
			// Assert.
			if err == nil {
				t.Fatalf("failed %s gate accepted: %s", gate, out)
			}
			if refs := command(
				t,
				fixture.repo,
				nil,
				"git",
				"ls-remote",
				"--refs",
				fixture.remote,
			); refs != fixture.initialRefs {
				t.Fatalf("failed %s gate published refs", gate)
			}
		})
	}
}
