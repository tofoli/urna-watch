// Package app wires the source adapter, polling, detection, persistence and
// notification components. It is independent from the terminal UI and remains
// fully functional in headless mode.
package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gpu/urna-watch/internal/chart"
	"github.com/gpu/urna-watch/internal/config"
	"github.com/gpu/urna-watch/internal/detector"
	"github.com/gpu/urna-watch/internal/domain"
	"github.com/gpu/urna-watch/internal/format"
	"github.com/gpu/urna-watch/internal/notification"
	"github.com/gpu/urna-watch/internal/observability"
	"github.com/gpu/urna-watch/internal/polling"
	"github.com/gpu/urna-watch/internal/store"
	"github.com/gpu/urna-watch/internal/telegram"
	"github.com/gpu/urna-watch/internal/tse"
	"github.com/gpu/urna-watch/internal/version"
)

// App owns all long-lived components.
type App struct {
	cfg     config.Config
	log     *slog.Logger
	adapter *tse.Adapter
	store   *store.FileStore
	notify  *notification.Service
	poller  *polling.Poller
	format  notification.Formatter

	startedAt time.Time
	mediaDir  string
	done      chan struct{}
	closeOnce sync.Once

	mu           sync.Mutex
	source       observability.SourceStatus
	previous     *domain.Snapshot
	lastError    string
	shuttingDown bool
	ufView       *observability.UFView
	ufSeq        int
	ufCache      map[string]ufCacheEntry
	mapStates    map[string]*domain.Snapshot
	mapConds     map[string]tse.Conditional
	mapAt        time.Time
	mapLoadMu    sync.Mutex
	mapSeq       int
	mapView      *observability.MapView
}

// New builds the application. It validates that the persisted state belongs to
// the same election selection; using a different election requires a different
// state file, which prevents cross-election notification bugs.
func New(cfg config.Config, log *slog.Logger) (*App, error) {
	if log == nil {
		log = slog.Default()
	}
	adapter, err := tse.NewAdapter(tse.Options{
		BaseURL:     cfg.TSEBaseURL,
		Environment: cfg.TSEEnvironment,
		UserAgent:   cfg.UserAgent,
		HTTPClient:  tse.DefaultHTTPClient(cfg.HTTPTimeout),
		ConfigTTL:   cfg.ConfigTTL,
		Logger:      log,
	})
	if err != nil {
		return nil, err
	}
	st, err := store.Open(cfg.StatePath)
	if err != nil {
		return nil, err
	}
	existing := st.Snapshot()
	if existing.Ref.ElectionCode != "" && !sameSelection(existing.Ref, cfg.Ref()) {
		return nil, fmt.Errorf("o estado em %s pertence à eleição %s; use outro --state para monitorar %s",
			cfg.StatePath, selectionLabel(existing.Ref), selectionLabel(cfg.Ref()))
	}

	var sender notification.Sender
	if cfg.Telegram.Enabled {
		client, err := telegram.New(cfg.Telegram.BotToken, cfg.Telegram.ChatID, cfg.Telegram.APIBase, nil)
		if err != nil {
			return nil, err
		}
		sender = client
	}
	svc, err := notification.NewService(notification.Options{
		Store:       st,
		Sender:      sender,
		Logger:      log,
		MaxAttempts: cfg.Telegram.MaxAttempts,
		BaseBackoff: cfg.Telegram.BaseBackoff.Std(),
		MaxBackoff:  30 * time.Minute,
	})
	if err != nil {
		return nil, err
	}
	poller, err := polling.New(polling.Options{
		Interval:   cfg.PollInterval,
		MinBackoff: cfg.MinBackoff,
		MaxBackoff: cfg.MaxBackoff,
		MaxRetries: cfg.MaxRetries,
		Logger:     log,
	})
	if err != nil {
		return nil, err
	}
	a := &App{
		cfg:       cfg,
		log:       log,
		adapter:   adapter,
		store:     st,
		notify:    svc,
		poller:    poller,
		format:    notification.Formatter{MaxCandidates: 5, MaxChanges: 6},
		startedAt: time.Now(),
		mediaDir:  filepath.Join(filepath.Dir(cfg.StatePath), "media"),
		done:      make(chan struct{}),
		ufCache:   map[string]ufCacheEntry{},
		mapStates: map[string]*domain.Snapshot{},
		mapConds:  map[string]tse.Conditional{},
		source: observability.SourceStatus{
			URL: "",
		},
	}
	poller.SetOnCycle(func(err error) {
		if err != nil {
			a.recordFailure(err)
		}
	})
	return a, nil
}

