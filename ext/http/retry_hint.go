package routeryhttp

import (
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/skosovsky/routery/policy/attempt"
)

const maxRetryAfterLength = 128

// HintClock supplies response receipt time and explicit caller clock provenance.
// References must be safe opaque labels, not raw URLs, credentials or header values.
type HintClock struct {
	ReceivedAt  time.Time
	Source      string
	Reference   string
	Uncertainty time.Duration
}

// RetryAfterHint normalizes one Retry-After field without granting replay permission.
// Invalid fields return a present invalid hint and ErrInvalidHint for declared ignore/reject.
func RetryAfterHint(headers http.Header, clock HintClock) (attempt.Hint, error) {
	values := headers.Values("Retry-After")
	result := attempt.Hint{
		Present: len(values) != 0, NotBefore: time.Time{}, Source: clock.Source,
		Clock: clock.Reference, Uncertainty: clock.Uncertainty,
	}
	if !result.Present {
		return result, nil
	}
	if len(values) != 1 || len(values[0]) > maxRetryAfterLength || clock.Source == "" ||
		clock.Reference == "" || clock.Uncertainty < 0 {
		return result, attempt.ErrInvalidHint
	}
	if _, err := attempt.AddDelay(clock.ReceivedAt, 0); err != nil {
		return result, err
	}
	notBefore, err := retryAfterTime(strings.Trim(values[0], " \t"), clock.ReceivedAt)
	if err != nil {
		return result, err
	}
	if _, err = attempt.AddDelay(notBefore, clock.Uncertainty); err != nil {
		return result, err
	}
	result.NotBefore = notBefore
	return result, nil
}

func retryAfterTime(value string, receivedAt time.Time) (time.Time, error) {
	if decimalSeconds(value) {
		seconds, err := strconv.ParseUint(value, 10, 64)
		if err != nil || seconds > uint64(math.MaxInt64/int64(time.Second)) {
			return time.Time{}, attempt.ErrInvalidHint
		}
		return attempt.AddDelay(receivedAt, time.Duration(seconds)*time.Second)
	}
	date, err := http.ParseTime(value)
	if err != nil {
		return time.Time{}, attempt.ErrInvalidHint
	}
	return attempt.AddDelay(date, 0)
}

func decimalSeconds(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}
