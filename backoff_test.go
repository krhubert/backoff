package backoff

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestIntervalBackoff(t *testing.T) {
	bo := NewIntervalBackoff(time.Second)
	if next := bo.NextBackoff(); next != time.Second {
		t.Errorf("expected 1 second, got %s", next)
	}
	if next := bo.NextBackoff(); next != time.Second {
		t.Errorf("expected 1 second, got %s", next)
	}
}

func TestExponentialBackoff(t *testing.T) {
	bo := NewExponentialBackoff()
	if next := bo.NextBackoff(); next != 0 {
		t.Errorf("expected 0, got %s", next)
	}

	for i := 0; i < 10; i++ {
		current := bo.current
		next := bo.NextBackoff()
		if next <= current && next <= current*time.Duration(bo.factor) {
			t.Errorf("expected 1 second, got %s", next)
		}
	}
}

func TestWithMaxInterval(t *testing.T) {
	bo := NewIntervalBackoff(5 * time.Second)
	bom := WithMaxInterval(bo, time.Second)
	if next := bom.NextBackoff(); next != time.Second {
		t.Errorf("expected 1 second, got %s", next)
	}
	bom = WithMaxInterval(bo, 10*time.Second)
	if next := bom.NextBackoff(); next != 5*time.Second {
		t.Errorf("expected 5 second, got %s", next)
	}
}

func TestWithMaxRetries(t *testing.T) {
	bo := NewIntervalBackoff(time.Second)
	bor := WithMaxRetries(bo, 1)
	if next := bor.NextBackoff(); next != time.Second {
		t.Errorf("expected 1 second, got %s", next)
	}

	if next := bor.NextBackoff(); next != Stop {
		t.Errorf("expected Stop, got %s", next)
	}
}

func TestRetryIntervalBackoffOptions(t *testing.T) {
	bo := NewExponentialBackoff()

	if bo.factor == 0 {
		t.Errorf("expected %f, got 0", bo.factor)
	}
	if bo.current == 0 {
		t.Errorf("expected %s, got 0", bo.current)
	}
	if bo.multiplier == 0 {
		t.Errorf("expected %f, got 0", bo.multiplier)
	}

	bo.WithInitial(time.Second)
	if bo.current != time.Second {
		t.Errorf("expected %s, got %s", time.Second, bo.current)
	}

	bo.WithFactor(2)
	if bo.factor != 2 {
		t.Errorf("expected 2, got %f", bo.factor)
	}

	bo.WithMultiplier(3)
	if bo.multiplier != 3 {
		t.Errorf("expected 3, got %f", bo.multiplier)
	}

	bo.WithRandom(nil)
	if bo.random != nil {
		t.Errorf("expected nil, got %v", bo.random)
	}
}

func TestRetryIntervalBackoff(t *testing.T) {
	bo := NewExponentialBackoff().
		WithInitial(time.Nanosecond)

	t.Run("success", func(t *testing.T) {
		bor := WithMaxRetries(bo, 1)
		if err := Retry(bor, func() error {
			return nil
		}); err != nil {
			t.Errorf("expected nil error, got %s", err)
		}
	})

	t.Run("fail", func(t *testing.T) {
		bor := WithMaxRetries(bo, 1)
		if err := Retry(bor, func() error {
			return errors.New("fail")
		}); err == nil {
			t.Errorf("expected error, got %s", err)
		}
	})
}

func TestRetryExponentialBackoff(t *testing.T) {
	bo := NewExponentialBackoff()

	t.Run("success", func(t *testing.T) {
		bor := WithMaxRetries(bo, 1)
		if err := Retry(bor, func() error {
			return nil
		}); err != nil {
			t.Errorf("expected nil error, got %s", err)
		}
	})

	t.Run("fail", func(t *testing.T) {
		bor := WithMaxRetries(bo, 1)
		if err := Retry(bor, func() error {
			return errors.New("fail")
		}); err == nil {
			t.Errorf("expected error, got %s", err)
		}
	})
}

func TestRetryExponentialBackoffFactor0(t *testing.T) {
	bo := NewExponentialBackoff().
		WithFactor(0).
		WithInitial(time.Second).
		WithMultiplier(2)
	if next := bo.NextBackoff(); next != 0 {
		t.Errorf("expected 1 second, got %s", next)
	}

	cur := bo.NextBackoff()
	if cur != time.Second {
		t.Errorf("expected 1 second, got %s", cur)
	}

	exp := cur * time.Duration(bo.multiplier)
	if next := bo.NextBackoff(); next != exp {
		t.Errorf("expected %s, got %s", exp, next)
	}
}

func TestRetryCtx(t *testing.T) {
	bo := NewIntervalBackoff(time.Second)

	t.Run("timeout before first call", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
		defer cancel()
		if err := RetryCtx(ctx, bo, func() error {
			return nil
		}); err == nil {
			t.Errorf("expected error, got %s", err)
		}
	})

	t.Run("timeout after first call", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := RetryCtx(ctx, bo, func() error {
			return errors.New("fail")
		}); err == nil {
			t.Errorf("expected error, got %s", err)
		}
	})
}