// Run blocks until ctx is cancelled.
func (a *App) Run(ctx context.Context) {
	defer a.closeOnce.Do(func() { close(a.done) })
	a.log.Info("Urna Watch iniciado",
		"versao", version.Version,
		"eleicao", a.cfg.Election,
		"cargo", a.cfg.Contest,
		"abrangencia", a.cfg.Scope().Key(),
		"intervalo", a.cfg.PollInterval.String(),
		"tui", a.cfg.TUI,
		"telegram", a.cfg.Telegram.Enabled,
		"estado", a.cfg.StatePath,
	)
	if !a.notify.Enabled() {
		if err := a.notify.SuppressPending(); err != nil {
			a.log.Warn("falha ao suprimir notificações pendentes", "erro", err)
		}
	}
	a.cleanupMedia()
	var wg sync.WaitGroup
	if a.cfg.StatusAddr != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := observability.ServeStatus(ctx, a.cfg.StatusAddr, a.Status, a.log); err != nil && ctx.Err() == nil {
				a.log.Error("servidor de status encerrado", "erro", err)
			}
		}()
	}
	if a.notify.Enabled() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a.notify.Run(ctx)
		}()
	}
	a.poller.Run(ctx, a.collect)
	a.mu.Lock()
	a.shuttingDown = true
	a.mu.Unlock()
	wg.Wait()
	a.log.Info("Urna Watch encerrado")
}

// Done is closed after Run returns.
func (a *App) Done() <-chan struct{} { return a.done }

type ufCacheEntry struct {
	snapshot *domain.Snapshot
	at       time.Time
}

// SelectUF loads a read-only state breakdown on demand. It never changes the
// monitored selection, the stored snapshot, detection or notifications.
func (a *App) SelectUF(uf string) {
	uf = strings.ToLower(strings.TrimSpace(uf))
	if uf == "" || uf == "br" {
		a.ClearUF()
		return
	}
	if len(uf) != 2 {
		a.setUFError(uf, "UF inválida")
		return
	}
	base := a.store.Snapshot().Snapshot
	if base == nil {
		a.setUFError(uf, "aguarde a primeira coleta consolidada")
		return
	}
	if !strings.EqualFold(base.Election.Level, "federal") || a.cfg.Scope().Kind != domain.ScopeBrazil {
		a.setUFError(uf, "visão por UF disponível apenas para eleição federal monitorada em âmbito Brasil")
		return
	}
	name := tse.StateName(uf)
	a.mu.Lock()
	a.ufSeq++
	seq := a.ufSeq
	if c, ok := a.ufCache[uf]; ok && time.Since(c.at) < 30*time.Second && c.snapshot != nil {
		a.ufView = &observability.UFView{UF: uf, Name: name, Snapshot: c.snapshot, FetchedAt: c.at}
		a.mu.Unlock()
		return
	}
	a.ufView = &observability.UFView{UF: uf, Name: name, Loading: true}
	a.mu.Unlock()

	ref := domain.ElectionRef{
		ElectionCode: base.Election.ElectionCode,
		ContestCode:  base.Election.ContestCode,
		Round:        base.Election.Round,
		Scope:        domain.Scope{Kind: domain.ScopeState, UF: uf},
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), a.cfg.HTTPTimeout*2)
		defer cancel()
		res, err := a.adapter.Fetch(ctx, ref, tse.Conditional{})
		if err != nil {
			a.mu.Lock()
			if seq == a.ufSeq {
				a.ufView = &observability.UFView{UF: uf, Name: name, Error: err.Error(), FetchedAt: time.Now()}
			}
			a.mu.Unlock()
			a.log.Warn("falha ao carregar visão por UF", "uf", uf, "erro", err)
			return
		}
		if res.Snapshot == nil {
			a.mu.Lock()
			if seq == a.ufSeq {
				a.ufView = &observability.UFView{UF: uf, Name: name, Error: "fonte não retornou snapshot para a UF", FetchedAt: time.Now()}
			}
			a.mu.Unlock()
			return
		}
		now := time.Now()
		a.mu.Lock()
		a.ufCache[uf] = ufCacheEntry{snapshot: res.Snapshot, at: now}
		if seq == a.ufSeq {
			a.ufView = &observability.UFView{UF: uf, Name: name, Snapshot: res.Snapshot, FetchedAt: now}
		}
		a.mu.Unlock()
		a.log.Debug("visão por UF carregada", "uf", uf, "versao_fonte", res.Snapshot.SourceVersion)
	}()
}

