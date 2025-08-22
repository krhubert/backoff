// Package backoff implements backoff algorithms for retrying operations.
package backoff

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"runtime"
	"time"
)

// Error holds information about a single execution of a retried function.
// It is passed to the error handler, if one is configured.
//
// With error handling many custom behaviors can be implemented, such as:
//
//   - custom backoff policies
//   - logging
//   - metrics
//
// Example of stopping retries after a certain error type:
//
//	type CustomBackoff struct{
//		stop bool
//	}
//
//	func (b *CustomBackoff) BackoffNext() time.Duration {
//		if b.stop {
//			return backoff.Stop
//		}
//		return time.Second
//	}
//
//	func (b *CustomBackoff) ErrorHandler(e backoff.Error) {
//		if errors.Is(e.Err, io.EOF) {
//			b.stop = true
//		}
type Error struct {
	Err        error
	Attempt    int
	StartedAt  time.Time
	FinishedAt time.Time
}

// PanicError is returned when the retried function panics.
// It captures the panic value and the stack trace at the moment of panic.
type PanicError struct {
	PCs   []uintptr
	Value any
}

func (e PanicError) Error() string {
	return fmt.Sprintf("backoff: function panicked: %v", e.Value)
}

// Backoff is a backoff policy for retrying operations.
type Backoff interface {
	// NextBackoff returns the next backoff interval.
	// If the returned interval <= 0, then Retry stops immediately.
	//
	// Method is called after error handler, if one is configured.
	// This is important, because error handler can modify the backoff state
	// allowing to implement more cusutomized backoff policies.
	NextBackoff() time.Duration
}

// Stop is a special backoff duration that stops the retry immediately.
const Stop time.Duration = -1

// IntervalBackoff is a policy that waits a fixed interval between each retries.
type IntervalBackoff struct {
	interval time.Duration
}

// NewIntervalBackoff returns IntervalBackoff with the given interval.
func NewIntervalBackoff(interval time.Duration) *IntervalBackoff {
	return &IntervalBackoff{
		interval: interval,
	}
}

// NextBackoff returns the constant duration.
func (b *IntervalBackoff) NextBackoff() time.Duration {
	return b.interval
}

// ExponentialBackoff is a policy that waits exponentially longer between each retries.
//
// It uses randomization to avoid thundering herd problems.
type ExponentialBackoff struct {
	factor              float64
	multiplier          float64
	random              func() float64
	maxInterval         time.Duration
	maxIntervalMinRange float64

	current time.Duration
}

// NewExponentialBackoff returns ExponentialBackoff with default settings.
//
// Default values for ExponentialBackoff are:
//
// DefaultExponentialInitial sets the initial value
// to start calculating from.
//
//	DefaultExponentialInitial = 100 * time.Millisecond
//
// DefaultExponentialRandomizationFactor set to 0.5 to provide
// a good level of randomness around the current backoff duration.
//
//	DefaultExponentialRandomizationFactor = 0.5
//
// Multiplier set to 1.5 to provide a balance between
// increasing the backoff duration and not not reaching
// the maximum interval too quickly.
//
//	DefaultExponentialMultiplier = 1.5
//
// DefaultExponentialMaxInterval protects against excessively
// long backoff intervals, which might hide problems and
// not notify the caller. Also having a very long backoff interval,
// can prevent function execution if the process is restarted frequently.
// For very long intervals it is recommended to use a solution
// that stores interval and state externally.
//
//	DefaultExponentialMaxInterval = 5 * time.Minute
//
// DefaultMaxIntervalMinRange is the minimum range when randomizing
// the backoff duration when the current duration is equal to maxInterval.
//
//	DefaultMaxIntervalMinRange = 0.8
func NewExponentialBackoff() *ExponentialBackoff {
	return &ExponentialBackoff{
		current:             100 * time.Millisecond,
		factor:              0.5,
		multiplier:          1.5,
		maxInterval:         5 * time.Minute,
		maxIntervalMinRange: 0.8,
		random:              rand.Float64,
	}
}

// WithInitial sets the initial backoff duration.
func (b *ExponentialBackoff) WithInitial(initial time.Duration) *ExponentialBackoff {
	b.current = initial
	return b
}

// WithRandomizationFactor sets the factor for randomizing the backoff duration.
func (b *ExponentialBackoff) WithRandomizationFactor(factor float64) *ExponentialBackoff {
	b.factor = factor
	return b
}

// WithMultiplier sets the multiplier for increasing the backoff duration.
func (b *ExponentialBackoff) WithMultiplier(multiplier float64) *ExponentialBackoff {
	b.multiplier = multiplier
	return b
}

