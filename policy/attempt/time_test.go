package attempt

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"
)

func TestSchedulingOverflowAndPastHint(t *testing.T) {
	t.Parallel()
	// Arrange.
	last := time.Date(9999, time.December, 31, 23, 59, 59, 0, time.UTC)
	now := time.Unix(100, 0)
	// Act.
	_, localErr := Schedule(Decision{Action: Retry}, ScheduleInput{Now: last, Backoff: 2 * time.Second})
	_, hintErr := Schedule(
		Decision{Action: Retry},
		ScheduleInput{
			Now:  now,
			Hint: Hint{Present: true, NotBefore: last, Source: "source", Clock: "clock", Uncertainty: 2 * time.Second},
		},
	)
	ignored, ignoreErr := Schedule(
		Decision{Action: Retry},
		ScheduleInput{
			Now: now,
			Hint: Hint{
				Present:     true,
				NotBefore:   last,
				Source:      "source",
				Clock:       "clock",
				Uncertainty: 2 * time.Second,
			},
			InvalidHint: IgnoreHint,
		},
	)
	past, pastErr := Schedule(
		Decision{Action: Retry},
		ScheduleInput{
			Now:     now,
			Backoff: time.Second,
			Hint:    Hint{Present: true, NotBefore: now.Add(-time.Hour), Source: "source", Clock: "clock"},
		},
	)
	large, largeErr := AddDelay(now, time.Duration(math.MaxInt64))
	// Assert.
	if !errors.Is(localErr, ErrInvalidHint) || !errors.Is(hintErr, ErrInvalidHint) || ignoreErr != nil ||
		ignored.Reason != HintIgnored {
		t.Fatalf("local=%v hint=%v ignored=%+v error=%v", localErr, hintErr, ignored, ignoreErr)
	}
	if pastErr != nil || !past.NotBefore.Equal(now.Add(time.Second)) || largeErr != nil ||
		large.Sub(now) != time.Duration(math.MaxInt64) {
		t.Fatalf("past=%+v error=%v large=%v error=%v", past, pastErr, large, largeErr)
	}
}

func TestWaitClockValidationAndZeroDelayCancellation(t *testing.T) {
	t.Parallel()
	// Arrange.
	now := time.Unix(100, 0)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	// Act.
	zeroErr := Wait(ctx, now, func() time.Time { return now })
	clockErr := Wait(t.Context(), now, func() time.Time { return time.Time{} })
	nilClockErr := Wait(t.Context(), now, nil)
	// Assert.
	if !errors.Is(zeroErr, context.Canceled) || !errors.Is(clockErr, ErrInvalidHint) ||
		!errors.Is(nilClockErr, ErrInvalidHint) {
		t.Fatalf("cancel=%v clock=%v nil=%v", zeroErr, clockErr, nilClockErr)
	}
}
