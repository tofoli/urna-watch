package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gpu/urna-watch/internal/domain"
)

func TestFileStorePersistsAtomically(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	err = st.Update(context.Background(), func(s *State) error {
		s.Ref = domain.ElectionRef{ElectionCode: "6258", ContestCode: "1", Scope: domain.Scope{Kind: domain.ScopeBrazil, UF: "br"}}
		s.Failures = 2
		s.LastSuccess = now
		s.Cache["k"] = CacheEntry{URL: "https://example.test", ETag: `"x"`, LastFetch: now}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("permissions = %o", info.Mode().Perm())
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	got := reopened.Snapshot()
	if got.Failures != 2 || got.Cache["k"].ETag != `"x"` || got.Ref.ElectionCode != "6258" {
		t.Fatalf("state = %+v", got)
	}
	if entries, err := os.ReadDir(filepath.Dir(path)); err != nil || len(entries) != 1 {
		t.Fatalf("temporary files left behind: %v %v", entries, err)
	}
}

func TestFileStoreRejectsCorruptState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("expected error")
	}
}

func TestFileStoreUpdateRollback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	err = st.Update(context.Background(), func(s *State) error {
		s.Failures = 1
		return context.Canceled
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if st.Snapshot().Failures != 0 {
		t.Fatal("failed update must not mutate state")
	}
}

func TestEventRetentionAndDedupe(t *testing.T) {
	s := State{}
	s.Ensure(time.Now())
	e := domain.EventRecord{ID: "a", DetectedAt: time.Now()}
	if !s.AddEvent(e, 2) {
		t.Fatal("first add")
	}
	if s.AddEvent(e, 2) {
		t.Fatal("duplicate add")
	}
	for i := 0; i < 5; i++ {
		s.AddEvent(domain.EventRecord{ID: string(rune('b' + i)), DetectedAt: time.Now().Add(time.Duration(i) * time.Second)}, 2)
	}
	if len(s.Events) != 2 {
		t.Fatalf("events = %d", len(s.Events))
	}
}

func TestNotificationOutbox(t *testing.T) {
	s := State{}
	now := time.Now()
	s.Ensure(now)
	n := Notification{ID: "e-0", EventID: "e", Text: "hello", CreatedAt: now, NextAttemptAt: now, State: domain.NotifyPending}
	if !s.AddNotification(n) {
		t.Fatal("add")
	}
	if s.AddNotification(n) {
		t.Fatal("duplicate add")
	}
	s.AddEvent(domain.EventRecord{ID: "e"}, 10)
	s.RecomputeEventStatus("e")
	if s.Events[0].NotificationStatus != domain.NotifyPending {
		t.Fatalf("status = %q", s.Events[0].NotificationStatus)
	}
	if got := s.DueNotifications(now); len(got) != 1 {
		t.Fatalf("due = %d", len(got))
	}
	if got := s.DueNotifications(now.Add(-time.Second)); len(got) != 0 {
		t.Fatalf("due before next attempt = %d", len(got))
	}
	s.UpdateNotification("e-0", func(n *Notification) {
		n.State = domain.NotifySent
		sent := now
		n.SentAt = &sent
	})
	if s.Events[0].NotificationStatus != domain.NotifySent {
		t.Fatalf("status after send = %q", s.Events[0].NotificationStatus)
	}
	if s.PendingCount() != 0 {
		t.Fatal("pending count")
	}
}

func TestSuppressPending(t *testing.T) {
	s := State{}
	now := time.Now()
	s.Ensure(now)
	s.AddEvent(domain.EventRecord{ID: "e"}, 10)
	s.AddNotification(Notification{ID: "e-0", EventID: "e", State: domain.NotifyPending, CreatedAt: now})
	s.SuppressPending(now)
	if s.Queue[0].State != domain.NotifySuppressed {
		t.Fatalf("state = %q", s.Queue[0].State)
	}
	if s.Events[0].NotificationStatus != domain.NotifySuppressed {
		t.Fatalf("event = %q", s.Events[0].NotificationStatus)
	}
}
