package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/tofoli/urna-watch/internal/config"
	"github.com/tofoli/urna-watch/internal/domain"
	"github.com/tofoli/urna-watch/internal/store"
)

func TestSameSelection(t *testing.T) {
	a := domain.ElectionRef{ElectionCode: "6258", ContestCode: "1", Scope: domain.Scope{Kind: domain.ScopeBrazil, UF: "br"}}
	b := a
	b.Cycle = "ele2026"
	if !sameSelection(a, b) {
		t.Fatal("cycle must not matter")
	}
	b.ContestCode = "3"
	if sameSelection(a, b) {
		t.Fatal("different contest")
	}
	b = a
	b.Scope.UF = "sp"
	b.Scope.Kind = domain.ScopeState
	if sameSelection(a, b) {
		t.Fatal("different scope")
	}
}

func TestIsOlderKeepsLastGood(t *testing.T) {
	now := time.Now()
	stored := &domain.Snapshot{TotalizedAt: now}
	incoming := &domain.Snapshot{TotalizedAt: now.Add(-time.Minute)}
	if !isOlder(incoming, stored) {
		t.Fatal("expected older")
	}
	if isOlder(stored, incoming) {
		t.Fatal("newer flagged")
	}
	if isOlder(&domain.Snapshot{}, stored) {
		t.Fatal("zero time must not be treated as older")
	}
}

func TestNewRejectsStateFromAnotherElection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	err = st.Update(context.Background(), func(s *store.State) error {
		s.Ref = domain.ElectionRef{
			ElectionCode: "6259", ContestCode: "3",
			Scope: domain.Scope{Kind: domain.ScopeState, UF: "pi"},
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.StatePath = path
	cfg.Telegram.Enabled = false
	if _, err := New(cfg, nil); err == nil {
		t.Fatal("expected selection mismatch")
	}
}