// ClearUF returns to the consolidated Brazil view.
func (a *App) ClearUF() {
	a.mu.Lock()
	a.ufSeq++
	a.ufView = nil
	a.mu.Unlock()
}

func (a *App) setUFError(uf, msg string) {
	a.mu.Lock()
	a.ufSeq++
	a.ufView = &observability.UFView{UF: uf, Name: tse.StateName(uf), Error: msg, FetchedAt: time.Now()}
	a.mu.Unlock()
}

// Pause pauses scheduled polling.
func (a *App) Pause() { a.poller.Pause() }

// Resume resumes polling and triggers an immediate collection.
func (a *App) Resume() { a.poller.Resume() }

// Refresh triggers an immediate collection.
func (a *App) Refresh() { a.poller.Refresh() }

// Status returns the immutable presentation model.
func (a *App) Status() observability.Status {
	st := a.store.Snapshot()
	a.mu.Lock()
	src := a.source
	lastErr := a.lastError
	shutting := a.shuttingDown
	a.mu.Unlock()
	events := make([]domain.EventRecord, len(st.Events))
	copy(events, st.Events)
	history := make([]domain.HistoryPoint, len(st.History))
	copy(history, st.History)
	poll := a.poller.Metrics()
	if st.Failures > 0 {
		poll.ConsecutiveFailures = st.Failures
	}
	if poll.LastSuccess.IsZero() {
		poll.LastSuccess = st.LastSuccess
	}
	if poll.LastFailure.IsZero() {
		poll.LastFailure = st.LastFailure
	}
	if poll.LastError == "" {
		poll.LastError = st.LastError
	}
	if src.LastSuccess.IsZero() {
		src.LastSuccess = st.LastSuccess
	}
	return observability.Status{
		Version:          version.Version,
		StartedAt:        a.startedAt,
		Ref:              st.Ref,
		Snapshot:         st.Snapshot,
		PreviousSnapshot: a.previousSnapshot(),
		UFView:           a.ufViewCopy(),
		Map:              a.mapViewCopy(),
		History:          history,
		Events:           events,
		Poll:             poll,
		Telegram:         a.notify.Stats(),
		Source:           src,
		Paused:           poll.Paused,
		StatePath:        a.cfg.StatePath,
		ConfigPath:       a.cfg.ConfigPath,
		BaseURL:          a.cfg.TSEBaseURL,
		Environment:      a.cfg.TSEEnvironment,
		Timezone:         a.cfg.Timezone,
		Error:            lastErr,
		ShuttingDown:     shutting,
	}
}

// CollectOnce performs one fetch without persisting or notifying. It is used by
// --once and by diagnostics.
func (a *App) CollectOnce(ctx context.Context) (tse.Result, domain.ChangeSet, error) {
	st := a.store.Snapshot()
	key := selectionKey(a.cfg.Ref())
	cond := tse.Conditional{}
	if c, ok := st.Cache[key]; ok {
		cond = tse.Conditional{ETag: c.ETag, LastModified: c.LastModified}
	}
	res, err := a.adapter.Fetch(ctx, a.cfg.Ref(), cond)
	if err != nil {
		return res, domain.ChangeSet{}, err
	}
	if res.NotModified {
		return res, domain.ChangeSet{Previous: st.Snapshot, Current: st.Snapshot}, nil
	}
	set := detector.Compare(st.Snapshot, res.Snapshot)
	return res, set, nil
}

// ProcessOnce performs one full collection cycle (fetch, detect, persist,
// enqueue) without waiting for the poller. It is used by integration tests and
// by diagnostic tooling.
func (a *App) ProcessOnce(ctx context.Context) error { return a.collect(ctx) }

