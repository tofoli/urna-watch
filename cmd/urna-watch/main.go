package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata"

	"github.com/tofoli/urna-watch/internal/app"
	"github.com/tofoli/urna-watch/internal/config"
	"github.com/tofoli/urna-watch/internal/detector"
	"github.com/tofoli/urna-watch/internal/domain"
	"github.com/tofoli/urna-watch/internal/format"
	"github.com/tofoli/urna-watch/internal/observability"
	"github.com/tofoli/urna-watch/internal/tse"
	"github.com/tofoli/urna-watch/internal/tui"
	"github.com/tofoli/urna-watch/internal/version"
)

func main() {
	os.Exit(run())
}

func run() int {
	var (
		configPath   = flag.String("config", "", "arquivo de configuração JSON opcional")
		election     = flag.String("election", "", "código da eleição (ex.: 6258)")
		contest      = flag.String("contest", "", "código do cargo (ex.: 1)")
		cargo        = flag.String("cargo", "", "alias de --contest")
		uf           = flag.String("uf", "", "UF (ex.: sp) ou br para Brasil")
		municipality = flag.String("municipality", "", "código do município (5 dígitos)")
		statePath    = flag.String("state", "", "arquivo de estado durável")
		pollInterval = flag.Duration("poll-interval", 0, "intervalo de coleta (mínimo 30s)")
		httpTimeout  = flag.Duration("http-timeout", 0, "timeout por requisição HTTP")
		statusAddr   = flag.String("status-addr", "", "endereço HTTP opcional de status (host:porta)")
		logLevel     = flag.String("log-level", "", "debug, info, warn ou error")
		logFormat    = flag.String("log-format", "", "text ou json")
		noTUI        = flag.Bool("no-tui", false, "executar sem interface de terminal")
		noTelegram   = flag.Bool("no-telegram", false, "desativar envio ao Telegram nesta execução")
		once         = flag.Bool("once", false, "fazer uma coleta e sair (não persiste nem notifica)")
		jsonOut      = flag.Bool("json", false, "com --once, imprimir JSON")
		validate     = flag.Bool("validate", false, "validar config e endpoint oficiais e sair")
		showVersion  = flag.Bool("version", false, "imprimir versão e sair")
	)
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Urna Watch — monitor dos resultados oficiais do TSE\n\nUso: urna-watch [opções]\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *showVersion {
		fmt.Printf("urna-watch %s (commit %s, build %s)\n", version.Version, version.Commit, version.Date)
		return 0
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "configuração inválida:", err)
		return 1
	}
	if *election != "" {
		cfg.Election = strings.TrimSpace(*election)
	}
	if *contest != "" {
		cfg.Contest = strings.TrimSpace(*contest)
	} else if *cargo != "" {
		cfg.Contest = strings.TrimSpace(*cargo)
	}
	if *uf != "" {
		cfg.UF = strings.ToLower(strings.TrimSpace(*uf))
	}
	if *municipality != "" {
		cfg.Municipality = strings.TrimSpace(*municipality)
	}
	if *statePath != "" {
		cfg.StatePath = *statePath
	}
	if *pollInterval > 0 {
		cfg.PollInterval = *pollInterval
	}
	if *httpTimeout > 0 {
		cfg.HTTPTimeout = *httpTimeout
	}
	if *statusAddr != "" {
		cfg.StatusAddr = *statusAddr
	}
	if *logLevel != "" {
		cfg.LogLevel = *logLevel
	}
	if *logFormat != "" {
		cfg.LogFormat = *logFormat
	}
	if *noTUI {
		cfg.TUI = false
	}
	if *jsonOut {
		*once = true
	}
	if *validate {
		*once = false
	}
	if *noTelegram || *once || *validate {
		cfg.Telegram.Enabled = false
		cfg.Telegram.BotToken = ""
		cfg.Telegram.ChatID = ""
	}
	if err := cfg.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, "configuração inválida:", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if *validate {
		return runValidate(ctx, cfg)
	}
	if *once {
		return runOnce(ctx, cfg, *jsonOut)
	}

	useTUI := cfg.TUI && isTerminal(os.Stdout)
	logger, closeLog, err := buildLogger(cfg, useTUI)
	if err != nil {
		fmt.Fprintln(os.Stderr, "logger:", err)
		return 1
	}
	defer closeLog()

	application, err := app.New(cfg, logger)
	if err != nil {
		logger.Error("falha ao iniciar", "erro", err)
		return 1
	}

	if !useTUI {
		if cfg.TUI {
			logger.Warn("TUI desabilitada: a saída padrão não é um terminal")
		}
		logger.Info("modo headless (sem TUI)")
		application.Run(ctx)
		return 0
	}

	go application.Run(ctx)
	tuiErr := tui.Run(ctx, application, cfg)
	stop()
	select {
	case <-application.Done():
	case <-time.After(20 * time.Second):
		logger.Warn("encerramento demorou mais de 20s; saindo")
	}
	if tuiErr != nil && ctx.Err() == nil {
		fmt.Fprintln(os.Stderr, "TUI:", tuiErr)
		return 1
	}
	return 0
}

