package provider

import "time"

// RetrySchedule defines a schedule for retrying on errors.
type RetrySchedule []time.Duration

type retryError struct {
	error
	retrySchedule RetrySchedule
}

var (
	// RetryFast should be used for error which are likely to resolve themselves quickly.
	RetryFast = RetrySchedule{
		1 * time.Second, 1 * time.Second,
		5 * time.Second, 5 * time.Second,
		10 * time.Second, 10 * time.Second, 10 * time.Second,
		1 * time.Minute, 1 * time.Minute, 1 * time.Minute, 1 * time.Minute, 1 * time.Minute,
		5 * time.Minute,
	}
	// RetrySlow is used for errors which are unlikely to resolve themselves without other interactions.
	RetrySlow = RetrySchedule{
		1 * time.Minute,
		5 * time.Minute,
		10 * time.Minute,
	}
)

// Next returns the next retry for the schedule.
func (r RetrySchedule) Next(retries int) (int, time.Time) {
	if retries >= len(r) {
		return retries + 1, time.Now().Add(r[len(r)-1])
	}
	return retries + 1, time.Now().Add(r[retries])
}

func newRetryError(err error, s RetrySchedule) error {
	return &retryError{
		error:         err,
		retrySchedule: s,
	}
}

// ErrorToRetrySchedule returns a retry schedule appropriate for the given error type,
// or a default slow retry if the error is unknown.
func ErrorToRetrySchedule(err error) RetrySchedule {
	rErr, ok := err.(*retryError)
	if !ok {
		return RetrySlow
	}
	return rErr.retrySchedule
}
