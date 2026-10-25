package polling

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestNewRejectsShortInterval(t *testing.T) {
	if _, err := New(Options{Interval: 10 * time.Second}); err == nil {
		t.Fatal("expected error")
	}
}

func TestRunFirstCycleAndMetrics(t *testing.T) {
	p, err := New(Options{Interval: 30 * time.Second, Jitter: 0, Logger: discardLogger()})
	if err != nil {
		t.Fatal(err)
	}
	p.interval = 25 * time.Millisecond
	ran := make(chan struct{}, 1)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		p.Run(ctx, func(ctx context.Context) error {
			select {
			case ran <- struct{}{}:
			default:
			}
			return nil
		})
		close(done)
	}()
	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("task did not run")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop")
	}
	m := p.Metrics()
	if m.Runs < 1 || m.Successes < 1 || m.Paused {
		t.Fatalf("metrics = %+v", m)
	}
}

type permanentError struct{}

func (permanentError) Error() string     { return "permanent" }
func (permanentError) IsRetryable() bool { return false }

type temporaryError struct{}

func (temporaryError) Error() string     { return "temporary" }
func (temporaryError) IsRetryable() bool { return true }

func TestPermanentErrorNotRetriedAndOnCycle(t *testing.T) {
	p, _ := New(Options{Interval: 30 * time.Second, Jitter: 0, MaxRetries: 3, Logger: discardLogger()})
	p.interval = 20 * time.Millisecond
	var attempts int32
	cycle := make(chan error, 1)
	p.SetOnCycle(func(err error) {
		select {
		case cycle <- err:
		default:
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx, func(ctx context.Context) error {
		atomic.AddInt32(&attempts, 1)
		return permanentError{}
	})
	select {
	case err := <-cycle:
		if err == nil {
			t.Fatal("expected error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no cycle callback")
	}
	cancel()
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Fatalf("attempts = %d", got)
	}
}

func TestRetryThenSuccess(t *testing.T) {
	p, _ := New(Options{Interval: 30 * time.Second, Jitter: 0, MaxRetries: 2, Logger: discardLogger()})
	p.interval = 20 * time.Millisecond
	p.retryBase = 5 * time.Millisecond
	var attempts int32
	var cycles int32
	var succeeded int32
	p.SetOnCycle(func(err error) {
		atomic.AddInt32(&cycles, 1)
		if err == nil {
			atomic.AddInt32(&succeeded, 1)
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		p.Run(ctx, func(ctx context.Context) error {
			if atomic.AddInt32(&attempts, 1) == 1 {
				return temporaryError{}
			}
			return nil
		})
		close(done)
	}()
	deadline := time.After(3 * time.Second)
	for atomic.LoadInt32(&succeeded) == 0 {
		select {
		case <-deadline:
			cancel()
			t.Fatalf("no success; attempts=%d cycles=%d", atomic.LoadInt32(&attempts), atomic.LoadInt32(&cycles))
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	<-done
	if got := atomic.LoadInt32(&attempts); got != 2 {
		t.Fatalf("attempts = %d", got)
	}
	if got := atomic.LoadInt32(&cycles); got != 1 {
		t.Fatalf("cycles = %d", got)
	}
}

type throttleError struct{ d time.Duration }

func (e throttleError) Error() string                  { return "throttled" }
func (e throttleError) IsRetryable() bool              { return true }
func (e throttleError) RetryAfterValue() time.Duration { return e.d }

func TestRetryAfterStopsCycle(t *testing.T) {
	p, _ := New(Options{Interval: 30 * time.Second, Jitter: 0, MaxRetries: 5, Logger: discardLogger()})
	p.interval = 20 * time.Millisecond
	var attempts int32
	cycle := make(chan struct{}, 1)
	p.SetOnCycle(func(err error) {
		select {
		case cycle <- struct{}{}:
		default:
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx, func(ctx context.Context) error {
		atomic.AddInt32(&attempts, 1)
		return throttleError{d: 30 * time.Millisecond}
	})
	select {
	case <-cycle:
	case <-time.After(2 * time.Second):
		t.Fatal("no cycle")
	}
	time.Sleep(20 * time.Millisecond)
	cancel()
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Fatalf("attempts = %d", got)
	}
}

func TestPauseResume(t *testing.T) {
	p, _ := New(Options{Interval: 30 * time.Second, Jitter: 0, Logger: discardLogger()})
	p.interval = 15 * time.Millisecond
	var runs int32
	first := make(chan struct{}, 1)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		p.Run(ctx, func(ctx context.Context) error {
			atomic.AddInt32(&runs, 1)
			select {
			case first <- struct{}{}:
			default:
			}
			return nil
		})
		close(done)
	}()
	<-first
	p.Pause()
	time.Sleep(80 * time.Millisecond)
	pausedRuns := atomic.LoadInt32(&runs)
	if pausedRuns > 2 {
		t.Fatalf("poller kept running while paused: %d", pausedRuns)
	}
	p.Resume()
	deadline := time.After(2 * time.Second)
	for atomic.LoadInt32(&runs) <= pausedRuns {
		select {
		case <-deadline:
			cancel()
			t.Fatal("did not resume")
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	<-done
}

func TestNotModifiedCounter(t *testing.T) {
	p, _ := New(Options{Interval: 30 * time.Second, Jitter: 0, Logger: discardLogger()})
	p.NotModified()
	if p.Metrics().NotModified != 1 {
		t.Fatal("counter")
	}
}

func TestSetOnCycleOnSuccess(t *testing.T) {
	p, _ := New(Options{Interval: 30 * time.Second, Jitter: 0, Logger: discardLogger()})
	p.interval = 10 * time.Millisecond
	cycle := make(chan error, 1)
	p.SetOnCycle(func(err error) { cycle <- err })
	ctx, cancel := context.WithCancel(context.Background())
	go p.Run(ctx, func(ctx context.Context) error { return nil })
	select {
	case err := <-cycle:
		if err != nil {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no callback")
	}
	cancel()
}