// Validate resolves the official config and fetches the selected result.
func (a *App) Validate(ctx context.Context) (domain.ElectionInfo, *domain.Snapshot, string, error) {
	info, endpoint, err := a.adapter.Resolve(ctx, a.cfg.Ref())
	if err != nil {
		return domain.ElectionInfo{}, nil, endpoint, err
	}
	res, err := a.adapter.Fetch(ctx, a.cfg.Ref(), tse.Conditional{})
	if err != nil {
		return info, nil, endpoint, err
	}
	if res.NotModified {
		return info, nil, endpoint, fmt.Errorf("fonte respondeu 304 sem snapshot em cache (validação sem condicional não deveria ocorrer)")
	}
	return info, res.Snapshot, endpoint, nil
}

// collect is one polling cycle.
func (a *App) collect(ctx context.Context) error {
	st := a.store.Snapshot()
	key := selectionKey(a.cfg.Ref())
	cond := tse.Conditional{}
	if c, ok := st.Cache[key]; ok {
		cond = tse.Conditional{ETag: c.ETag, LastModified: c.LastModified}
	}
	attemptAt := time.Now()
	res, err := a.adapter.Fetch(ctx, a.cfg.Ref(), cond)
	if err != nil {
		a.setSource(observability.SourceStatus{
			URL:         res.URL,
			StatusCode:  res.StatusCode,
			ETag:        res.ETag,
			NotModified: false,
			LastAttempt: attemptAt,
			LastSuccess: st.LastSuccess,
			LastError:   err.Error(),
			Latency:     res.Duration.String(),
		})
		return err
	}
	if res.NotModified {
		a.setSource(observability.SourceStatus{
			URL:          res.URL,
			StatusCode:   res.StatusCode,
			ETag:         res.ETag,
			LastModified: res.LastModified,
			NotModified:  true,
			LastAttempt:  attemptAt,
			LastSuccess:  res.FetchedAt,
			Latency:      res.Duration.String(),
		})
		a.poller.NotModified()
		var events []domain.EventRecord
		var rows []store.Notification
		if st.Snapshot != nil {
			events, rows = a.buildEvents(ctx, st, domain.ChangeSet{Previous: st.Snapshot, Current: st.Snapshot})
		}
		if err := a.persistSuccess(ctx, st, res, events, rows); err != nil {
			return err
		}
		if len(rows) > 0 {
			a.notify.Wake()
		}
		a.mu.Lock()
		a.lastError = ""
		a.mu.Unlock()
		a.log.Debug("fonte sem alterações (HTTP 304)", "url", res.URL, "latencia", res.Duration.String())
		return nil
	}
	snap := res.Snapshot
	if snap == nil {
		return &tse.ParseError{Reason: "resposta 200 sem snapshot"}
	}
	if st.Snapshot != nil && isOlder(snap, st.Snapshot) {
		err := &staleError{stored: st.Snapshot, incoming: snap}
		a.setSource(observability.SourceStatus{
			URL: res.URL, StatusCode: res.StatusCode, ETag: res.ETag, LastModified: res.LastModified,
			LastAttempt: attemptAt, LastSuccess: st.LastSuccess, LastError: err.Error(),
			Latency: res.Duration.String(),
		})
		return err
	}
	set := detector.Compare(st.Snapshot, snap)
	if set.Changed && st.Snapshot != nil {
		a.mu.Lock()
		a.previous = st.Snapshot
		a.mu.Unlock()
	}
	events, rows := a.buildEvents(ctx, st, set)
	a.setSource(observability.SourceStatus{
		URL:          res.URL,
		StatusCode:   res.StatusCode,
		ETag:         res.ETag,
		LastModified: res.LastModified,
		LastAttempt:  attemptAt,
		LastSuccess:  res.FetchedAt,
		Latency:      res.Duration.String(),
	})
	if err := a.persistSuccess(ctx, st, res, events, rows); err != nil {
		return err
	}
	a.mu.Lock()
	a.lastError = ""
	a.mu.Unlock()
	if set.Changed {
		a.log.Info("mudança detectada",
			"versao_fonte", snap.SourceVersion,
			"resumo", detector.Summarize(set),
			"revisao", set.Revision,
			"eventos", len(events),
			"notificacoes", len(rows),
		)
	} else {
		a.log.Debug("coleta sem mudanças relevantes", "versao_fonte", snap.SourceVersion)
	}
	if len(rows) > 0 {
		a.notify.Wake()
	}
	return nil
}

