// Package store persists the last good snapshot, HTTP validators, the event
// audit log and the notification outbox.
package store

import (
	"sort"
	"time"

	"github.com/gpu/urna-watch/internal/domain"
)

const StateVersion = 1

// CacheEntry keeps conditional request validators for one URL.
type CacheEntry struct {
	URL          string    `json:"url"`
	ETag         string    `json:"etag,omitempty"`
	LastModified string    `json:"lastModified,omitempty"`
	LastFetch    time.Time `json:"lastFetch"`
	StatusCode   int       `json:"statusCode,omitempty"`
}

// Notification is one outbox row. A long Telegram message is split into
// deterministic chunks, each with its own row and ID.
type Notification struct {
	ID            string                    `json:"id"`
	Kind          string                    `json:"kind,omitempty"` // ""/"text" or "photo"
	MediaPath     string                    `json:"mediaPath,omitempty"`
	EventID       string                    `json:"eventId"`
	Chunk         int                       `json:"chunk"`
	Chunks        int                       `json:"chunks"`
	Text          string                    `json:"text"`
	CreatedAt     time.Time                 `json:"createdAt"`
	NextAttemptAt time.Time                 `json:"nextAttemptAt"`
	SentAt        *time.Time                `json:"sentAt,omitempty"`
	Attempts      int                       `json:"attempts"`
	State         domain.NotificationStatus `json:"state"`
	LastError     string                    `json:"lastError,omitempty"`
	MessageID     int                       `json:"messageId,omitempty"`
}

// State is the complete durable application state. It contains no secrets.
type State struct {
	Version     int                   `json:"version"`
	CreatedAt   time.Time             `json:"createdAt"`
	UpdatedAt   time.Time             `json:"updatedAt"`
	Ref         domain.ElectionRef    `json:"ref"`
	Snapshot    *domain.Snapshot      `json:"snapshot,omitempty"`
	History     []domain.HistoryPoint `json:"history,omitempty"`
	Cache       map[string]CacheEntry `json:"cache,omitempty"`
	Events      []domain.EventRecord  `json:"events,omitempty"`
	Queue       []Notification        `json:"queue,omitempty"`
	LastSuccess time.Time             `json:"lastSuccess,omitempty"`
	LastFailure time.Time             `json:"lastFailure,omitempty"`
	LastError   string                `json:"lastError,omitempty"`
	Failures    int                   `json:"consecutiveFailures,omitempty"`
}

// Ensure initializes maps and version on a freshly loaded state.
func (s *State) Ensure(now time.Time) {
	if s.Version == 0 {
		s.Version = StateVersion
	}
	if s.CreatedAt.IsZero() {
		s.CreatedAt = now
	}
	if s.Cache == nil {
		s.Cache = map[string]CacheEntry{}
	}
}

// AddHistory appends a compact history point, deduplicating the current source
// generation/fingerprint and enforcing the retention window.
func (s *State) AddHistory(p domain.HistoryPoint, retention int) bool {
	if len(s.History) > 0 {
		last := s.History[len(s.History)-1]
		if last.SourceVersion == p.SourceVersion && last.Fingerprint == p.Fingerprint {
			return false
		}
	}
	s.History = append(s.History, p)
	if retention > 0 && len(s.History) > retention {
		s.History = s.History[len(s.History)-retention:]
	}
	return true
}

// Event returns the event with the given ID.
func (s *State) Event(id string) (domain.EventRecord, bool) {
	for _, e := range s.Events {
		if e.ID == id {
			return e, true
		}
	}
	return domain.EventRecord{}, false
}

// AddEvent appends an event, deduplicating by ID and trimming to retention.
func (s *State) AddEvent(event domain.EventRecord, retention int) bool {
	if _, ok := s.Event(event.ID); ok {
		return false
	}
	s.Events = append(s.Events, event)
	sort.SliceStable(s.Events, func(i, j int) bool {
		return s.Events[i].DetectedAt.After(s.Events[j].DetectedAt)
	})
	if retention > 0 && len(s.Events) > retention {
		s.Events = s.Events[:retention]
	}
	return true
}

