// Package polling schedules collection attempts with context cancellation,
// bounded retries and exponential backoff. It does not know about the TSE or
// about Telegram; the application supplies the task.
package polling

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"
)

// Task performs one collection cycle. Returning nil means the cycle succeeded,
// including a valid HTTP 304 with no new data.
type Task func(ctx context.Context) error

// CommandType is a control message accepted while the poller is running.
type CommandType int

const (
	CommandPause CommandType = iota
	CommandResume
	CommandRefresh
	CommandSetInterval
)

// Command controls the running poller.
type Command struct {
	Type     CommandType
	Interval time.Duration
}

// Options configures a Poller.
type Options struct {
	Interval   time.Duration
	MinBackoff time.Duration
	MaxBackoff time.Duration
	MaxRetries int
	Jitter     float64
	Logger     *slog.Logger
}

// Metrics is a point-in-time view for the UI and status endpoint.
type Metrics struct {
	Started             bool          `json:"started"`
	Paused              bool          `json:"paused"`
	Running             bool          `json:"running"`
	Runs                int64         `json:"runs"`
	Successes           int64         `json:"successes"`
	NotModified         int64         `json:"notModified"`
	Failures            int64         `json:"failures"`
	ConsecutiveFailures int           `json:"consecutiveFailures"`
	LastRun             time.Time     `json:"lastRun,omitempty"`
	LastSuccess         time.Time     `json:"lastSuccess,omitempty"`
	LastFailure         time.Time     `json:"lastFailure,omitempty"`
	LastError           string        `json:"lastError,omitempty"`
	NextRun             time.Time     `json:"nextRun,omitempty"`
	Interval            time.Duration `json:"intervalNanos"`
}

type retryAfterError interface {
	RetryAfterValue() time.Duration
}

type retryableError interface {
	IsRetryable() bool
}

// Poller owns the scheduling loop.
type Poller struct {
	interval   time.Duration
	minBackoff time.Duration
	maxBackoff time.Duration
	maxRetries int
	jitter     float64
	log        *slog.Logger
	now        func() time.Time
	onCycle    func(error)
	retryBase  time.Duration

	commands chan Command

	mu                  sync.Mutex
	started             bool
	paused              bool
	running             bool
	runs                int64
	successes           int64
	notModified         int64
	failures            int64
	consecutiveFailures int
	lastRun             time.Time
	lastSuccess         time.Time
	lastFailure         time.Time
	lastError           string
	nextRun             time.Time
}

// New validates options and builds a poller.
func New(opt Options) (*Poller, error) {
	if opt.Interval < 30*time.Second {
		return nil, errors.New("intervalo de polling deve ser de pelo menos 30s")
	}
	if opt.MinBackoff <= 0 {
		opt.MinBackoff = 5 * time.Second
	}
	if opt.MaxBackoff < opt.MinBackoff {
		opt.MaxBackoff = 5 * time.Minute
	}
	if opt.MaxRetries < 0 {
		opt.MaxRetries = 0
	}
	if opt.MaxRetries == 0 {
		opt.MaxRetries = 2
	}
	if opt.Jitter < 0 || opt.Jitter > 0.5 {
		opt.Jitter = 0.05
	}
	if opt.Logger == nil {
		opt.Logger = slog.Default()
	}
	return &Poller{
		interval:   opt.Interval,
		minBackoff: opt.MinBackoff,
		maxBackoff: opt.MaxBackoff,
		maxRetries: opt.MaxRetries,
		jitter:     opt.Jitter,
		log:        opt.Logger,
		now:        time.Now,
		commands:   make(chan Command, 16),
	}, nil
}

// SetOnCycle installs a callback invoked once per cycle with nil on success or
// the final error. It must be set before Run.
func (p *Poller) SetOnCycle(fn func(error)) { p.onCycle = fn }

// Send delivers a control command without blocking the caller (usually the UI).
func (p *Poller) Send(cmd Command) {
	select {
	case p.commands <- cmd:
	default:
		p.log.Warn("comando de polling descartado: fila cheia", "comando", cmd.Type)
	}
}

// Pause stops scheduled collections; an in-flight collection is not aborted.
func (p *Poller) Pause()   { p.Send(Command{Type: CommandPause}) }
func (p *Poller) Resume()  { p.Send(Command{Type: CommandResume}) }
func (p *Poller) Refresh() { p.Send(Command{Type: CommandRefresh}) }

// NotModified records a conditional request that returned 304.
func (p *Poller) NotModified() {
	p.mu.Lock()
	p.notModified++
	p.mu.Unlock()
}

// Metrics returns a consistent copy of counters.
func (p *Poller) Metrics() Metrics {
	p.mu.Lock()
	defer p.mu.Unlock()
	return Metrics{
		Started:             p.started,
		Paused:              p.paused,
		Running:             p.running,
		Runs:                p.runs,
		Successes:           p.successes,
		NotModified:         p.notModified,
		Failures:            p.failures,
		ConsecutiveFailures: p.consecutiveFailures,
		LastRun:             p.lastRun,
		LastSuccess:         p.lastSuccess,
		LastFailure:         p.lastFailure,
		LastError:           p.lastError,
		NextRun:             p.nextRun,
		Interval:            p.interval,
	}
}