func (a *App) buildEvents(ctx context.Context, st store.State, set domain.ChangeSet) ([]domain.EventRecord, []store.Notification) {
	now := time.Now()
	var events []domain.EventRecord
	var rows []store.Notification
	if set.Changed {
		event := domain.EventRecord{
			ID:            set.EventID(),
			DetectedAt:    now,
			Baseline:      set.Baseline,
			Revision:      set.Revision,
			Summary:       detector.Summarize(set),
			SourceVersion: set.Current.SourceVersion,
			ElectionKey:   selectionKey(a.cfg.Ref()),
			Items:         set.Items,
		}
		if _, exists := st.Event(event.ID); !exists {
			var text string
			if set.Baseline {
				text = a.format.Baseline(set.Current)
			} else {
				text = a.format.Update(set)
			}
			status := domain.NotifyPending
			if !a.notify.Enabled() {
				status = domain.NotifyDisabled
			} else if set.Baseline && !a.cfg.Telegram.Baseline {
				status = domain.NotifySuppressed
			}
			event.NotificationStatus = status
			if status == domain.NotifyPending {
				if photoRows := a.chartRows(ctx, event.ID, text, set.Current, now); photoRows != nil {
					rows = append(rows, photoRows...)
				} else {
					rows = append(rows, notification.BuildRows(event.ID, text, now)...)
				}
			}
			events = append(events, event)
		}
	}

	// Recovery is only emitted once, on the transition from a failure streak
	// at or above the configured threshold to a successful collection.
	if st.Failures >= a.cfg.MaxFailures && a.cfg.Telegram.RecoveryAlerts && a.notify.Enabled() && set.Current != nil {
		id := stableID("recovery", selectionKey(a.cfg.Ref()), st.LastFailure.UTC().Format(time.RFC3339), strconvI(st.Failures))
		if _, exists := st.Event(id); !exists {
			event := domain.EventRecord{
				ID:                 id,
				DetectedAt:         now,
				Summary:            fmt.Sprintf("coleta recuperada após %d falhas", st.Failures),
				SourceVersion:      set.Current.SourceVersion,
				ElectionKey:        selectionKey(a.cfg.Ref()),
				NotificationStatus: domain.NotifyPending,
			}
			events = append(events, event)
			rows = append(rows, notification.BuildRows(id, a.format.Recovery(set.Current, st.Failures), now)...)
		}
	}
	return events, rows
}

// chartRows renders a PNG and returns a photo outbox row. A nil result means
// the caller should fall back to text (disabled, no votes, or caption too long).
func (a *App) chartRows(ctx context.Context, eventID, caption string, s *domain.Snapshot, now time.Time) []store.Notification {
	if !a.cfg.Telegram.Charts || s == nil {
		return nil
	}
	if telegram.UTF16Length(caption) > telegram.MaxCaptionLength {
		return nil
	}
	data, err := a.renderChart(ctx, s)
	if err != nil {
		if !errors.Is(err, chart.ErrNoData) {
			a.log.Warn("falha ao gerar gráfico; enviando texto", "erro", err)
		}
		return nil
	}
	if err := os.MkdirAll(a.mediaDir, 0o700); err != nil {
		a.log.Warn("falha ao criar diretório de gráficos; enviando texto", "erro", err)
		return nil
	}
	path := filepath.Join(a.mediaDir, eventID+".png")
	tmp, err := os.CreateTemp(a.mediaDir, ".chart-*.tmp")
	if err != nil {
		a.log.Warn("falha ao criar gráfico temporário; enviando texto", "erro", err)
		return nil
	}
	tmpName := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}
	if err := tmp.Chmod(0o600); err != nil {
		cleanup()
		return nil
	}
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return nil
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return nil
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return nil
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return nil
	}
	return notification.PhotoRows(eventID, caption, path, now)
}

// renderChart prefers the state-winner dashboard map for federal Brazil and
// falls back to the candidate bar chart when map data is unavailable.
func (a *App) renderChart(ctx context.Context, s *domain.Snapshot) ([]byte, error) {
	if a.cfg.Telegram.Maps && isFederalBrazil(s) {
		states := a.stateResults(ctx, s)
		if len(states) > 0 {
			if data, err := chart.RenderMap(s, states); err == nil {
				return data, nil
			} else if !errors.Is(err, chart.ErrNoData) {
				a.log.Warn("falha ao gerar mapa; usando gráfico de barras", "erro", err)
			}
		}
	}
	return chart.Render(s)
}

