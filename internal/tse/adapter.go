// Package tse implements the official TSE EA20 result source adapter.
//
// The adapter reads structured JSON files only. It never parses the results
// application HTML. Endpoints and field semantics are documented in
// docs/research/tse-data-source.md.
package tse

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tofoli/urna-watch/internal/domain"
)

const (
	defaultConfigTTL = 30 * time.Minute
	maxBodyBytes     = 32 << 20
)

// Options configures the official source adapter.
type Options struct {
	BaseURL     string
	Environment string
	UserAgent   string
	HTTPClient  *http.Client
	ConfigTTL   time.Duration
	Logger      *slog.Logger
}

// Conditional carries the validators from the previous successful request.
type Conditional struct {
	ETag         string
	LastModified string
}

// Result is one fetch attempt. A NotModified result has no Snapshot.
type Result struct {
	Snapshot     *domain.Snapshot
	NotModified  bool
	ETag         string
	LastModified string
	URL          string
	StatusCode   int
	FetchedAt    time.Time
	Duration     time.Duration
}

// Adapter talks to the official TSE CDN.
type Adapter struct {
	BaseURL     string
	Environment string
	UserAgent   string

	http      *http.Client
	configTTL time.Duration
	log       *slog.Logger
	now       func() time.Time

	mu      sync.Mutex
	cfg     *wireConfig
	cfgAt   time.Time
	cfgETag string
	cfgLM   string
	cfgURL  string
}

// NewAdapter validates options and builds the adapter.
func NewAdapter(opt Options) (*Adapter, error) {
	base := strings.TrimRight(strings.TrimSpace(opt.BaseURL), "/")
	if base == "" {
		base = "https://resultados.tse.jus.br"
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("URNA_TSE_BASE_URL inválida: %q", opt.BaseURL)
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return nil, fmt.Errorf("URNA_TSE_BASE_URL deve usar http ou https")
	}
	env := strings.Trim(strings.TrimSpace(opt.Environment), "/")
	if env == "" {
		env = "oficial"
	}
	ua := strings.TrimSpace(opt.UserAgent)
	if ua == "" {
		ua = "urna-watch/0.1 (+https://github.com/tofoli/urna-watch; monitor de resultados oficiais)"
	}
	hc := opt.HTTPClient
	if hc == nil {
		hc = DefaultHTTPClient(20 * time.Second)
	}
	ttl := opt.ConfigTTL
	if ttl <= 0 {
		ttl = defaultConfigTTL
	}
	log := opt.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Adapter{
		BaseURL:     base,
		Environment: env,
		UserAgent:   ua,
		http:        hc,
		configTTL:   ttl,
		log:         log,
		now:         time.Now,
		cfgURL:      base + "/" + env + "/comum/config/ele-c.json",
	}, nil
}

// DefaultHTTPClient builds a connection-reusing client with conservative
// timeouts. The per-request context still controls the total deadline.
func DefaultHTTPClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          32,
		MaxIdleConnsPerHost:   8,
		MaxConnsPerHost:       8,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     true,
	}
	return &http.Client{Transport: tr, Timeout: timeout}
}

// Resolve fetches (with cache) the official election config and validates the
// requested election, contest and scope.
func (a *Adapter) Resolve(ctx context.Context, ref domain.ElectionRef) (domain.ElectionInfo, string, error) {
	cfg, err := a.electionConfig(ctx)
	if err != nil {
		return domain.ElectionInfo{}, "", err
	}
	return a.resolveElection(cfg, ref)
}

// Fetch retrieves and normalizes one result file.
func (a *Adapter) Fetch(ctx context.Context, ref domain.ElectionRef, cond Conditional) (Result, error) {
	start := a.now()
	info, endpoint, err := a.Resolve(ctx, ref)
	if err != nil {
		return Result{URL: endpoint, FetchedAt: start}, err
	}
	body, status, etag, lm, retryAfter, err := a.get(ctx, endpoint, cond)
	elapsed := a.now().Sub(start)
	if err != nil {
		return Result{URL: endpoint, StatusCode: status, FetchedAt: start, Duration: elapsed}, err
	}
	if status == http.StatusNotModified {
		return Result{
			NotModified:  true,
			ETag:         firstNonEmpty(etag, cond.ETag),
			LastModified: firstNonEmpty(lm, cond.LastModified),
			URL:          endpoint,
			StatusCode:   status,
			FetchedAt:    a.now(),
			Duration:     elapsed,
		}, nil
	}
	_ = retryAfter
	snapshot, err := ParseResult(body, info, endpoint, a.now())
	if err != nil {
		return Result{URL: endpoint, StatusCode: status, FetchedAt: start, Duration: elapsed}, err
	}
	return Result{
		Snapshot:     snapshot,
		ETag:         etag,
		LastModified: lm,
		URL:          endpoint,
		StatusCode:   status,
		FetchedAt:    a.now(),
		Duration:     elapsed,
	}, nil
}

