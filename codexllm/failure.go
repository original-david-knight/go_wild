package codexllm

import (
	"errors"
	"strings"
)

// ErrBusy marks a run the provider turned away for load: the model at
// capacity, overloaded, or rate limited. It passes in minutes, and another
// model may serve the same request now.
var ErrBusy = errors.New("codex: the model is busy")

// ErrUsageLimit marks a run the account's usage limit stopped. It passes
// when the limit's window resets, hours rather than minutes.
var ErrUsageLimit = errors.New("codex: the usage limit is reached")

// RunError is a run that exited with an error. Error is the message
// Generate has always returned; errors.Is reaches the exit error and, when
// the run's own failure text names one, ErrBusy or ErrUsageLimit.
type RunError struct {
	Msg string
	// Exit is the process's error, from cmd.Wait.
	Exit error
	// Reason is the failed turn's or error event's message, "" when the
	// run gave none.
	Reason string
}

func (e *RunError) Error() string { return e.Msg }

func (e *RunError) Unwrap() []error {
	errs := []error{e.Exit}
	if k := classify(e.Reason); k != nil {
		errs = append(errs, k)
	}
	return errs
}

// classify reads a failure message: ErrUsageLimit, ErrBusy, or nil when it
// names neither. The usage limit is tested first, since its wording can
// mention a rate.
func classify(reason string) error {
	r := strings.ToLower(reason)
	switch {
	case r == "":
		return nil
	case strings.Contains(r, "usage limit"), strings.Contains(r, "hit your limit"), strings.Contains(r, "quota"):
		return ErrUsageLimit
	case strings.Contains(r, "at capacity"), strings.Contains(r, "overloaded"), strings.Contains(r, "high demand"),
		strings.Contains(r, "rate limit"), strings.Contains(r, "too many requests"), strings.Contains(r, "429"),
		strings.Contains(r, "503"), strings.Contains(r, "529"):
		return ErrBusy
	}
	return nil
}
