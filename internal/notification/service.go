package notification

import (
	"context"
	"crypto/rand"
	"errors"
	"log/slog"
	"math/big"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tofoli/urna-watch/internal/domain"
	"github.com/tofoli/urna-watch/internal/store"
	"github.com/tofoli/urna-watch/internal/telegram"
)

// Sender is the subset of the Telegram client needed by the outbox worker.
type Sender interface {
	SendMessage(ctx context.Context, text string) (int, error)
	SendPhoto(ctx context.Context, caption, path string) (int, error)
}

// Options configures the durable notification service.
type Options struct {
	Store       *store.FileStore
	Sender      Sender
	Logger      *slog.Logger
	MaxAttempts int
	BaseBackoff time.Duration
	MaxBackoff  time.Duration
	SendTimeout time.Duration
	Jitter      float64
}

// Service owns the outbox worker. It never blocks the collection loop.
type Service struct {
	store       *store.FileStore
	sender      Sender
	log         *slog.Logger
	maxAttempts int
	baseBackoff time.Duration
	maxBackoff  time.Duration
	sendTimeout time.Duration
	jitter      float64
	wake        chan struct{}
	now         func() time.Time

	mu         sync.Mutex
	lastSent   time.Time
	lastError  time.Time
	lastErrMsg string
	started    bool
}

// NewService validates options. A nil sender means Telegram is disabled; the
// outbox worker still exists but is not started by the application.
func NewService(opt Options) (*Service, error) {
	if opt.Store == nil {
		return nil, errors.New("notification service requires a store")
	}
	if opt.Logger == nil {
		opt.Logger = slog.Default()
	}
	if opt.MaxAttempts <= 0 {
		opt.MaxAttempts = 8
	}
	if opt.BaseBackoff <= 0 {
		opt.BaseBackoff = 5 * time.Second
	}
	if opt.MaxBackoff <= 0 {
		opt.MaxBackoff = 30 * time.Minute
	}
	if opt.SendTimeout <= 0 {
		opt.SendTimeout = 30 * time.Second
	}
	if opt.Jitter < 0 || opt.Jitter > 1 {
		opt.Jitter = 0.2
	}
	return &Service{
		store:       opt.Store,
		sender:      opt.Sender,
		log:         opt.Logger,
		maxAttempts: opt.MaxAttempts,
		baseBackoff: opt.BaseBackoff,
		maxBackoff:  opt.MaxBackoff,
		sendTimeout: opt.SendTimeout,
		jitter:      opt.Jitter,
		wake:        make(chan struct{}, 1),
		now:         time.Now,
	}, nil
}

// Enabled reports whether a Telegram sender is configured.
func (s *Service) Enabled() bool { return s.sender != nil }

// Wake asks the worker to check the outbox immediately.
func (s *Service) Wake() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// SuppressPending is called when Telegram is disabled so that old rows are
// never delivered after a later restart.
func (s *Service) SuppressPending() error {
	now := s.now()
	return s.store.Update(context.Background(), func(st *store.State) error {
		st.SuppressPending(now)
		for i := range st.Events {
			if st.Events[i].NotificationStatus == domain.NotifyPending {
				st.Events[i].NotificationStatus = domain.NotifySuppressed
			}
		}
		return nil
	})
}