func (a *Adapter) electionConfig(ctx context.Context) (*wireConfig, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cfg != nil && a.now().Sub(a.cfgAt) < a.configTTL {
		return a.cfg, nil
	}
	body, status, etag, lm, _, err := a.get(ctx, a.cfgURL, Conditional{ETag: a.cfgETag, LastModified: a.cfgLM})
	if err != nil {
		if a.cfg != nil {
			a.log.Warn("config oficial do TSE indisponível; usando cópia em memória", "erro", err)
			return a.cfg, nil
		}
		return nil, fmt.Errorf("não foi possível obter o config oficial do TSE: %w", err)
	}
	if status == http.StatusNotModified && a.cfg != nil {
		a.cfgAt = a.now()
		return a.cfg, nil
	}
	var cfg wireConfig
	if err := json.Unmarshal(body, &cfg); err != nil {
		return nil, &ParseError{Reason: "config oficial não é JSON válido", Err: err}
	}
	if len(cfg.PL) == 0 {
		return nil, &ParseError{Reason: "config oficial sem pleitos"}
	}
	a.cfg = &cfg
	a.cfgAt = a.now()
	a.cfgETag = etag
	a.cfgLM = lm
	a.log.Info("config oficial do TSE carregado",
		"url", a.cfgURL,
		"gerado_em", strings.TrimSpace(cfg.DG+" "+cfg.HG),
		"pleitos", len(cfg.PL),
		"eleicoes", len(sortedElectionCodes(&cfg)),
	)
	return a.cfg, nil
}

// get executes one conditional GET. A 304 response is not an error.
func (a *Adapter) get(ctx context.Context, endpoint string, cond Conditional) (body []byte, status int, etag, lastModified string, retryAfter time.Duration, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, "", "", 0, err
	}
	req.Header.Set("User-Agent", a.UserAgent)
	req.Header.Set("Accept", "application/json")
	if cond.ETag != "" {
		req.Header.Set("If-None-Match", cond.ETag)
	}
	if cond.LastModified != "" {
		req.Header.Set("If-Modified-Since", cond.LastModified)
	}
	resp, err := a.http.Do(req)
	if err != nil {
		return nil, 0, "", "", 0, fmt.Errorf("falha de rede ao consultar o TSE: %w", sanitizeNetworkError(err))
	}
	defer resp.Body.Close()
	etag = resp.Header.Get("ETag")
	lastModified = resp.Header.Get("Last-Modified")
	status = resp.StatusCode
	if status == http.StatusNotModified {
		return nil, status, etag, lastModified, 0, nil
	}
	limited := io.LimitReader(resp.Body, maxBodyBytes+1)
	body, err = io.ReadAll(limited)
	if err != nil {
		return nil, status, etag, lastModified, 0, fmt.Errorf("falha ao ler resposta do TSE: %w", err)
	}
	if len(body) > maxBodyBytes {
		return nil, status, etag, lastModified, 0, &ParseError{Reason: "resposta do TSE excede o limite de 32 MiB"}
	}
	if status < 200 || status >= 300 {
		ra := parseRetryAfter(resp.Header.Get("Retry-After"))
		if ra == 0 && status == http.StatusTooManyRequests {
			ra = parseRetryAfterBody(body)
		}
		return body, status, etag, lastModified, ra, &HTTPError{
			StatusCode: status,
			URL:        endpoint,
			RetryAfter: ra,
			Body:       snippet(body, 240),
		}
	}
	return body, status, etag, lastModified, 0, nil
}

func parseRetryAfter(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if secs, err := strconv.Atoi(value); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(value); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

func parseRetryAfterBody(body []byte) time.Duration {
	var payload struct {
		Parameters struct {
			RetryAfter int `json:"retry_after"`
		} `json:"parameters"`
	}
	if err := json.Unmarshal(body, &payload); err == nil && payload.Parameters.RetryAfter > 0 {
		return time.Duration(payload.Parameters.RetryAfter) * time.Second
	}
	return 0
}

func snippet(body []byte, n int) string {
	s := strings.TrimSpace(string(body))
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > n {
		s = s[:n] + "…"
	}
	return s
}

func sanitizeNetworkError(err error) error {
	// url.Error embeds the request URL. TSE URLs carry no secrets, but keep the
	// error compact and free of large request dumps.
	if ue, ok := err.(*url.Error); ok {
		return fmt.Errorf("%s: %v", ue.Op, ue.Err)
	}
	return err
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