// Run blocks until ctx is cancelled. The first collection starts immediately.
func (p *Poller) Run(ctx context.Context, task Task) {
	p.mu.Lock()
	p.started = true
	p.mu.Unlock()

	runNow := true
	forceOnce := false
	failureDelay := time.Duration(0)
	for {
		if ctx.Err() != nil {
			return
		}
		if runNow {
			p.mu.Lock()
			paused := p.paused
			p.mu.Unlock()
			if !paused || forceOnce {
				failureDelay = p.runCycle(ctx, task)
				forceOnce = false
			}
			runNow = false
		}
		if ctx.Err() != nil {
			return
		}

		p.mu.Lock()
		paused := p.paused
		interval := p.interval
		backoff := failureDelay
		p.mu.Unlock()

		wait := interval
		if backoff > 0 {
			wait = backoff
		}
		if paused {
			wait = 24 * time.Hour
		}
		p.mu.Lock()
		p.nextRun = p.now().Add(wait)
		p.mu.Unlock()

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case cmd := <-p.commands:
			timer.Stop()
			switch cmd.Type {
			case CommandPause:
				p.mu.Lock()
				p.paused = true
				p.mu.Unlock()
				p.log.Info("polling pausado")
			case CommandResume:
				p.mu.Lock()
				p.paused = false
				p.mu.Unlock()
				p.log.Info("polling retomado")
				failureDelay = 0
				runNow = true
			case CommandRefresh:
				failureDelay = 0
				forceOnce = true
				runNow = true
			case CommandSetInterval:
				if cmd.Interval >= 30*time.Second {
					p.mu.Lock()
					p.interval = cmd.Interval
					p.mu.Unlock()
					p.log.Info("intervalo de polling alterado", "intervalo", cmd.Interval)
				}
				runNow = false
			}
		case <-timer.C:
			runNow = true
			failureDelay = 0
		}
	}
}

// runCycle performs one task with bounded retries. It returns a suggested
// failure delay (zero on success).
func (p *Poller) runCycle(ctx context.Context, task Task) time.Duration {
	p.mu.Lock()
	p.running = true
	p.runs++
	p.lastRun = p.now()
	attempt := 0
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.running = false
		p.mu.Unlock()
	}()

	var lastErr error
	for {
		attempt++
		err := task(ctx)
		if err == nil {
			p.mu.Lock()
			p.successes++
			p.consecutiveFailures = 0
			p.lastSuccess = p.now()
			p.lastError = ""
			p.mu.Unlock()
			if p.onCycle != nil {
				p.onCycle(nil)
			}
			return 0
		}
		lastErr = err
		if ctx.Err() != nil {
			return 0
		}
		if ra := retryAfter(err); ra > 0 {
			// Honor the server's request and do not hammer the CDN.
			p.recordFailure(err)
			if p.onCycle != nil {
				p.onCycle(err)
			}
			return ra
		}
		if !isRetryable(err) || attempt > p.maxRetries {
			break
		}
		delay := p.retryDelay(attempt)
		p.log.Warn("falha na coleta; retentando", "tentativa", attempt, "espera", delay, "erro", err)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return 0
		case <-timer.C:
		}
	}
	p.recordFailure(lastErr)
	if p.onCycle != nil {
		p.onCycle(lastErr)
	}
	p.mu.Lock()
	streak := p.consecutiveFailures
	p.mu.Unlock()
	return p.cycleBackoff(streak)
}

func (p *Poller) recordFailure(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.failures++
	p.consecutiveFailures++
	p.lastFailure = p.now()
	p.lastError = err.Error()
}

func (p *Poller) retryDelay(attempt int) time.Duration {
	d := p.retryBase
	for i := 1; i < attempt; i++ {
		d *= 2
		if d > 15*time.Second {
			d = 15 * time.Second
			break
		}
	}
	return jitter(d, p.jitter)
}

func (p *Poller) cycleBackoff(streak int) time.Duration {
	if streak < 1 {
		streak = 1
	}
	d := p.minBackoff
	for i := 1; i < streak; i++ {
		if d >= p.maxBackoff/2 {
			d = p.maxBackoff
			break
		}
		d *= 2
	}
	if d > p.maxBackoff {
		d = p.maxBackoff
	}
	return jitter(d, p.jitter)
}

func jitter(d time.Duration, fraction float64) time.Duration {
	if fraction <= 0 {
		return d
	}
	f := 1 + fraction*(rand.Float64()*2-1)
	out := time.Duration(float64(d) * f)
	if out < time.Second {
		return time.Second
	}
	return out
}

func retryAfter(err error) time.Duration {
	var ra retryAfterError
	if errors.As(err, &ra) {
		return ra.RetryAfterValue()
	}
	return 0
}

func isRetryable(err error) bool {
	var r retryableError
	if errors.As(err, &r) {
		return r.IsRetryable()
	}
	return true
}
