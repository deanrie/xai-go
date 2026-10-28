package xai

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const (
	// DefaultPollInterval is the pause between polls of a deferred or batch
	// result. Same as the Python SDK's PollTimer default.
	DefaultPollInterval = time.Second
	// DefaultPollTimeout bounds a whole polling loop. Same as the Python SDK.
	DefaultPollTimeout = 10 * time.Minute
	// MaxPollDuration caps any poll interval or timeout a caller passes, so
	// a very large or infinite value can never turn into a busy loop or an
	// unbounded wait (the lesson from xai-sdk-ts#22/#23).
	MaxPollDuration = 24 * time.Hour
)

// ErrPollTimeout is returned when a polling loop exhausts its timeout.
var ErrPollTimeout = errors.New("xai: polling timed out")

// PollOptions configure a polling loop. Zero values mean the defaults.
type PollOptions struct {
	Interval time.Duration
	Timeout  time.Duration
}

func (o PollOptions) normalized() PollOptions {
	o.Interval = boundDuration(o.Interval, DefaultPollInterval)
	o.Timeout = boundDuration(o.Timeout, DefaultPollTimeout)
	return o
}

// boundDuration maps zero and negative to def and caps the result at
// MaxPollDuration.
func boundDuration(d, def time.Duration) time.Duration {
	if d <= 0 {
		d = def
	}
	if d > MaxPollDuration {
		d = MaxPollDuration
	}
	return d
}

// poll calls step until it returns done, the context ends or the timeout
// passes. step receives the context and reports done or an error.
func poll(ctx context.Context, opts PollOptions, step func(context.Context) (bool, error)) error {
	opts = opts.normalized()
	deadline := time.Now().Add(opts.Timeout)
	timer := time.NewTimer(opts.Interval)
	defer timer.Stop()
	for {
		done, err := step(ctx)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%w after %s", ErrPollTimeout, opts.Timeout)
		}
		timer.Reset(opts.Interval)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
}
