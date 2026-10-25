package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tofoli/urna-watch/internal/app"
	"github.com/tofoli/urna-watch/internal/config"
	"github.com/tofoli/urna-watch/internal/domain"
	"github.com/tofoli/urna-watch/internal/observability"
	"github.com/tofoli/urna-watch/internal/store"
)

const (
	configPath = "/oficial/comum/config/ele-c.json"
	resultPath = "/oficial/ele2026/6258/dados/br/br-c0001-e006258-u.json"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "tests", "fixtures", "tse", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

type tseServer struct {
	srv     *httptest.Server
	version atomic.Int64
	body    []byte
	mu      sync.Mutex
}

func newTSEServer(t *testing.T) *tseServer {
	t.Helper()
	ts := &tseServer{body: fixture(t, "br-c0001-e006258-u.json")}
	ts.version.Store(1)
	mux := http.NewServeMux()
	mux.HandleFunc(configPath, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture(t, "ele-c.json"))
	})
	resultHandler := func(w http.ResponseWriter, r *http.Request) {
		v := ts.version.Load()
		etag := `"v` + itoa64(v) + `"`
		if r.Header.Get("If-None-Match") == etag {
			w.Header().Set("ETag", etag)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		ts.mu.Lock()
		body := append([]byte(nil), ts.body...)
		ts.mu.Unlock()
		w.Header().Set("ETag", etag)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}
	mux.HandleFunc(resultPath, resultHandler)
	mux.HandleFunc("/oficial/ele2026/6258/dados/sp/sp-c0001-e006258-u.json", resultHandler)
	ts.srv = httptest.NewServer(mux)
	t.Cleanup(ts.srv.Close)
	return ts
}

func (ts *tseServer) advance(t *testing.T, votes int64) {
	t.Helper()
	ts.mu.Lock()
	defer ts.mu.Unlock()
	var root map[string]any
	if err := json.Unmarshal(ts.body, &root); err != nil {
		t.Fatal(err)
	}
	v := ts.version.Add(1)
	root["idg"] = "9000" + itoa(int(v))
	carg := root["carg"].([]any)[0].(map[string]any)
	agr := carg["agr"].([]any)[0].(map[string]any)
	par := agr["par"].([]any)[0].(map[string]any)
	cand := par["cand"].([]any)[0].(map[string]any)
	cand["vap"] = itoa64(votes)
	cand["pvap"] = "58,00"
	cand["pvapn"] = "58.000000000"
	out, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	ts.body = out
}

func itoa(v int) string { return itoa64(int64(v)) }