// EnqueueEvent splits a rendered message and inserts deterministic outbox
// rows. Re-enqueuing the same event is a no-op.
func (s *Service) EnqueueEvent(event domain.EventRecord, text string) (bool, error) {
	if s.sender == nil {
		_ = s.store.Update(context.Background(), func(st *store.State) error {
			for i := range st.Events {
				if st.Events[i].ID == event.ID {
					st.Events[i].NotificationStatus = domain.NotifyDisabled
				}
			}
			return nil
		})
		return false, nil
	}
	rows := BuildRows(event.ID, text, s.now())
	if len(rows) == 0 {
		return false, nil
	}
	inserted := false
	err := s.store.Update(context.Background(), func(st *store.State) error {
		for _, n := range rows {
			if st.AddNotification(n) {
				inserted = true
			}
		}
		for i := range st.Events {
			if st.Events[i].ID == event.ID && inserted {
				st.Events[i].NotificationStatus = domain.NotifyPending
			}
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	if inserted {
		s.Wake()
	}
	return inserted, nil
}

// BuildRows splits a message into deterministic outbox rows. Callers that need
// an atomic event+outbox write (the application) can use this directly.
func BuildRows(eventID, text string, now time.Time) []store.Notification {
	chunks := telegram.SplitMessage(strings.TrimSpace(text), telegram.MaxMessageLength)
	if len(chunks) == 0 {
		return nil
	}
	rows := make([]store.Notification, 0, len(chunks))
	for i, chunk := range chunks {
		rows = append(rows, store.Notification{
			ID:            eventID + "-" + strconv.Itoa(i),
			Kind:          "text",
			EventID:       eventID,
			Chunk:         i + 1,
			Chunks:        len(chunks),
			Text:          chunk,
			CreatedAt:     now,
			NextAttemptAt: now,
			State:         domain.NotifyPending,
		})
	}
	return rows
}

// PhotoRows returns a single photo notification with the caption. If the
// caption is too long for Telegram, the caller should fall back to BuildRows.
func PhotoRows(eventID, caption, mediaPath string, now time.Time) []store.Notification {
	return []store.Notification{{
		ID:            eventID + "-0",
		Kind:          "photo",
		MediaPath:     mediaPath,
		EventID:       eventID,
		Chunk:         1,
		Chunks:        1,
		Text:          caption,
		CreatedAt:     now,
		NextAttemptAt: now,
		State:         domain.NotifyPending,
	}}
}

// Run is the outbox worker loop. It should run in its own goroutine.
func (s *Service) Run(ctx context.Context) {
	if s.sender == nil {
		return
	}
	s.mu.Lock()
	s.started = true
	s.mu.Unlock()
	pruneTicker := time.NewTicker(time.Hour)
	defer pruneTicker.Stop()
	if err := s.prune(); err != nil {
		s.log.Warn("falha ao aplicar retenção do outbox", "erro", err)
	}
	for {
		if ctx.Err() != nil {
			return
		}
		due := s.due()
		if len(due) == 0 {
			wait := s.nextWait()
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-s.wake:
				timer.Stop()
			case <-timer.C:
			case <-pruneTicker.C:
				timer.Stop()
				if err := s.prune(); err != nil {
					s.log.Warn("falha ao aplicar retenção do outbox", "erro", err)
				}
			}
			continue
		}
		for _, item := range due {
			if ctx.Err() != nil {
				return
			}
			s.process(ctx, item)
		}
	}
}

func (s *Service) due() []store.Notification {
	st := s.store.Snapshot()
	return st.DueNotifications(s.now())
}

func (s *Service) nextWait() time.Duration {
	st := s.store.Snapshot()
	now := s.now()
	var next time.Time
	for _, n := range st.Queue {
		if n.State != domain.NotifyPending {
			continue
		}
		if next.IsZero() || n.NextAttemptAt.Before(next) {
			next = n.NextAttemptAt
		}
	}
	if next.IsZero() {
		return 5 * time.Second
	}
	d := next.Sub(now)
	if d < time.Second {
		return time.Second
	}
	if d > 5*time.Second {
		return 5 * time.Second
	}
	return d
}

func (s *Service) process(parent context.Context, item store.Notification) {
	ctx, cancel := context.WithTimeout(parent, s.sendTimeout)
	defer cancel()
	start := s.now()
	var msgID int
	var err error
	if item.Kind == "photo" {
		msgID, err = s.sender.SendPhoto(ctx, item.Text, item.MediaPath)
	} else {
		msgID, err = s.sender.SendMessage(ctx, item.Text)
	}
	attempts := item.Attempts + 1
	if err == nil {
		now := s.now()
		_ = s.store.Update(context.Background(), func(st *store.State) error {
			st.UpdateNotification(item.ID, func(n *store.Notification) {
				n.Attempts = attempts
				n.State = domain.NotifySent
				n.SentAt = &now
				n.MessageID = msgID
				n.LastError = ""
				n.NextAttemptAt = now
			})
			return nil
		})
		s.mu.Lock()
		s.lastSent = now
		s.lastErrMsg = ""
		s.mu.Unlock()
		if item.Kind == "photo" && item.MediaPath != "" {
			if err := os.Remove(item.MediaPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				s.log.Warn("não foi possível remover o gráfico enviado", "caminho", item.MediaPath, "erro", err)
			}
		}
		kind := item.Kind
		if kind == "" {
			kind = "text"
		}
		s.log.Info("notificação enviada ao Telegram",
			"evento", item.EventID, "parte", item.Chunk, "partes", item.Chunks, "tipo", kind,
			"tentativa", attempts, "latencia", now.Sub(start).String())
		return
	}

	retryAfter := retryAfter(err)
	retryable := isRetryable(err)
	now := s.now()
	delay := retryAfter
	if delay <= 0 {
		delay = s.backoff(attempts)
	}
	permanent := !retryable || attempts >= s.maxAttempts
	next := now.Add(delay)
	errText := truncate(err.Error(), 500)
	_ = s.store.Update(context.Background(), func(st *store.State) error {
		st.UpdateNotification(item.ID, func(n *store.Notification) {
			n.Attempts = attempts
			n.LastError = errText
			n.NextAttemptAt = next
			if permanent {
				n.State = domain.NotifyFailed
			}
		})
		if permanent && item.Kind == "photo" && item.MediaPath != "" {
			_ = os.Remove(item.MediaPath)
		}
		return nil
	})
	s.mu.Lock()
	s.lastError = now
	s.lastErrMsg = errText
	s.mu.Unlock()
	level := slog.LevelWarn
	if permanent {
		level = slog.LevelError
	}
	s.log.Log(ctx, level, "falha ao enviar notificação ao Telegram",
		"evento", item.EventID, "parte", item.Chunk, "tentativa", attempts,
		"permanente", permanent, "proxima_tentativa", next.Format(time.RFC3339), "erro", errText)
}

func (s *Service) backoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	d := s.baseBackoff
	for i := 1; i < attempt; i++ {
		if d >= s.maxBackoff/2 {
			d = s.maxBackoff
			break
		}
		d *= 2
	}
	if d > s.maxBackoff {
		d = s.maxBackoff
	}
	if s.jitter > 0 {
		// Symmetric jitter without introducing a global RNG dependency.
		n, err := rand.Int(rand.Reader, big.NewInt(1000))
		if err == nil {
			factor := 1 + s.jitter*((float64(n.Int64())/500)-1)
			d = time.Duration(float64(d) * factor)
		}
	}
	if d < time.Second {
		d = time.Second
	}
	return d
}

func (s *Service) prune() error {
	now := s.now()
	return s.store.Update(context.Background(), func(st *store.State) error {
		st.Prune(now, 7*24*time.Hour, 30*24*time.Hour)
		return nil
	})
}

// Stats is a snapshot for the UI.
type Stats struct {
	Enabled    bool      `json:"enabled"`
	Pending    int       `json:"pending"`
	LastSent   time.Time `json:"lastSent,omitempty"`
	LastError  time.Time `json:"lastErrorAt,omitempty"`
	LastErrMsg string    `json:"lastErrorMessage,omitempty"`
	Started    bool      `json:"started"`
}

func (s *Service) Stats() Stats {
	st := s.store.Snapshot()
	s.mu.Lock()
	defer s.mu.Unlock()
	lastSent := s.lastSent
	lastError := s.lastError
	lastErrMsg := s.lastErrMsg
	for _, n := range st.Queue {
		if n.SentAt != nil && n.SentAt.After(lastSent) {
			lastSent = *n.SentAt
		}
		if n.LastError != "" && n.CreatedAt.After(lastError) {
			lastError = n.CreatedAt
			lastErrMsg = n.LastError
		}
	}
	return Stats{
		Enabled:    s.sender != nil,
		Pending:    st.PendingCount(),
		LastSent:   lastSent,
		LastError:  lastError,
		LastErrMsg: lastErrMsg,
		Started:    s.started,
	}
}

func retryAfter(err error) time.Duration {
	var withRetry interface{ RetryAfterValue() time.Duration }
	if errors.As(err, &withRetry) {
		return withRetry.RetryAfterValue()
	}
	return 0
}

func isRetryable(err error) bool {
	var retryable interface{ IsRetryable() bool }
	if errors.As(err, &retryable) {
		return retryable.IsRetryable()
	}
	// Network errors, timeouts and context deadline errors are transient.
	return true
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