// stateResults fetches the per-UF result files used only to colour the map.
// It is cached for 60s and never mutates persisted state.
func (a *App) stateResults(ctx context.Context, base *domain.Snapshot) map[string]*domain.Snapshot {
	if base == nil {
		return nil
	}
	a.mapLoadMu.Lock()
	defer a.mapLoadMu.Unlock()
	if time.Since(a.mapAt) < 60*time.Second && len(a.mapStates) > 0 {
		return a.mapStates
	}
	mapCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	type fetchResult struct {
		uf    string
		snap  *domain.Snapshot
		cond  tse.Conditional
		err   error
		stale bool
	}
	ufs := []string{
		"ac", "al", "am", "ap", "ba", "ce", "df", "es", "go", "ma", "mg", "ms", "mt",
		"pa", "pb", "pe", "pi", "pr", "rj", "rn", "ro", "rr", "rs", "sc", "se", "sp", "to",
	}
	results := make(chan fetchResult, len(ufs))
	sem := make(chan struct{}, 4)
	localConds := make(map[string]tse.Conditional, len(ufs))
	localStates := make(map[string]*domain.Snapshot, len(ufs))
	for _, uf := range ufs {
		localConds[uf] = a.mapConds[uf]
		localStates[uf] = a.mapStates[uf]
	}
	var wg sync.WaitGroup
	for _, uf := range ufs {
		wg.Add(1)
		go func(uf string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			ref := domain.ElectionRef{
				ElectionCode: base.Election.ElectionCode,
				ContestCode:  base.Election.ContestCode,
				Round:        base.Election.Round,
				Scope:        domain.Scope{Kind: domain.ScopeState, UF: uf},
			}
			res, err := a.adapter.Fetch(mapCtx, ref, localConds[uf])
			results <- fetchResult{
				uf: uf, snap: res.Snapshot, cond: tse.Conditional{ETag: res.ETag, LastModified: res.LastModified},
				err: err, stale: res.NotModified,
			}
		}(uf)
	}
	go func() { wg.Wait(); close(results) }()
	fresh := map[string]*domain.Snapshot{}
	newConds := make(map[string]tse.Conditional, len(ufs))
	for uf, cond := range localConds {
		newConds[uf] = cond
	}
	errorsCount := 0
	for res := range results {
		switch {
		case res.err == nil && res.snap != nil:
			fresh[res.uf] = res.snap
			newConds[res.uf] = res.cond
		case res.err == nil && (res.stale || res.snap == nil):
			if cached := localStates[res.uf]; cached != nil {
				fresh[res.uf] = cached
			}
		default:
			errorsCount++
			if cached := localStates[res.uf]; cached != nil {
				fresh[res.uf] = cached
			}
		}
	}
	if len(fresh) > 0 {
		a.mapStates = fresh
		a.mapConds = newConds
		a.mapAt = time.Now()
	}
	if errorsCount > 0 {
		a.log.Warn("não foi possível carregar todas as UFs para o mapa", "falhas", errorsCount, "ufs_com_dados", len(fresh))
	}
	return fresh
}

// LoadMap loads a read-only state map for the TUI. It is independent from the
// Telegram chart path but shares the 60s per-UF cache.
func (a *App) LoadMap() {
	base := a.store.Snapshot().Snapshot
	if base == nil {
		a.setMapView(&observability.MapView{Error: "aguarde a primeira coleta consolidada", FetchedAt: time.Now()})
		return
	}
	if !isFederalBrazil(base) {
		a.setMapView(&observability.MapView{Error: "mapa por UF disponível apenas para eleição federal monitorada em âmbito Brasil", FetchedAt: time.Now()})
		return
	}
	a.mu.Lock()
	if a.mapView != nil && a.mapView.Loading {
		a.mu.Unlock()
		return
	}
	a.mapSeq++
	seq := a.mapSeq
	a.mapView = &observability.MapView{Loading: true, States: map[string]observability.MapState{}}
	a.mu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		states := a.stateResults(ctx, base)
		mv := buildMapView(states)
		a.mu.Lock()
		if seq == a.mapSeq {
			a.mapView = mv
		}
		a.mu.Unlock()
	}()
}