func itoa64(v int64) string {
	if v == 0 {
		return "0"
	}
	var b [24]byte
	i := len(b)
	neg := v < 0
	if neg {
		v = -v
	}
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func testAppConfig(t *testing.T, tseURL, statePath string) config.Config {
	t.Helper()
	cfg := config.Default()
	cfg.TSEBaseURL = tseURL
	cfg.StatePath = statePath
	cfg.PollInterval = 30 * time.Second
	cfg.ConfigTTL = time.Hour
	cfg.Telegram.Enabled = false
	cfg.TUI = false
	return cfg
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func loadState(t *testing.T, path string) store.State {
	t.Helper()
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return st.Snapshot()
}

func TestPipelineBaselineChangeAndDedupe(t *testing.T) {
	ts := newTSEServer(t)
	statePath := filepath.Join(t.TempDir(), "state.json")
	cfg := testAppConfig(t, ts.srv.URL, statePath)
	a, err := app.New(cfg, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := a.ProcessOnce(ctx); err != nil {
		t.Fatalf("baseline: %v", err)
	}
	state := loadState(t, statePath)
	if state.Snapshot == nil || state.Snapshot.SourceVersion != "3948414" {
		t.Fatalf("snapshot = %+v", state.Snapshot)
	}
	if state.Snapshot.Election.Scope.Municipality != "" {
		t.Fatalf("brazil scope leaked municipality: %+v", state.Snapshot.Election.Scope)
	}
	// Restarting with the same selection must accept the persisted state.
	if _, err := app.New(cfg, discardLogger()); err != nil {
		t.Fatalf("restart refused valid state: %v", err)
	}
	if len(state.Events) != 1 || !state.Events[0].Baseline {
		t.Fatalf("events = %+v", state.Events)
	}
	if len(state.History) != 1 {
		t.Fatalf("history = %+v", state.History)
	}
	if state.Events[0].NotificationStatus != domain.NotifyDisabled {
		t.Fatalf("status = %q", state.Events[0].NotificationStatus)
	}

	// Same data, conditional request -> 304, no new event.
	if err := a.ProcessOnce(ctx); err != nil {
		t.Fatalf("second: %v", err)
	}
	state = loadState(t, statePath)
	if len(state.Events) != 1 {
		t.Fatalf("304 created events: %+v", state.Events)
	}
	if len(state.History) != 1 {
		t.Fatalf("304 added history: %+v", state.History)
	}

	ts.advance(t, 143169)
	if err := a.ProcessOnce(ctx); err != nil {
		t.Fatalf("change: %v", err)
	}
	state = loadState(t, statePath)
	if len(state.Events) != 2 {
		t.Fatalf("events after change = %d", len(state.Events))
	}
	if len(state.History) != 2 {
		t.Fatalf("history after change = %d", len(state.History))
	}
	if state.Events[0].Summary == "" || state.Events[0].SourceVersion != "90002" {
		t.Fatalf("event = %+v", state.Events[0])
	}
	foundVotes := false
	for _, c := range state.Snapshot.Candidates {
		if c.Number == "22" && c.Votes.Value == 143169 {
			foundVotes = true
		}
	}
	if !foundVotes {
		t.Fatalf("votes = %+v", state.Snapshot.Candidates)
	}

	// Repeating the same source generation must not duplicate the event.
	if err := a.ProcessOnce(ctx); err != nil {
		t.Fatalf("repeat: %v", err)
	}
	state = loadState(t, statePath)
	if len(state.Events) != 2 {
		t.Fatalf("dedupe failed: %+v", state.Events)
	}

	reopened, err := store.Open(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.Snapshot(); got.Snapshot == nil || len(got.Events) != 2 || len(got.History) != 2 {
		t.Fatalf("reopened state = %+v", got)
	}
}

func TestUFBreakdownIsReadOnlyAndDoesNotTouchState(t *testing.T) {
	ts := newTSEServer(t)
	statePath := filepath.Join(t.TempDir(), "state.json")
	cfg := testAppConfig(t, ts.srv.URL, statePath)
	a, err := app.New(cfg, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	if err := a.ProcessOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	before := loadState(t, statePath)
	a.SelectUF("sp")
	deadline := time.Now().Add(5 * time.Second)
	var view *observability.UFView
	for {
		st := a.Status()
		view = st.UFView
		if view != nil && !view.Loading {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("UF view did not load: %+v", view)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if view.Error != "" || view.Snapshot == nil {
		t.Fatalf("UF view = %+v", view)
	}
	if view.Snapshot.Election.Scope.Kind != domain.ScopeState || view.Snapshot.Election.Scope.UF != "sp" {
		t.Fatalf("scope = %+v", view.Snapshot.Election.Scope)
	}
	after := loadState(t, statePath)
	if after.Snapshot.SourceVersion != before.Snapshot.SourceVersion || len(after.Events) != len(before.Events) {
		t.Fatalf("UF view mutated persisted state: before=%+v after=%+v", before.Snapshot, after.Snapshot)
	}
	a.ClearUF()
	if a.Status().UFView != nil {
		t.Fatal("UF view not cleared")
	}
}

func TestTUIMapLoadIsReadOnly(t *testing.T) {
	ts := newTSEServer(t)
	statePath := filepath.Join(t.TempDir(), "state.json")
	cfg := testAppConfig(t, ts.srv.URL, statePath)
	a, err := app.New(cfg, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	if err := a.ProcessOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	before := loadState(t, statePath)
	a.LoadMap()
	deadline := time.Now().Add(5 * time.Second)
	var mv *observability.MapView
	for {
		mv = a.Status().Map
		if mv != nil && !mv.Loading {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("map did not load: %+v", mv)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if mv.Error != "" {
		t.Fatalf("map error: %s", mv.Error)
	}
	sp, ok := mv.States["sp"]
	if !ok || !sp.Counted || sp.WinnerNumber == "" {
		t.Fatalf("SP state = %+v", sp)
	}
	after := loadState(t, statePath)
	if after.Snapshot.SourceVersion != before.Snapshot.SourceVersion || len(after.Events) != len(before.Events) {
		t.Fatalf("map load changed persisted state")
	}
}

func TestPipelineTelegramDelivery(t *testing.T) {
	ts := newTSEServer(t)
	statePath := filepath.Join(t.TempDir(), "state.json")

	type tgMessage struct {
		ChatID string `json:"chat_id"`
		Text   string `json:"text"`
		Photo  bool   `json:"-"`
	}
	received := make(chan tgMessage, 4)
	tg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/sendPhoto"):
			if err := r.ParseMultipartForm(8 << 20); err != nil {
				t.Errorf("telegram multipart: %v", err)
				http.Error(w, "bad form", http.StatusBadRequest)
				return
			}
			file, _, err := r.FormFile("photo")
			if err != nil {
				t.Errorf("telegram photo: %v", err)
				http.Error(w, "no photo", http.StatusBadRequest)
				return
			}
			head := make([]byte, 8)
			n, _ := io.ReadFull(file, head)
			file.Close()
			if n != 8 || !bytes.HasPrefix(head, []byte("\x89PNG\r\n\x1a\n")) {
				t.Errorf("telegram photo is not PNG: %x", head)
			}
			received <- tgMessage{ChatID: r.FormValue("chat_id"), Text: r.FormValue("caption"), Photo: true}
		case strings.HasSuffix(r.URL.Path, "/sendMessage"):
			var msg tgMessage
			if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
				t.Errorf("telegram body: %v", err)
			}
			received <- msg
		default:
			t.Errorf("telegram path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":7}}`))
	}))
	defer tg.Close()

	cfg := testAppConfig(t, ts.srv.URL, statePath)
	cfg.Telegram.Enabled = true
	cfg.Telegram.BotToken = "123:test"
	cfg.Telegram.ChatID = "-100999"
	cfg.Telegram.APIBase = tg.URL
	cfg.Telegram.Baseline = true
	a, err := app.New(cfg, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		a.Run(ctx)
		close(done)
	}()

	var msg tgMessage
	select {
	case msg = <-received:
	case <-time.After(8 * time.Second):
		cancel()
		t.Fatal("baseline message was not delivered")
	}
	if msg.ChatID != "-100999" {
		t.Fatalf("chat = %q", msg.ChatID)
	}
	if !msg.Photo {
		t.Fatal("expected a chart sent via sendPhoto")
	}
	for _, want := range []string{"primeiro snapshot", "LULA", "193.574", "Ver no site do TSE"} {
		if !strings.Contains(msg.Text, want) {
			t.Fatalf("message missing %q:\n%s", want, msg.Text)
		}
	}
	// The worker persists "sent" after the fake API confirms.
	deadline := time.Now().Add(3 * time.Second)
	for {
		st, err := store.Open(statePath)
		if err != nil {
			t.Fatal(err)
		}
		state := st.Snapshot()
		if len(state.Events) == 1 && state.Events[0].NotificationStatus == domain.NotifySent && state.PendingCount() == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("outbox not marked sent: events=%+v queue=%+v", state.Events, state.Queue)
		}
		time.Sleep(50 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("app did not stop")
	}
}
