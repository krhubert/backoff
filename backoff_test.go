package backoff

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestIntervalBackoff(t *testing.T) {
	bo := NewIntervalBackoff(time.Second)
	if next := bo.NextBackoff(); next != time.Second {
		t.Fatalf("expected 1 second, got %s", next)
	}
	if next := bo.NextBackoff(); next != time.Second {
		t.Fatalf("expected 1 second, got %s", next)
	}
}

func TestExponentialBackoff(t *testing.T) {
	bo := NewExponentialBackoff()

	for range 50 {
		current := bo.current
		next := bo.NextBackoff()
		if next <= current && next <= current*time.Duration(bo.factor) {
			t.Fatalf("expected less than %s, got %s", current*time.Duration(bo.factor), next)
		}
	}
}

func TestExponentialBackoffNoRandom(t *testing.T) {
	bo := NewExponentialBackoff().
		WithInitial(50 * time.Millisecond).
		WithMultiplier(4).
		WithMaxInterval(4 * time.Hour).
		WithRandomizationFactor(0)

	cur := bo.NextBackoff()
	for i := range 8 {
		next := bo.NextBackoff()
		if next != cur*4 && i < 8 {
			t.Fatalf("expected %s, got %s", cur*4, next)
		}
		if i == 8 && next != 4*time.Hour {
			t.Fatalf("expected max interval %s, got %s", 4*time.Hour, next)
		}
		cur = next
	}
}

func TestExponentialBackoffMaxIntervalMinRange(t *testing.T) {
	bo := NewExponentialBackoff().
		WithInitial(1 * time.Second).
		WithMaxInterval(1 * time.Second).
		WithMaxIntervalMinRange(1.01)

	for range 10 {
		next := bo.NextBackoff()
		if next != 1*time.Second {
			t.Fatalf("expected at least 1 second, got %s", next)
		}
	}
}

func TestRetrySuccess(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newTestExec(nil)

		if err := Retry(t.Context(), e.fn); err != nil {
			t.Fatalf("expected nil, got %v", err)
		}
		if e.called != 1 {
			t.Fatalf("expected 1 call, got %d", e.called)
		}
	})
}

func TestRetry2Success(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fn := func() (int, error) {
			return 1, nil
		}

		out, err := Retry2(t.Context(), fn)
		if err != nil {
			t.Fatalf("expected nil, got %v", err)
		}
		if out != 1 {
			t.Fatalf("expected 1, got %d", out)
		}
	})
}

func TestRetryWithCanceledContext(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newTestExec(nil)

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		err := Retry(ctx, e.fn, WithInitialDelay(100*time.Millisecond))
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	})
}

func TestRetryWithContextTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		terr := errors.New("err")
		e := newTestExec(terr)

		ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
		defer cancel()

		err := Retry(ctx, e.fn)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected context.DeadlineExceeded, got %v", err)
		}
	})
}

func TestRetryWithMaxRetries(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		terr := errors.New("err")
		e := newTestExec(terr)

		err := Retry(t.Context(), e.fn, WithMaxRetries(5))
		if !errors.Is(err, terr) {
			t.Fatalf("expected %v, got %v", terr, err)
		}
		if e.called != 5 {
			t.Fatalf("expected 1 calls, got %d", e.called)
		}
	})
}

func TestRetryWithInitialDelay(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newTestExec(nil)
		initialDelay := 10 * time.Second

		start := time.Now()
		err := Retry(t.Context(), e.fn, WithInitialDelay(initialDelay))
		if err != nil {
			t.Fatalf("expected nil, got %v", err)
		}

		if d := e.firstCalledAt.Sub(start); d < initialDelay {
			t.Fatalf("expected at least %s delay, got %s", initialDelay, d)
		}
	})
}

