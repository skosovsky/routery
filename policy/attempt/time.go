package attempt

import "time"

const (
	minimumYear = 1
	maximumYear = 9999
)

// AddDelay validates portable calendar range and nonnegative duration arithmetic.
// Zero timestamps represent absence and cannot be used as scheduling clocks.
func AddDelay(base time.Time, delay time.Duration) (time.Time, error) {
	if !validTimestamp(base) || delay < 0 {
		return time.Time{}, ErrInvalidHint
	}
	result := base.Add(delay)
	if !validTimestamp(result) || result.Before(base) || !result.Add(-delay).Equal(base) {
		return time.Time{}, ErrInvalidHint
	}
	return result, nil
}

func validTimestamp(value time.Time) bool {
	year := value.UTC().Year()
	return !value.IsZero() && year >= minimumYear && year <= maximumYear
}