func (a *App) setMapView(mv *observability.MapView) {
	a.mu.Lock()
	a.mapSeq++
	a.mapView = mv
	a.mu.Unlock()
}

func (a *App) mapViewCopy() *observability.MapView {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.mapView == nil {
		return nil
	}
	cp := *a.mapView
	if a.mapView.States != nil {
		cp.States = make(map[string]observability.MapState, len(a.mapView.States))
		for k, v := range a.mapView.States {
			cp.States[k] = v
		}
	}
	return &cp
}

func buildMapView(states map[string]*domain.Snapshot) *observability.MapView {
	mv := &observability.MapView{States: map[string]observability.MapState{}, FetchedAt: time.Now()}
	counted := 0
	for uf, snap := range states {
		item := observability.MapState{UF: strings.ToUpper(uf)}
		if snap != nil {
			for _, c := range snap.Candidates {
				if c.Votes.OrZero() > 0 {
					item.WinnerNumber = c.Number
					item.WinnerName = c.DisplayName()
					item.WinnerParty = c.PartyAcronym
					item.Votes = c.Votes.OrZero()
					item.PercentNano = c.PercentNano
					item.PercentText = c.PercentText
					if item.PercentText == "" {
						item.PercentText = format.PercentNano(item.PercentNano)
					}
					item.Counted = true
					counted++
					break
				}
			}
		}
		mv.States[uf] = item
	}
	if counted == 0 {
		mv.Error = "ainda não há votos por UF publicados"
	}
	return mv
}

func isFederalBrazil(s *domain.Snapshot) bool {
	return s != nil && s.Election.Scope.Kind == domain.ScopeBrazil && strings.EqualFold(s.Election.Level, "federal")
}

// cleanupMedia removes orphan chart files older than a week. Files belonging
// to pending notifications are recent and are kept.
func (a *App) cleanupMedia() {
	entries, err := os.ReadDir(a.mediaDir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		_ = os.Remove(filepath.Join(a.mediaDir, entry.Name()))
	}
}

func (a *App) persistSuccess(ctx context.Context, before store.State, res tse.Result, events []domain.EventRecord, rows []store.Notification) error {
	now := time.Now()
	key := selectionKey(a.cfg.Ref())
	var newSnapshot *domain.Snapshot = before.Snapshot
	if res.Snapshot != nil {
		newSnapshot = res.Snapshot
	}
	err := a.store.Update(ctx, func(st *store.State) error {
		if newSnapshot != nil {
			st.Snapshot = newSnapshot
			st.Ref = refFromInfo(newSnapshot.Election)
		}
		if st.Cache == nil {
			st.Cache = map[string]store.CacheEntry{}
		}
		st.Cache[key] = store.CacheEntry{
			URL:          res.URL,
			ETag:         res.ETag,
			LastModified: res.LastModified,
			LastFetch:    res.FetchedAt,
			StatusCode:   res.StatusCode,
		}
		st.LastSuccess = now
		st.Failures = 0
		st.LastError = ""
		if newSnapshot != nil {
			fingerprintChanged := before.Snapshot == nil || newSnapshot.Fingerprint() != before.Snapshot.Fingerprint()
			if fingerprintChanged || len(st.History) == 0 {
				st.AddHistory(newSnapshot.HistoryPoint(), 240)
			}
		}
		for _, event := range events {
			if st.AddEvent(event, a.cfg.RetentionEvents) {
				for _, row := range rows {
					if row.EventID == event.ID {
						st.AddNotification(row)
					}
				}
				if event.NotificationStatus != "" {
					st.MarkEventStatus(event.ID, event.NotificationStatus)
				}
			}
		}
		// Rows for an event that already existed are ignored by ID dedupe.
		st.Prune(now, 7*24*time.Hour, 30*24*time.Hour)
		return nil
	})
	if err != nil {
		return fmt.Errorf("persistir estado: %w", err)
	}
	return nil
}

