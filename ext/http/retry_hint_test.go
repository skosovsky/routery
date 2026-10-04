package routeryhttp

import (
	"errors"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/skosovsky/routery/policy/attempt"
)

func TestRetryAfterHintNormalizedForms(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.October, 4, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name    string
		values  []string
		want    time.Time
		present bool
	}{
		{name: "absent"},
		{name: "zero", values: []string{"0"}, want: now, present: true},
		{name: "seconds and OWS", values: []string{" \t0030\t "}, want: now.Add(30 * time.Second), present: true},
		{name: "HTTP date", values: []string{now.Add(time.Minute).Format(http.TimeFormat)}, want: now.Add(time.Minute), present: true},
		{name: "past HTTP date", values: []string{now.Add(-time.Minute).Format(http.TimeFormat)}, want: now.Add(-time.Minute), present: true},
		{name: "largest whole seconds", values: []string{"9223372036"}, want: now.Add(time.Duration(math.MaxInt64/int64(time.Second)) * time.Second), present: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// Arrange.
			headers := make(http.Header)
			if test.values != nil {
				headers["Retry-After"] = test.values
			}
			clock := HintClock{
				ReceivedAt:  now,
				Source:      "endpoint-reference",
				Reference:   "receipt-clock",
				Uncertainty: time.Second,
			}
			// Act.
			hint, err := RetryAfterHint(headers, clock)
			// Assert.
			if err != nil || hint.Present != test.present || !hint.NotBefore.Equal(test.want) ||
				hint.Source != clock.Source ||
				hint.Clock != clock.Reference ||
				hint.Uncertainty != time.Second {
				t.Fatalf("hint=%+v error=%v", hint, err)
			}
		})
	}
}

func TestRetryAfterHintRejectOrExplicitIgnore(t *testing.T) {
	t.Parallel()
	for _, values := range [][]string{
		{""}, {" "}, {"-1"}, {"+1"}, {"1.5"}, {"9223372037"}, {"18446744073709551616"},
		{"30", "60"}, {"30, 60"}, {"\n30"}, {strings.Repeat("9", maxRetryAfterLength+1)}, {"secret provider text"},
	} {
		// Arrange.
		now := time.Unix(100, 0)
		clock := HintClock{ReceivedAt: now, Source: "source", Reference: "clock"}
		headers := http.Header{"Retry-After": values}
		// Act.
		hint, parseErr := RetryAfterHint(headers, clock)
		_, rejectErr := attempt.Schedule(
			attempt.Decision{Action: attempt.Retry},
			attempt.ScheduleInput{Now: now, Hint: hint},
		)
		ignored, ignoreErr := attempt.Schedule(
			attempt.Decision{Action: attempt.Retry},
			attempt.ScheduleInput{Now: now, Hint: hint, InvalidHint: attempt.IgnoreHint},
		)
		// Assert.
		if !errors.Is(parseErr, attempt.ErrInvalidHint) || !hint.Present || !hint.NotBefore.IsZero() ||
			!errors.Is(rejectErr, attempt.ErrInvalidHint) {
			t.Fatalf("values=%q hint=%+v parse=%v reject=%v", values, hint, parseErr, rejectErr)
		}
		if ignoreErr != nil || ignored.Reason != attempt.HintIgnored || !ignored.NotBefore.Equal(now) {
			t.Fatalf("ignore=%+v error=%v", ignored, ignoreErr)
		}
		if strings.Contains(parseErr.Error(), "secret provider text") {
			t.Fatal("raw response metadata leaked to diagnostics")
		}
	}
}

func TestRetryAfterHintClockAndUncertaintyValidation(t *testing.T) {
	t.Parallel()
	now := time.Unix(100, 0)
	for _, clock := range []HintClock{
		{Source: "source", Reference: "clock"},
		{ReceivedAt: now, Reference: "clock"},
		{ReceivedAt: now, Source: "source"},
		{ReceivedAt: now, Source: "source", Reference: "clock", Uncertainty: -time.Second},
		{ReceivedAt: time.Date(9999, time.December, 31, 23, 59, 59, 0, time.UTC), Source: "source", Reference: "clock", Uncertainty: 2 * time.Second},
	} {
		// Arrange.
		headers := http.Header{"Retry-After": []string{"0"}}
		// Act.
		hint, err := RetryAfterHint(headers, clock)
		// Assert.
		if !errors.Is(err, attempt.ErrInvalidHint) || !hint.Present || !hint.NotBefore.IsZero() {
			t.Fatalf("clock=%+v hint=%+v error=%v", clock, hint, err)
		}
	}
}

func TestRetryAfterHintSchedulingDeadlineAndScope(t *testing.T) {
	t.Parallel()
	// Arrange.
	now := time.Unix(100, 0)
	clock := HintClock{ReceivedAt: now, Source: "account-A", Reference: "receipt-clock", Uncertainty: 2 * time.Second}
	hint, err := RetryAfterHint(http.Header{"Retry-After": []string{"30"}}, clock)
	if err != nil {
		t.Fatal(err)
	}
	cooldowns := attempt.NewCooldowns[string]()
	cooldowns.Extend("A", hint.NotBefore.Add(hint.Uncertainty))
	// Act.
	deferred, deferErr := attempt.Schedule(attempt.Decision{Action: attempt.Retry}, attempt.ScheduleInput{
		Now: now, Backoff: time.Second, Hint: hint, Cooldown: cooldowns.Until("A"), Deadline: now.Add(5 * time.Second),
	})
	independent, independentErr := attempt.Schedule(attempt.Decision{Action: attempt.Retry}, attempt.ScheduleInput{
		Now: now, Backoff: time.Second, Cooldown: cooldowns.Until("B"),
	})
	// Assert.
	if deferErr != nil || deferred.Action != attempt.Defer || !deferred.NotBefore.Equal(now.Add(32*time.Second)) {
		t.Fatalf("deferred=%+v error=%v", deferred, deferErr)
	}
	if independentErr != nil || !independent.NotBefore.Equal(now.Add(time.Second)) {
		t.Fatalf("unrelated scope blocked: %+v error=%v", independent, independentErr)
	}
}

func TestStatusErrorDoesNotPublishRawProviderReason(t *testing.T) {
	t.Parallel()
	// Arrange.
	failure := &StatusError{Code: http.StatusServiceUnavailable,
		Response: &http.Response{Status: "503 secret provider state"},
	}
	// Act.
	message := failure.Error()
	// Assert.
	if message != "routery/ext/http: unexpected status 503" || strings.Contains(message, "secret") {
		t.Fatalf("raw provider status leaked: %q", message)
	}
}