// WithMaxInterval sets the maximum backoff duration.
func (b *ExponentialBackoff) WithMaxInterval(max time.Duration) *ExponentialBackoff {
	b.maxInterval = max
	return b
}

// WithMaxIntervalMinRange sets the minimum range when randomizing
// the backoff duration when the current duration is equal to maxInterval.
// Value must be between 0 and 1, where 0 means no range (always return maxInterval)
// and 1 means full range (return value between 0 and maxInterval).
func (b *ExponentialBackoff) WithMaxIntervalMinRange(r float64) *ExponentialBackoff {
	if r < 0 {
		r = 0
	}
	if r > 1 {
		r = 1
	}
	b.maxIntervalMinRange = r
	return b
}

// WithRandom sets the custom number generator.
//
// You can set up your own random function source or stub it for testing.
func (b *ExponentialBackoff) WithRandom(random func() float64) *ExponentialBackoff {
	b.random = random
	return b
}

// NextBackoff returns the next backoff duration.
func (b *ExponentialBackoff) NextBackoff() time.Duration {
	var next time.Duration
	switch {
	case b.factor <= 0:
		// if randomization is disabled, use current value directly
		next = b.current * time.Duration(b.multiplier)
		next = min(next, b.maxInterval)
		b.current = next

	case b.current == b.maxInterval:
		// return value between 0.8*maxInterval and maxInterval
		min := b.maxIntervalMinRange * float64(b.maxInterval)
		next = time.Duration(b.random()*(float64(b.maxInterval)-min) + min)

	default:
		delta := float64(b.current) * b.factor
		min, max := float64(b.current)-delta, float64(b.current)+delta
		next = time.Duration(min + (b.random() * (max - min + 1)))
		// Check for overflow, if overflow is detected set
		// the current interval to the max interval.
		if float64(b.current) >= float64(b.maxInterval)/b.multiplier {
			b.current = b.maxInterval
		} else {
			b.current = time.Duration(float64(b.current) * b.multiplier)
		}
	}

	return next
}

// Timer is an interface that abstracts [time.Timer] methods used by retry.
type Timer interface {
	C() <-chan time.Time
	Reset(d time.Duration) bool
	Stop() bool
}

// option holds all options that are applied during retrying.
type option struct {
	backoff      Backoff
	initialDelay time.Duration
	maxRetries   int
	errorHandler func(Error)
	execDuration bool
	repanic      bool
	now          func() time.Time
	timer        Timer
}

func newOption() *option {
	return &option{
		backoff:      NewExponentialBackoff(),
		maxRetries:   32,
		initialDelay: 0,
		errorHandler: nil,
		repanic:      false,
		now:          time.Now,
		timer:        newTimer(),
	}
}

// Option allows to configures [Retry]/[Retry2] behavior.
//
// Possible options are:
//
//   - [WithBackoff]
//   - [WithErrorHandler]
//   - [WithInitialDelay]
//   - [WithMaxRetries]
//   - [WithClock]
//   - [WithTimer]
//   - [WithExecDuration]
//   - [WithRepanic]
//
// There's a default set of arbitrary options that are applied
// if no options are provided.
//
// They are adjusted to provide a reasonable safe defaults that
// protects against busy loops and too many retries.
//
// Default values are:
//
//   - backoff: [ExponentialBackoff]
//     exponential backoff is a good general purpose backoff policy
//     that adds some randomness to avoid thundering herd problems.
//
//   - maxRetries: 32
//     low value protects against infinite retries, which
//     can lead to excessive resource consumption.
//
//   - initialDelay: 0s
//     0s makes sure that function is called at least once
//
//   - repanic: false
//     function panics are captured and returned as [PanicError]
//     by default, allowing the caller to handle them as errors
//     without crashing the entire application.
//
//   - no error handler
//     it's recommended to provide your own error handler
//     and keep track of all errors.
type Option func(*option)

// WithBackoff returns an option that sets the backoff policy.
func WithBackoff(b Backoff) Option {
	return func(o *option) {
		if b != nil {
			o.backoff = b
		}
	}
}

// WithErrorHandler returns an option that calls the given function
// with the error returned by the retried function.
//
// This handler can be used for multiple purposes, see [Error] for details.
func WithErrorHandler(fn func(Error)) Option {
	return func(o *option) {
		o.errorHandler = fn
	}
}

// WithInitialDelay returns a backoff policy that waits
// the given initial delay before the first execution.
func WithInitialDelay(d time.Duration) Option {
	return func(o *option) {
		o.initialDelay = d
	}
}