func TestRetryPanics(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fn := func() error { panic("test panic") }

		var pe PanicError
		err := Retry(t.Context(), fn)
		if !errors.As(err, &pe) {
			t.Fatalf("expected PanicError, got %v", err)
		}
		if s, ok := pe.Value.(string); !ok || s != "test panic" {
			t.Fatalf("expected panic value 'test panic', got %v", pe.Value)
		}

		callersFrames := runtime.CallersFrames(pe.PCs)
		frames := []runtime.Frame{}
		for {
			frame, more := callersFrames.Next()
			frames = append(frames, frame)
			if !more {
				break
			}
		}

		if len(frames) < 2 {
			t.Fatalf("expected at least 2 frames, got %d", len(frames))
		}
		if !strings.HasSuffix(frames[0].Function, "backoff.TestRetryPanics.func1.1") {
			t.Errorf("expected top frame to be TestRetryPanics.func1, got %s", frames[0].Function)
		}
	})
}

func TestRetryWithRepanic(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatalf("expected panic, got nil")
			}
		}()

		fn := func() error { panic("test panic") }

		err := Retry(t.Context(), fn, WithRepanic())
		t.Fatalf("should not reach here, got err: %v", err)
	})
}

func TestRetryWithExecDuration(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		d := 10 * time.Second

		errHandler := func(err Error) {
			if err.Attempt == 2 {
				if since := time.Since(start); since > 2*d {
					t.Fatalf("expected exec duration to be taken into account, got %s", since)
				}
			}
		}

		fn := func() error {
			time.Sleep(d)
			return errors.New("test error")
		}

		err := Retry(
			t.Context(),
			fn,
			WithBackoff(NewIntervalBackoff(d)),
			WithExecDuration(),
			WithMaxRetries(2),
			WithErrorHandler(errHandler),
		)
		if err == nil {
			t.Fatalf("expected nil, got %v", err)
		}
	})
}

func TestRetryStop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		terr := errors.New("err")
		e := newTestExec(terr)

		err := Retry(
			t.Context(),
			e.fn,
			WithBackoff(&testStopBackoff{}),
		)
		if !errors.Is(err, terr) {
			t.Fatalf("expected %v, got %v", terr, err)
		}

		if e.called != 1 {
			t.Fatalf("expected 1 call, got %d", e.called)
		}
	})
}

func TestRetryWithErrorHandler(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		errfn := errors.New("test error")
		fn := func() error { return errfn }

		handlerCalled := false
		handler := func(err Error) {
			handlerCalled = true
			if !errors.Is(err.Err, errfn) {
				t.Fatalf("expected %v, got %v", errfn, err)
			}
			if err.Attempt != 1 {
				t.Fatalf("expected attempt 1, got %d", err.Attempt)
			}
		}

		err := Retry(
			t.Context(),
			fn,
			WithErrorHandler(handler),
			WithMaxRetries(1),
		)
		if !errors.Is(err, errfn) {
			t.Fatalf("expected %v, got %v", errfn, err)
		}
		if !handlerCalled {
			t.Fatalf("expected handler to be called")
		}
	})
}

func TestRetryWithClock(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		errfn := errors.New("test error")
		fn := func() error { return errfn }

		handler := func(err Error) {
			if !err.StartedAt.Equal(err.FinishedAt) {
				t.Fatalf("expected StartedAt and FinishedAt to be equal, got %s and %s",
					err.StartedAt, err.FinishedAt)
			}
		}

		err := Retry(
			t.Context(),
			fn,
			WithClock(func() time.Time { return time.Unix(0, 0) }),
			WithMaxRetries(1),
			WithErrorHandler(handler),
		)
		if !errors.Is(err, errfn) {
			t.Fatalf("expected %v, got %v", errfn, err)
		}
	})
}

// testExec is a helper to track calls and return a predefined error.
type testExec struct {
	err error
	bo  Backoff

	called        int
	firstCalledAt time.Time
}

func newTestExec(err error) *testExec {
	return &testExec{
		err: err,
	}
}

func (e *testExec) fn() error {
	if e.firstCalledAt.IsZero() {
		e.firstCalledAt = time.Now()
	}
	e.called++
	return e.err
}

type testStopBackoff struct{}

func (b *testStopBackoff) NextBackoff() time.Duration {
	return Stop
}