// recordFailure persists the failure streak and emits one alert when the
// threshold is crossed. It never overwrites the last good snapshot.
func (a *App) recordFailure(cause error) {
	now := time.Now()
	a.mu.Lock()
	a.lastError = cause.Error()
	a.mu.Unlock()
	err := a.store.Update(context.Background(), func(st *store.State) error {
		st.Failures++
		st.LastFailure = now
		st.LastError = truncate(cause.Error(), 500)
		if st.Failures == a.cfg.MaxFailures && a.cfg.Telegram.FailureAlerts && a.notify.Enabled() {
			last := "nunca"
			if !st.LastSuccess.IsZero() {
				last = st.LastSuccess.UTC().Format(time.RFC3339)
			}
			id := stableID("failure", selectionKey(a.cfg.Ref()), last, strconvI(st.Failures))
			if _, exists := st.Event(id); !exists {
				event := domain.EventRecord{
					ID:                 id,
					DetectedAt:         now,
					Summary:            fmt.Sprintf("falha persistente na coleta (%d tentativas)", st.Failures),
					SourceVersion:      snapshotVersion(st.Snapshot),
					ElectionKey:        selectionKey(a.cfg.Ref()),
					NotificationStatus: domain.NotifyPending,
				}
				text := a.format.Failure(st.Snapshot, cause.Error(), st.LastSuccess)
				rows := notification.BuildRows(id, text, now)
				if st.AddEvent(event, a.cfg.RetentionEvents) {
					for _, row := range rows {
						st.AddNotification(row)
					}
					st.MarkEventStatus(id, domain.NotifyPending)
				}
			}
		}
		return nil
	})
	if err != nil {
		a.log.Error("falha ao persistir erro de coleta", "erro", err)
	}
	a.log.Warn("coleta falhou", "consecutivas", a.currentFailures(), "erro", cause)
	if len(a.store.Snapshot().Queue) > 0 {
		a.notify.Wake()
	}
}

func (a *App) currentFailures() int {
	return a.store.Snapshot().Failures
}

func (a *App) ufViewCopy() *observability.UFView {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.ufView == nil {
		return nil
	}
	cp := *a.ufView
	return &cp
}

func (a *App) previousSnapshot() *domain.Snapshot {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.previous
}

func (a *App) setSource(s observability.SourceStatus) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if s.LastSuccess.IsZero() {
		s.LastSuccess = a.source.LastSuccess
	}
	a.source = s
}

func isOlder(incoming, stored *domain.Snapshot) bool {
	if stored == nil || incoming == nil {
		return false
	}
	if !incoming.TotalizedAt.IsZero() && !stored.TotalizedAt.IsZero() && incoming.TotalizedAt.Before(stored.TotalizedAt) {
		return true
	}
	return false
}

type staleError struct {
	stored   *domain.Snapshot
	incoming *domain.Snapshot
}

func (e *staleError) Error() string {
	return fmt.Sprintf("snapshot recebido é mais antigo que o último válido (fonte: %s; armazenado: %s); preservando o último estado bom",
		e.incoming.TotalizedAt.Format(time.RFC3339), e.stored.TotalizedAt.Format(time.RFC3339))
}

// IsRetryable allows the poller to try again; a CDN edge may be stale.
func (e *staleError) IsRetryable() bool { return true }

func refFromInfo(info domain.ElectionInfo) domain.ElectionRef {
	return domain.ElectionRef{
		Cycle:        info.Cycle,
		ElectionCode: info.ElectionCode,
		Round:        info.Round,
		ContestCode:  info.ContestCode,
		Scope:        info.Scope,
		ContestName:  info.ContestName,
		ElectionName: info.ElectionName,
		Level:        info.Level,
	}
}

func sameSelection(a, b domain.ElectionRef) bool {
	return strings.EqualFold(a.ElectionCode, b.ElectionCode) &&
		a.ContestCode == b.ContestCode &&
		a.Scope.Key() == b.Scope.Key()
}

func selectionKey(ref domain.ElectionRef) string {
	return strings.Join([]string{ref.ElectionCode, ref.ContestCode, ref.Scope.Key()}, "|")
}

func selectionLabel(ref domain.ElectionRef) string {
	return fmt.Sprintf("%s/%s/%s", ref.ElectionCode, ref.ContestCode, ref.Scope.Key())
}

func stableID(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:16])
}

func snapshotVersion(s *domain.Snapshot) string {
	if s == nil {
		return ""
	}
	return s.SourceVersion
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func strconvI(v int) string {
	return fmt.Sprintf("%d", v)
}
