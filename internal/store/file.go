package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// FileStore is a single-writer JSON state store with atomic replacement.
// The expected state is small (one snapshot, a bounded event log and a bounded
// outbox), so a database would add operational cost without a demonstrated
// need. See docs/adr/0005-persistence.md.
type FileStore struct {
	Path string

	mu    sync.Mutex
	state State
	now   func() time.Time
}

// Open loads the state file, creating an empty state when it does not exist.
func Open(path string) (*FileStore, error) {
	if path == "" {
		return nil, errors.New("caminho do estado não informado")
	}
	st := &FileStore{Path: path, now: time.Now}
	state, err := loadFile(path)
	if err != nil {
		return nil, err
	}
	state.Ensure(st.now())
	st.state = state
	return st, nil
}

func loadFile(path string) (State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return State{}, nil
		}
		return State{}, fmt.Errorf("ler estado %s: %w", path, err)
	}
	if len(data) == 0 {
		return State{}, nil
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, fmt.Errorf("estado corrompido em %s (faça backup e remova o arquivo para reiniciar): %w", path, err)
	}
	if state.Version > StateVersion {
		return State{}, fmt.Errorf("estado em versão %d não suportada por este binário (máximo %d)", state.Version, StateVersion)
	}
	return state, nil
}

// Snapshot returns a deep copy of the current state.
func (s *FileStore) Snapshot() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return deepCopy(s.state)
}

// Update mutates the state under a lock and persists it atomically. If fn
// returns an error nothing is written.
func (s *FileStore) Update(ctx context.Context, fn func(*State) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	working := deepCopy(s.state)
	if err := fn(&working); err != nil {
		return err
	}
	now := s.now()
	working.Ensure(now)
	working.UpdatedAt = now
	if err := writeFileAtomic(s.Path, working); err != nil {
		return err
	}
	s.state = working
	return nil
}

func deepCopy(in State) State {
	data, _ := json.Marshal(in)
	var out State
	_ = json.Unmarshal(data, &out)
	out.Ensure(time.Now())
	return out
}

func writeFileAtomic(path string, state State) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("criar diretório de estado: %w", err)
	}
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("serializar estado: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".urna-watch-state-*.tmp")
	if err != nil {
		return fmt.Errorf("criar arquivo temporário de estado: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}
	if err := tmp.Chmod(0o600); err != nil {
		cleanup()
		return fmt.Errorf("proteger arquivo de estado: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("escrever estado: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("sincronizar estado: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("fechar estado: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("substituir estado: %w", err)
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}