func runValidate(ctx context.Context, cfg config.Config) int {
	adapter, err := tse.NewAdapter(tse.Options{
		BaseURL:     cfg.TSEBaseURL,
		Environment: cfg.TSEEnvironment,
		UserAgent:   cfg.UserAgent,
		HTTPClient:  tse.DefaultHTTPClient(cfg.HTTPTimeout),
		ConfigTTL:   cfg.ConfigTTL,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		return 1
	}
	ctx, cancel := context.WithTimeout(ctx, cfg.HTTPTimeout*3)
	defer cancel()
	info, endpoint, err := adapter.Resolve(ctx, cfg.Ref())
	if err != nil {
		fmt.Fprintln(os.Stderr, "falha ao resolver eleição no config oficial:", err)
		return 1
	}
	res, err := adapter.Fetch(ctx, cfg.Ref(), tse.Conditional{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "falha ao consultar %s: %v\n", endpoint, err)
		return 1
	}
	if res.Snapshot == nil {
		fmt.Fprintln(os.Stderr, "resposta sem snapshot")
		return 1
	}
	s := res.Snapshot
	fmt.Printf("eleição:       %s (%s)\n", info.ElectionName, info.ElectionCode)
	fmt.Printf("cargo:         %s (%s)\n", info.ContestName, info.ContestCode)
	fmt.Printf("abrangência:   %s\n", info.ScopeName)
	fmt.Printf("turno:         %s\n", info.RoundLabel())
	fmt.Printf("endpoint:      %s\n", endpoint)
	fmt.Printf("HTTP:          %d (%s)\n", res.StatusCode, res.Duration)
	fmt.Printf("versão fonte:  %s\n", s.SourceVersion)
	fmt.Printf("gerado em:     %s\n", format.DateTime(s.GeneratedAt))
	fmt.Printf("totalizado em: %s\n", format.DateTime(s.TotalizedAt))
	fmt.Printf("estado:        %s\n", s.StateLabel())
	fmt.Printf("seções:        %s / %s (%s%%)\n", measureText(s.SectionsCounted), measureText(s.SectionsTotal), format.PercentNano(s.SectionsPercentNano))
	fmt.Printf("candidatos:    %d\n", len(s.Candidates))
	fmt.Println("validação:     OK — resposta estruturada aceita pelo normalizador")
	return 0
}

func runOnce(ctx context.Context, cfg config.Config, jsonOut bool) int {
	logger, closeLog, err := buildLogger(cfg, false)
	if err != nil {
		fmt.Fprintln(os.Stderr, "logger:", err)
		return 1
	}
	defer closeLog()
	application, err := app.New(cfg, logger)
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		return 1
	}
	ctx, cancel := context.WithTimeout(ctx, cfg.HTTPTimeout*3)
	defer cancel()
	res, set, err := application.CollectOnce(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "coleta falhou:", err)
		return 1
	}
	snap := res.Snapshot
	if snap == nil {
		snap = set.Current
	}
	if jsonOut {
		payload := map[string]any{
			"notModified": res.NotModified,
			"endpoint":    res.URL,
			"snapshot":    snap,
			"change": map[string]any{
				"baseline": set.Baseline,
				"changed":  set.Changed,
				"revision": set.Revision,
				"summary":  detector.Summarize(set),
			},
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fmt.Fprintln(os.Stderr, "json:", err)
			return 1
		}
		return 0
	}
	if res.NotModified {
		fmt.Println("HTTP 304 — nenhuma atualização desde o último estado persistido.")
	}
	if snap == nil {
		fmt.Println("Sem snapshot disponível.")
		return 0
	}
	printSnapshot(snap, set)
	return 0
}

