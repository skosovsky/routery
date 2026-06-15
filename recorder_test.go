package routery

import "testing"

func TestResultRecorderStopAndIdempotent(t *testing.T) {
	t.Parallel()

	rec := NewResultRecorder[string]()
	rec.Stop("first", "handled")
	rec.Stop("second", "duplicate")

	payload, ok := rec.Payload()
	if !ok || payload != "first" {
		t.Fatalf("payload = %q, ok = %v; want first, true", payload, ok)
	}
	if rec.Action() != ActionStop {
		t.Fatalf("action = %q; want stop", rec.Action())
	}
}

func TestResultRecorderNext(t *testing.T) {
	t.Parallel()

	rec := NewResultRecorder[int]()
	rec.Next("continue")

	if rec.Action() != ActionNext {
		t.Fatalf("action = %q; want next", rec.Action())
	}
	if _, ok := rec.Payload(); ok {
		t.Fatal("expected no payload after Next")
	}
}

func TestResultRecorderIgnore(t *testing.T) {
	t.Parallel()

	rec := NewResultRecorder[int]()
	rec.Ignore("ignored")
	rec.Next("should noop")

	if rec.ReasonCode() != "ignored" {
		t.Fatalf("reason = %q; want ignored", rec.ReasonCode())
	}
	if rec.Action() != ActionStop {
		t.Fatalf("action = %q; want stop", rec.Action())
	}
}

func TestResultRecorderAsyncDefaultReason(t *testing.T) {
	t.Parallel()

	rec := NewResultRecorder[string]()
	rec.Async("ticket-1", "")

	if rec.ReasonCode() != reasonAsyncAccepted {
		t.Fatalf("reason = %q; want %q", rec.ReasonCode(), reasonAsyncAccepted)
	}
}

func TestCopyRecorderOutcome(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		setup      func(ResultRecorder[string])
		wantAction RouteAction
		wantReason string
		wantOK     bool
		wantValue  string
	}{
		{
			name: "stop with payload",
			setup: func(src ResultRecorder[string]) {
				src.Stop("data", "handled")
			},
			wantAction: ActionStop,
			wantReason: "handled",
			wantOK:     true,
			wantValue:  "data",
		},
		{
			name: "ignore",
			setup: func(src ResultRecorder[string]) {
				src.Ignore("skip")
			},
			wantAction: ActionStop,
			wantReason: "skip",
			wantOK:     false,
		},
		{
			name: "async",
			setup: func(src ResultRecorder[string]) {
				src.Async("ticket", "async_accepted")
			},
			wantAction: ActionStop,
			wantReason: "async_accepted",
			wantOK:     true,
			wantValue:  "ticket",
		},
		{
			name: "next",
			setup: func(src ResultRecorder[string]) {
				src.Next("delegate")
			},
			wantAction: ActionNext,
			wantReason: "delegate",
			wantOK:     false,
		},
		{
			name:       "initial no-op",
			setup:      func(ResultRecorder[string]) {},
			wantAction: ActionNext,
			wantReason: "",
			wantOK:     false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			src := NewResultRecorder[string]()
			tc.setup(src)

			dest := NewResultRecorder[string]()
			CopyRecorderOutcome(dest, src)

			if dest.Action() != tc.wantAction {
				t.Fatalf("action = %q; want %q", dest.Action(), tc.wantAction)
			}
			if dest.ReasonCode() != tc.wantReason {
				t.Fatalf("reason = %q; want %q", dest.ReasonCode(), tc.wantReason)
			}
			payload, ok := dest.Payload()
			if ok != tc.wantOK {
				t.Fatalf("payload ok = %v; want %v", ok, tc.wantOK)
			}
			if tc.wantOK && payload != tc.wantValue {
				t.Fatalf("payload = %q; want %q", payload, tc.wantValue)
			}
		})
	}
}

func TestThreadSafeRecorderFirstStopWins(t *testing.T) {
	t.Parallel()

	parent := NewResultRecorder[int]()
	safe := newThreadSafeRecorder(parent)

	safe.Stop(1, "a")
	safe.Stop(2, "b")

	payload, ok := parent.Payload()
	if !ok || payload != 1 {
		t.Fatalf("payload = %d, ok = %v; want 1, true", payload, ok)
	}
}
