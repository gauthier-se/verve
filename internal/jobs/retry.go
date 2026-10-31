package jobs

import (
	"errors"
	"time"
)

const (
	// firstBackoff is the delay before the first retry. It doubles per attempt.
	firstBackoff = 30 * time.Second
	maxBackoff   = time.Hour
	// maxErrorLength is the number of characters that a job keeps of its last error.
	maxErrorLength = 1000
)

// permanentError marks an error that a retry cannot fix.
type permanentError struct {
	err error
}

func (e *permanentError) Error() string {
	return e.err.Error()
}

func (e *permanentError) Unwrap() error {
	return e.err
}

// Permanent marks err as an error that a retry cannot fix, such as a corrupted file: a handler that
// returns it, even wrapped, fails its job at once. It returns nil when err is nil.
func Permanent(err error) error {
	if err == nil {
		return nil
	}

	return &permanentError{err: err}
}

func isPermanent(err error) bool {
	_, ok := errors.AsType[*permanentError](err)

	return ok
}

// backoff returns the delay before the next run of a job that failed attempts times.
func backoff(attempts int64) time.Duration {
	delay := firstBackoff

	for range attempts - 1 {
		delay *= 2
		if delay >= maxBackoff {
			return maxBackoff
		}
	}

	return delay
}

// lastError returns the text of err that a job keeps.
func lastError(err error) string {
	message := []rune(err.Error())
	if len(message) > maxErrorLength {
		message = message[:maxErrorLength]
	}

	return string(message)
}