func printSnapshot(s *domain.Snapshot, set domain.ChangeSet) {
	fmt.Printf("Eleição:      %s (%s)\n", s.Election.ElectionName, s.Election.ElectionCode)
	fmt.Printf("Cargo:        %s · %s\n", s.Election.ContestName, s.Election.RoundLabel())
	fmt.Printf("Abrangência:  %s\n", s.Election.ScopeName)
	fmt.Printf("Fonte:        %s\n", s.SourceURL)
	fmt.Printf("Versão:       %s\n", s.SourceVersion)
	fmt.Printf("Gerado em:    %s\n", format.DateTime(s.GeneratedAt))
	fmt.Printf("Totalizado:   %s\n", format.DateTime(s.TotalizedAt))
	fmt.Printf("Estado:       %s\n", s.StateLabel())
	if s.SectionsCounted.Known && s.SectionsTotal.Known {
		fmt.Printf("Seções:       %s / %s (%s%%)\n", format.Votes(s.SectionsCounted.Value), format.Votes(s.SectionsTotal.Value), format.PercentNano(s.SectionsPercentNano))
	}
	fmt.Printf("Votação:      %s\n", measureText(s.Totals.TotalVotes))
	fmt.Printf("Válidos:      %s · Brancos: %s · Nulos: %s\n", measureText(s.Totals.ValidVotes), measureText(s.Totals.BlankVotes), measureText(s.Totals.NullVotes))
	fmt.Println()
	fmt.Printf("%-3s %-5s %-32s %-8s %12s %8s %10s\n", "#", "Nº", "Candidato", "Partido", "Votos", "%", "Situação")
	for _, c := range s.Candidates {
		name := format.Truncate(c.DisplayName(), 32)
		pct := c.PercentText
		if pct == "" {
			pct = format.PercentNano(c.PercentNano)
		}
		fmt.Printf("%-3d %-5s %-32s %-8s %12s %8s %10s\n", c.Position, c.Number, name, c.PartyAcronym, format.Votes(c.Votes.OrZero()), pct, format.Truncate(c.Status, 10))
	}
	if set.Changed {
		fmt.Printf("\nMudanças: %s\n", detector.Summarize(set))
	}
	if set.Revision {
		fmt.Println("Atenção: revisão de dados previamente publicados.")
	}
}

func measureText(m domain.Measure) string {
	if !m.Known {
		return "não informado"
	}
	return format.Votes(m.Value)
}

func buildLogger(cfg config.Config, tuiMode bool) (*slog.Logger, func(), error) {
	var w io.Writer = os.Stderr
	closeFn := func() {}
	if tuiMode {
		path := cfg.LogFile
		if path == "" {
			path = filepath.Join(filepath.Dir(cfg.StatePath), "urna-watch.log")
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, nil, fmt.Errorf("criar diretório de log: %w", err)
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return nil, nil, fmt.Errorf("abrir log %s: %w", path, err)
		}
		w = f
		closeFn = func() { _ = f.Close() }
	}
	logger, err := observability.NewLogger(cfg.LogLevel, cfg.LogFormat, w)
	if err != nil {
		closeFn()
		return nil, nil, err
	}
	return logger, closeFn, nil
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
