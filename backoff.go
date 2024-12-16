package backoff

import (
	"context"
	"math/rand/v2"
	"time"
)

// Backoff is a backoff policy for retrying operations.
type Backoff interface {
	// NextBackoff returns the next backoff duration.
	// If the returned duration <= 0, then Retry stops immediately.
	NextBackoff() time.Duration
}

// Stop is a special backoff duration that stops the retry immediately.
const Stop time.Duration = -1

// IntervalBackoff is a backoff policy that waits a fixed interval between retries.
type IntervalBackoff struct {
	interval time.Duration
}

// NewIntervalBackoff returns IntervalBackoff with the given interval.
func NewIntervalBackoff(interval time.Duration) *IntervalBackoff {
	return &IntervalBackoff{
		interval: interval,
	}
}

func (b *IntervalBackoff) NextBackoff() time.Duration {
	return b.interval
}

// Random is a source of random numbers.
type Random interface {
	Float64() float64
}

type mathRand struct{}

func (mathRand) Float64() float64 {
	return rand.Float64()
}

// ExponentialBackoff is a policy that waits exponentially
// longer between retries.
type ExponentialBackoff struct {
	factor     float64
	multiplier float64
	random     Random
	first      bool

	current time.Duration
}

// Default values for ExponentialBackoff.
const (
	DefaultExponentialInitial    = 100 * time.Millisecond
	DefaultExponentialFactor     = 0.5
	DefaultExponentialMultiplier = 1.5
)

// NewExponentialBackoff returns ExponentialBackoff with default settings.
func NewExponentialBackoff() *ExponentialBackoff {
	return &ExponentialBackoff{
		factor:     DefaultExponentialFactor,
		multiplier: DefaultExponentialMultiplier,
		current:    DefaultExponentialInitial,
		random:     mathRand{},
		first:      true,
	}
}

// WithInitial sets the initial backoff duration.
func (b *ExponentialBackoff) WithInitial(initial time.Duration) *ExponentialBackoff {
	b.current = initial
	return b
}

// WithFactor sets the factor for randomizing the backoff duration.
func (b *ExponentialBackoff) WithFactor(factor float64) *ExponentialBackoff {
	b.factor = factor
	return b
}

// WithMultiplier sets the multiplier for increasing the backoff duration.
func (b *ExponentialBackoff) WithMultiplier(multiplier float64) *ExponentialBackoff {
	b.multiplier = multiplier
	return b
}

// WithRandom sets the random number generator.
func (b *ExponentialBackoff) WithRandom(random Random) *ExponentialBackoff {
	b.random = random
	return b
}

func (b *ExponentialBackoff) NextBackoff() time.Duration {
	// always return 0 for the first backoff call to avoid waiting
	if b.first {
		b.first = false
		return 0
	}

	var next time.Duration
	if b.factor <= 0 {
		next = b.current
	} else {
		delta := float64(b.current) * b.factor
		min, max := float64(b.current)-delta, float64(b.current)+delta
		next = time.Duration(min + (b.random.Float64() * (max - min + 1)))
	}

	b.current = time.Duration(float64(b.current) * b.multiplier)
	return next
}

// maxRetriesBackoff wraps another backoff policy and stops after the given number of retries.
type maxRetriesBackoff struct {
	parent Backoff // backoff policy to use
	max    int     // maximum number of retries
	n      int     // number of retries
}

func (b *maxRetriesBackoff) NextBackoff() time.Duration {
	if b.n >= b.max {
		return Stop
	}
	b.n++
	return b.parent.NextBackoff()
}

// WithMaxRetries returns a backoff policy that stops after the given number of retries.
func WithMaxRetries(backoff Backoff, max int) Backoff {
	return &maxRetriesBackoff{
		parent: backoff,
		max:    max,
		n:      0,
	}
}

// maxIntervalBackoff wraps another backoff policy and makes sure that
// the backoff duration never exceeds the given maximum.
type maxIntervalBackoff struct {
	parent Backoff // backoff policy to use
	max    time.Duration
}

func (b *maxIntervalBackoff) NextBackoff() time.Duration {
	next := b.parent.NextBackoff()
	if next > b.max {
		return b.max
	}
	return next
}

// WithMaxInterval returns a backoff policy that makes sure
// that the backoff duration never exceeds the given maximum.
func WithMaxInterval(backoff Backoff, max time.Duration) Backoff {
	return &maxIntervalBackoff{
		parent: backoff,
		max:    max,
	}
}

// Retry retries the given function until it returns nil or Backoff stops.
func Retry(
	backoff Backoff,
	fn func() error,
) error {
	return retry(context.Background(), backoff, fn)
}

// RetryCtx retries the given function until it returns nil or
// ctx is done or Backoff stops.
func RetryCtx(
	ctx context.Context,
	backoff Backoff,
	fn func() error,
) error {
	return retry(ctx, backoff, fn)
}

func retry(
	ctx context.Context,
	backoff Backoff,
	fn func() error,
) error {
	var err error
	timer := time.NewTimer(0)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err = fn(); err == nil {
			return nil
		}

		next := backoff.NextBackoff()
		if next < 0 {
			return err
		}

		timer.Reset(next)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
}