// Notification returns the outbox row with the given ID.
func (s *State) Notification(id string) (Notification, bool) {
	for _, n := range s.Queue {
		if n.ID == id {
			return n, true
		}
	}
	return Notification{}, false
}

// AddNotification inserts an outbox row if its deterministic ID is new.
func (s *State) AddNotification(n Notification) bool {
	if _, ok := s.Notification(n.ID); ok {
		return false
	}
	s.Queue = append(s.Queue, n)
	return true
}

// DueNotifications returns pending rows whose next attempt is due.
func (s *State) DueNotifications(now time.Time) []Notification {
	out := make([]Notification, 0)
	for _, n := range s.Queue {
		if n.State == domain.NotifyPending && !n.NextAttemptAt.After(now) {
			out = append(out, n)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// PendingCount returns the number of pending outbox rows.
func (s *State) PendingCount() int {
	n := 0
	for _, item := range s.Queue {
		if item.State == domain.NotifyPending {
			n++
		}
	}
	return n
}

// UpdateNotification applies fn to one outbox row and recomputes the event's
// aggregate notification status.
func (s *State) UpdateNotification(id string, fn func(*Notification)) bool {
	for i := range s.Queue {
		if s.Queue[i].ID != id {
			continue
		}
		fn(&s.Queue[i])
		s.RecomputeEventStatus(s.Queue[i].EventID)
		return true
	}
	return false
}

// RecomputeEventStatus derives pending/sent/failed for an event from its rows.
func (s *State) RecomputeEventStatus(eventID string) {
	if eventID == "" {
		return
	}
	pending, failed, sent, suppressed, total := 0, 0, 0, 0, 0
	for _, n := range s.Queue {
		if n.EventID != eventID {
			continue
		}
		total++
		switch n.State {
		case domain.NotifyPending:
			pending++
		case domain.NotifyFailed:
			failed++
		case domain.NotifySent:
			sent++
		case domain.NotifySuppressed:
			suppressed++
		}
	}
	status := domain.NotificationStatus("")
	switch {
	case total == 0:
		return
	case pending > 0:
		status = domain.NotifyPending
	case failed > 0:
		status = domain.NotifyFailed
	case sent == total:
		status = domain.NotifySent
	case suppressed == total:
		status = domain.NotifySuppressed
	}
	for i := range s.Events {
		if s.Events[i].ID == eventID {
			s.Events[i].NotificationStatus = status
			return
		}
	}
}

// SuppressPending marks every pending row as suppressed. Used when Telegram is
// disabled so stale notifications are never sent after a later restart.
func (s *State) SuppressPending(now time.Time) {
	for i := range s.Queue {
		if s.Queue[i].State == domain.NotifyPending {
			s.Queue[i].State = domain.NotifySuppressed
			s.Queue[i].NextAttemptAt = now
			s.RecomputeEventStatus(s.Queue[i].EventID)
		}
	}
}

// Prune applies retention to outbox rows. Terminal rows older than the
// horizons are removed from disk but event history remains.
func (s *State) Prune(now time.Time, sentHorizon, failedHorizon time.Duration) {
	kept := s.Queue[:0]
	for _, n := range s.Queue {
		switch n.State {
		case domain.NotifySent:
			if n.SentAt != nil && now.Sub(*n.SentAt) > sentHorizon {
				continue
			}
		case domain.NotifyFailed, domain.NotifySuppressed:
			if now.Sub(n.CreatedAt) > failedHorizon {
				continue
			}
		}
		kept = append(kept, n)
	}
	s.Queue = kept
}

// MarkEventStatus sets an event's notification status explicitly.
func (s *State) MarkEventStatus(eventID string, status domain.NotificationStatus) {
	for i := range s.Events {
		if s.Events[i].ID == eventID {
			s.Events[i].NotificationStatus = status
			return
		}
	}
}