// WithMaxRetries returns a backoff policy that stops retrying
// after the given number of retries.
func WithMaxRetries(max int) Option {
	return func(o *option) {
		o.maxRetries = max
	}
}

// WithRepanic returns an option that makes [Retry]/[Retry2] repanic
// if the retried function panics.
func WithRepanic() Option {
	return func(o *option) {
		o.repanic = true
	}
}

// WithExecDuration returns an option that subtracts the execution duration
// of the retried function from the backoff duration.
//
// Backoff duration is not modified by default. With this option the backoff
// duration is calculated as:
//
//	next_backoff = max(0, backoff - execution_duration)
//
// This is useful if you want to ensure that the total time between retries
// is approximately equal to the backoff duration.
func WithExecDuration() Option {
	return func(o *option) {
		o.execDuration = true
	}
}

// WithClock returns an option that uses custom now implementation,
// instead of time.Now.
//
// This is useful for testing if you want to avoid using real time clock.
func WithClock(fn func() time.Time) Option {
	return func(o *option) {
		if fn != nil {
			o.now = fn
		}
	}
}

// WithTimer returns an option that uses a custom timer implementation,
// instead of time.Timer.
//
// This is useful for testing if you want to avoid using real time timers.
func WithTimer(t Timer) Option {
	return func(o *option) {
		if t != nil {
			o.timer = t
		}
	}
}

// Retry retries the function until it succeeds.
// The retries can be terminated by:
//
//   - context cancellation
//   - reaching the maximum number of retries (see [WithMaxRetries])
//   - backoff policy returning [Stop]
//   - function panicking
//
// When function panics, the retrying is stopped immediately
// and the panic is either propagated or recovered based on
// the [WithRepanic] option.
//
// The retry behavior can be further customized with options,
// see [Option] for details and defaults.
func Retry(
	ctx context.Context,
	fn func() error,
	options ...Option,
) error {
	fn2 := func() (struct{}, error) { return struct{}{}, fn() }
	_, err := retry(ctx, fn2, options...)
	return err
}

// Retry2 is like [Retry] but for functions that return a value.
// See [Retry] for more details.
func Retry2[Out any](
	ctx context.Context,
	fn func() (Out, error),
	options ...Option,
) (Out, error) {
	return retry(ctx, fn, options...)
}

func retry[Out any](
	ctx context.Context,
	fn func() (Out, error),
	options ...Option,
) (out Out, err error) {
	opts := newOption()
	for _, o := range options {
		o(opts)
	}

	defer func() {
		if r := recover(); r != nil {
			if opts.repanic {
				panic(r)
			}
			err = PanicError{
				PCs:   captureFunctionPanicStacktrace(),
				Value: r,
			}
		}
	}()

	var currentAttempt int // retry state
	var zero Out           // for returning zero value of Out type

	// set initial delay timer, or set
	// default 0 value to run immediately

	opts.timer.Reset(opts.initialDelay)
	defer opts.timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return zero, errors.Join(err, ctx.Err())
		case <-opts.timer.C():
		}

		currentAttempt++
		startedAt := opts.now()

		out, err = fn()
		if err == nil {
			return out, nil
		}

		finishedAt := opts.now()

		if opts.errorHandler != nil {
			opts.errorHandler(Error{
				Err:        err,
				Attempt:    currentAttempt,
				StartedAt:  startedAt,
				FinishedAt: finishedAt,
			})
		}

		if currentAttempt >= opts.maxRetries {
			return zero, err
		}

		next := opts.backoff.NextBackoff()
		if next < 0 {
			return zero, err
		}

		if opts.execDuration {
			d := finishedAt.Sub(startedAt)
			next = max(next-d, 0)
		}

		opts.timer.Reset(next)
	}
}

// captureFunctionPanicStacktrace returns a stack trace
// of the current goroutine skipping frames:
//
// 1. runtime.Callers
// 2. captureFunctionPanicStacktrace
// 3. recover defer function.
// 4. retry
func captureFunctionPanicStacktrace() []uintptr {
	const depth = 64
	var pcs [depth]uintptr
	n := runtime.Callers(4, pcs[:])
	return pcs[:n]
}

// timer is a wrapper around time.Timer to implement Timer interface.
// It exposes C channel as a method.
type timer struct{ t *time.Timer }

func newTimer() Timer { return &timer{t: time.NewTimer(0)} }

func (t *timer) C() <-chan time.Time        { return t.t.C }
func (t *timer) Reset(d time.Duration) bool { return t.t.Reset(d) }
func (t *timer) Stop() bool                 { return t.t.Stop() }
