// Package config loads defaults, an optional JSON file, environment variables
// and command-line overrides. Secrets are accepted only from the environment
// by documentation; the file is supported for completeness but is gitignored.
package config

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gpu/urna-watch/internal/domain"
)

// Duration is a time.Duration that unmarshals from strings such as "120s".
type Duration time.Duration

func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		v, err := time.ParseDuration(s)
		if err != nil {
			return err
		}
		*d = Duration(v)
		return nil
	}
	var n int64
	if err := json.Unmarshal(b, &n); err != nil {
		return fmt.Errorf("duração deve ser string (ex.: \"120s\") ou nanossegundos")
	}
	*d = Duration(n)
	return nil
}

func (d Duration) Std() time.Duration { return time.Duration(d) }

// Telegram holds notification settings. The token is never written to logs.
type Telegram struct {
	Enabled        bool     `json:"enabled"`
	BotToken       string   `json:"botToken"`
	ChatID         string   `json:"chatId"`
	APIBase        string   `json:"apiBase"`
	Baseline       bool     `json:"baseline"`
	FailureAlerts  bool     `json:"failureAlerts"`
	RecoveryAlerts bool     `json:"recoveryAlerts"`
	MaxAttempts    int      `json:"maxAttempts"`
	BaseBackoff    Duration `json:"baseBackoff"`
}

// Config is the fully resolved runtime configuration.
type Config struct {
	Election     string `json:"election"`
	Contest      string `json:"contest"`
	UF           string `json:"uf"`
	Municipality string `json:"municipality"`

	PollInterval time.Duration `json:"-"`
	HTTPTimeout  time.Duration `json:"-"`
	ConfigTTL    time.Duration `json:"-"`
	MinBackoff   time.Duration `json:"-"`
	MaxBackoff   time.Duration `json:"-"`

	MaxRetries      int `json:"maxRetries"`
	MaxFailures     int `json:"maxFailures"`
	RetentionEvents int `json:"retentionEvents"`

	StatePath  string `json:"statePath"`
	LogLevel   string `json:"logLevel"`
	LogFormat  string `json:"logFormat"`
	LogFile    string `json:"logFile"`
	TUI        bool   `json:"tui"`
	StatusAddr string `json:"statusAddr"`
	Timezone   string `json:"timezone"`
	UserAgent  string `json:"userAgent"`

	TSEBaseURL     string `json:"tseBaseUrl"`
	TSEEnvironment string `json:"tseEnvironment"`

	Telegram Telegram `json:"telegram"`

	// ConfigPath is recorded for status/debugging; it is not a secret.
	ConfigPath string `json:"-"`
}

type fileConfig struct {
	Election        *string   `json:"election"`
	Contest         *string   `json:"contest"`
	UF              *string   `json:"uf"`
	Municipality    *string   `json:"municipality"`
	PollInterval    *Duration `json:"pollInterval"`
	HTTPTimeout     *Duration `json:"httpTimeout"`
	ConfigTTL       *Duration `json:"configTtl"`
	MinBackoff      *Duration `json:"minBackoff"`
	MaxBackoff      *Duration `json:"maxBackoff"`
	MaxRetries      *int      `json:"maxRetries"`
	MaxFailures     *int      `json:"maxFailures"`
	RetentionEvents *int      `json:"retentionEvents"`
	StatePath       *string   `json:"statePath"`
	LogLevel        *string   `json:"logLevel"`
	LogFormat       *string   `json:"logFormat"`
	LogFile         *string   `json:"logFile"`
	TUI             *bool     `json:"tui"`
	StatusAddr      *string   `json:"statusAddr"`
	Timezone        *string   `json:"timezone"`
	UserAgent       *string   `json:"userAgent"`
	TSEBaseURL      *string   `json:"tseBaseUrl"`
	TSEEnvironment  *string   `json:"tseEnvironment"`
	Telegram        *struct {
		Enabled        *bool     `json:"enabled"`
		BotToken       *string   `json:"botToken"`
		ChatID         *string   `json:"chatId"`
		APIBase        *string   `json:"apiBase"`
		Baseline       *bool     `json:"baseline"`
		FailureAlerts  *bool     `json:"failureAlerts"`
		RecoveryAlerts *bool     `json:"recoveryAlerts"`
		MaxAttempts    *int      `json:"maxAttempts"`
		BaseBackoff    *Duration `json:"baseBackoff"`
	} `json:"telegram"`
}

// Default returns conservative defaults for the currently documented runoff.
func Default() Config {
	return Config{
		Election:        "6258",
		Contest:         "1",
		UF:              "br",
		PollInterval:    120 * time.Second,
		HTTPTimeout:     20 * time.Second,
		ConfigTTL:       30 * time.Minute,
		MinBackoff:      5 * time.Second,
		MaxBackoff:      5 * time.Minute,
		MaxRetries:      2,
		MaxFailures:     3,
		RetentionEvents: 200,
		StatePath:       filepath.Join("data", "state.json"),
		LogLevel:        "info",
		LogFormat:       "text",
		TUI:             true,
		Timezone:        "America/Sao_Paulo",
		UserAgent:       "urna-watch/0.1 (+https://github.com/gpu/urna-watch; monitor de resultados oficiais)",
		TSEBaseURL:      "https://resultados.tse.jus.br",
		TSEEnvironment:  "oficial",
		Telegram: Telegram{
			Enabled:        false,
			APIBase:        "https://api.telegram.org",
			Baseline:       true,
			FailureAlerts:  true,
			RecoveryAlerts: true,
			MaxAttempts:    8,
			BaseBackoff:    Duration(5 * time.Second),
		},
	}
}

// Load resolves file (if any), environment and validation.
func Load(path string) (Config, error) {
	cfg := Default()
	if path == "" {
		path = os.Getenv("URNA_CONFIG")
	}
	if path != "" {
		if err := loadFile(path, &cfg); err != nil {
			return Config{}, err
		}
		cfg.ConfigPath = path
	}
	if err := applyEnv(&cfg); err != nil {
		return Config{}, err
	}
	cfg.normalize()
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func loadFile(path string, cfg *Config) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("abrir config %s: %w", path, err)
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	var raw fileConfig
	if err := dec.Decode(&raw); err != nil {
		return fmt.Errorf("config %s inválida: %w", path, err)
	}
	applyFile(cfg, raw)
	return nil
}

func applyFile(cfg *Config, r fileConfig) {
	set := func(dst *string, src *string) {
		if src != nil {
			*dst = *src
		}
	}
	set(&cfg.Election, r.Election)
	set(&cfg.Contest, r.Contest)
	set(&cfg.UF, r.UF)
	set(&cfg.Municipality, r.Municipality)
	set(&cfg.StatePath, r.StatePath)
	set(&cfg.LogLevel, r.LogLevel)
	set(&cfg.LogFormat, r.LogFormat)
	set(&cfg.LogFile, r.LogFile)
	set(&cfg.StatusAddr, r.StatusAddr)
	set(&cfg.Timezone, r.Timezone)
	set(&cfg.UserAgent, r.UserAgent)
	set(&cfg.TSEBaseURL, r.TSEBaseURL)
	set(&cfg.TSEEnvironment, r.TSEEnvironment)
	if r.PollInterval != nil {
		cfg.PollInterval = r.PollInterval.Std()
	}
	if r.HTTPTimeout != nil {
		cfg.HTTPTimeout = r.HTTPTimeout.Std()
	}
	if r.ConfigTTL != nil {
		cfg.ConfigTTL = r.ConfigTTL.Std()
	}
	if r.MinBackoff != nil {
		cfg.MinBackoff = r.MinBackoff.Std()
	}
	if r.MaxBackoff != nil {
		cfg.MaxBackoff = r.MaxBackoff.Std()
	}
	if r.MaxRetries != nil {
		cfg.MaxRetries = *r.MaxRetries
	}
	if r.MaxFailures != nil {
		cfg.MaxFailures = *r.MaxFailures
	}
	if r.RetentionEvents != nil {
		cfg.RetentionEvents = *r.RetentionEvents
	}
	if r.TUI != nil {
		cfg.TUI = *r.TUI
	}
	if r.Telegram != nil {
		t := &cfg.Telegram
		if r.Telegram.Enabled != nil {
			t.Enabled = *r.Telegram.Enabled
		}
		if r.Telegram.BotToken != nil {
			t.BotToken = *r.Telegram.BotToken
		}
		if r.Telegram.ChatID != nil {
			t.ChatID = *r.Telegram.ChatID
		}
		if r.Telegram.APIBase != nil {
			t.APIBase = *r.Telegram.APIBase
		}
		if r.Telegram.Baseline != nil {
			t.Baseline = *r.Telegram.Baseline
		}
		if r.Telegram.FailureAlerts != nil {
			t.FailureAlerts = *r.Telegram.FailureAlerts
		}
		if r.Telegram.RecoveryAlerts != nil {
			t.RecoveryAlerts = *r.Telegram.RecoveryAlerts
		}
		if r.Telegram.MaxAttempts != nil {
			t.MaxAttempts = *r.Telegram.MaxAttempts
		}
		if r.Telegram.BaseBackoff != nil {
			t.BaseBackoff = *r.Telegram.BaseBackoff
		}
	}
}

func applyEnv(cfg *Config) error {
	var err error
	str := func(name string, dst *string) {
		if v, ok := os.LookupEnv(name); ok && strings.TrimSpace(v) != "" {
			*dst = strings.TrimSpace(v)
		}
	}
	str("URNA_ELECTION", &cfg.Election)
	str("URNA_CONTEST", &cfg.Contest)
	str("URNA_CARGO", &cfg.Contest)
	str("URNA_UF", &cfg.UF)
	str("URNA_MUNICIPALITY", &cfg.Municipality)
	str("URNA_STATE_PATH", &cfg.StatePath)
	str("URNA_LOG_LEVEL", &cfg.LogLevel)
	str("URNA_LOG_FORMAT", &cfg.LogFormat)
	str("URNA_LOG_FILE", &cfg.LogFile)
	str("URNA_STATUS_ADDR", &cfg.StatusAddr)
	str("URNA_TIMEZONE", &cfg.Timezone)
	str("URNA_USER_AGENT", &cfg.UserAgent)
	str("URNA_TSE_BASE_URL", &cfg.TSEBaseURL)
	str("URNA_TSE_ENVIRONMENT", &cfg.TSEEnvironment)
	str("TELEGRAM_BOT_TOKEN", &cfg.Telegram.BotToken)
	str("TELEGRAM_CHAT_ID", &cfg.Telegram.ChatID)
	str("TELEGRAM_API_BASE", &cfg.Telegram.APIBase)

	if v, ok := os.LookupEnv("URNA_POLL_INTERVAL"); ok {
		if cfg.PollInterval, err = time.ParseDuration(strings.TrimSpace(v)); err != nil {
			return fmt.Errorf("URNA_POLL_INTERVAL inválido: %w", err)
		}
	}
	if v, ok := os.LookupEnv("URNA_HTTP_TIMEOUT"); ok {
		if cfg.HTTPTimeout, err = time.ParseDuration(strings.TrimSpace(v)); err != nil {
			return fmt.Errorf("URNA_HTTP_TIMEOUT inválido: %w", err)
		}
	}
	if v, ok := os.LookupEnv("URNA_CONFIG_TTL"); ok {
		if cfg.ConfigTTL, err = time.ParseDuration(strings.TrimSpace(v)); err != nil {
			return fmt.Errorf("URNA_CONFIG_TTL inválido: %w", err)
		}
	}
	if v, ok := os.LookupEnv("URNA_MIN_BACKOFF"); ok {
		if cfg.MinBackoff, err = time.ParseDuration(strings.TrimSpace(v)); err != nil {
			return fmt.Errorf("URNA_MIN_BACKOFF inválido: %w", err)
		}
	}
	if v, ok := os.LookupEnv("URNA_MAX_BACKOFF"); ok {
		if cfg.MaxBackoff, err = time.ParseDuration(strings.TrimSpace(v)); err != nil {
			return fmt.Errorf("URNA_MAX_BACKOFF inválido: %w", err)
		}
	}
	if v, ok := os.LookupEnv("TELEGRAM_BASE_BACKOFF"); ok {
		d, e := time.ParseDuration(strings.TrimSpace(v))
		if e != nil {
			return fmt.Errorf("TELEGRAM_BASE_BACKOFF inválido: %w", e)
		}
		cfg.Telegram.BaseBackoff = Duration(d)
	}

	intEnv := func(name string, dst *int) error {
		if v, ok := os.LookupEnv(name); ok {
			n, e := strconv.Atoi(strings.TrimSpace(v))
			if e != nil {
				return fmt.Errorf("%s inválido: %w", name, e)
			}
			*dst = n
		}
		return nil
	}
	if err := intEnv("URNA_MAX_RETRIES", &cfg.MaxRetries); err != nil {
		return err
	}
	if err := intEnv("URNA_MAX_FAILURES", &cfg.MaxFailures); err != nil {
		return err
	}
	if err := intEnv("URNA_RETENTION_EVENTS", &cfg.RetentionEvents); err != nil {
		return err
	}
	if err := intEnv("TELEGRAM_MAX_ATTEMPTS", &cfg.Telegram.MaxAttempts); err != nil {
		return err
	}

	boolEnv := func(name string, dst *bool) error {
		if v, ok := os.LookupEnv(name); ok {
			b, e := parseBool(v)
			if e != nil {
				return fmt.Errorf("%s inválido: %w", name, e)
			}
			*dst = b
		}
		return nil
	}
	if err := boolEnv("URNA_TUI", &cfg.TUI); err != nil {
		return err
	}
	if err := boolEnv("TELEGRAM_ENABLED", &cfg.Telegram.Enabled); err != nil {
		return err
	}
	if err := boolEnv("TELEGRAM_BASELINE", &cfg.Telegram.Baseline); err != nil {
		return err
	}
	if err := boolEnv("TELEGRAM_FAILURE_ALERTS", &cfg.Telegram.FailureAlerts); err != nil {
		return err
	}
	if err := boolEnv("TELEGRAM_RECOVERY_ALERTS", &cfg.Telegram.RecoveryAlerts); err != nil {
		return err
	}
	return nil
}

func (c *Config) normalize() {
	c.Election = strings.TrimSpace(c.Election)
	c.Contest = strings.TrimSpace(c.Contest)
	c.UF = strings.ToLower(strings.TrimSpace(c.UF))
	c.Municipality = strings.TrimSpace(c.Municipality)
	c.LogLevel = strings.ToLower(strings.TrimSpace(c.LogLevel))
	c.LogFormat = strings.ToLower(strings.TrimSpace(c.LogFormat))
	c.TSEEnvironment = strings.Trim(strings.TrimSpace(c.TSEEnvironment), "/")
}

// Scope converts the configured geography into a domain scope.
func (c Config) Scope() domain.Scope {
	switch {
	case c.Municipality != "":
		mun := strings.TrimSpace(c.Municipality)
		for len(mun) < 5 {
			mun = "0" + mun
		}
		return domain.Scope{Kind: domain.ScopeMunicipality, UF: c.UF, Municipality: mun}
	case c.UF == "" || c.UF == "br":
		return domain.Scope{Kind: domain.ScopeBrazil, UF: "br"}
	default:
		return domain.Scope{Kind: domain.ScopeState, UF: c.UF}
	}
}

// Ref is the configured election reference.
func (c Config) Ref() domain.ElectionRef {
	return domain.ElectionRef{
		ElectionCode: c.Election,
		ContestCode:  c.Contest,
		Scope:        c.Scope(),
	}
}

// Validate rejects unsafe or ambiguous configuration.
func (c Config) Validate() error {
	if c.Election == "" {
		return fmt.Errorf("eleição não configurada")
	}
	if _, err := strconv.Atoi(c.Election); err != nil {
		return fmt.Errorf("URNA_ELECTION deve ser numérica")
	}
	if c.Contest == "" {
		return fmt.Errorf("cargo não configurado")
	}
	if _, err := strconv.Atoi(c.Contest); err != nil {
		return fmt.Errorf("URNA_CONTEST deve ser numérico")
	}
	if c.UF == "" {
		return fmt.Errorf("UF não configurada (use br para Brasil)")
	}
	if c.UF != "br" && len(c.UF) != 2 {
		return fmt.Errorf("UF inválida: %q", c.UF)
	}
	if c.Municipality != "" {
		if c.UF == "br" {
			return fmt.Errorf("município exige uma UF específica")
		}
		if len(c.Municipality) > 5 {
			return fmt.Errorf("código de município deve ter até 5 dígitos")
		}
		if _, err := strconv.Atoi(c.Municipality); err != nil {
			return fmt.Errorf("código de município deve ser numérico")
		}
	}
	if c.PollInterval < 30*time.Second {
		return fmt.Errorf("intervalo de polling mínimo é 30s (o próprio TSE revalida dados parciais a cada 30s)")
	}
	if c.PollInterval > 24*time.Hour {
		return fmt.Errorf("intervalo de polling máximo é 24h")
	}
	if c.HTTPTimeout < 2*time.Second || c.HTTPTimeout > 2*time.Minute {
		return fmt.Errorf("timeout HTTP deve estar entre 2s e 2m")
	}
	if c.ConfigTTL < time.Minute || c.ConfigTTL > 24*time.Hour {
		return fmt.Errorf("TTL do config deve estar entre 1m e 24h")
	}
	if c.MinBackoff < time.Second || c.MaxBackoff < c.MinBackoff || c.MaxBackoff > time.Hour {
		return fmt.Errorf("backoff inválido (mínimo 1s, máximo 1h e >= mínimo)")
	}
	if c.MaxRetries < 0 || c.MaxRetries > 10 {
		return fmt.Errorf("URNA_MAX_RETRIES deve estar entre 0 e 10")
	}
	if c.MaxFailures < 1 || c.MaxFailures > 100 {
		return fmt.Errorf("URNA_MAX_FAILURES deve estar entre 1 e 100")
	}
	if c.RetentionEvents < 20 || c.RetentionEvents > 5000 {
		return fmt.Errorf("URNA_RETENTION_EVENTS deve estar entre 20 e 5000")
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "warning", "error":
	default:
		return fmt.Errorf("nível de log inválido: %q", c.LogLevel)
	}
	switch c.LogFormat {
	case "text", "json":
	default:
		return fmt.Errorf("formato de log inválido: %q", c.LogFormat)
	}
	if c.StatePath == "" {
		return fmt.Errorf("caminho do estado não configurado")
	}
	if c.TSEBaseURL == "" {
		return fmt.Errorf("URL base do TSE não configurada")
	}
	if !strings.HasPrefix(c.TSEBaseURL, "https://") && !strings.HasPrefix(c.TSEBaseURL, "http://") {
		return fmt.Errorf("URL base do TSE deve usar http(s)")
	}
	if !validEnvironment(c.TSEEnvironment) {
		return fmt.Errorf("ambiente do TSE inválido")
	}
	if c.StatusAddr != "" {
		if _, _, err := net.SplitHostPort(c.StatusAddr); err != nil {
			return fmt.Errorf("URNA_STATUS_ADDR inválido (use host:porta): %w", err)
		}
	}
	if _, err := time.LoadLocation(c.Timezone); err != nil {
		return fmt.Errorf("timezone inválida: %q", c.Timezone)
	}
	if c.Telegram.Enabled {
		if strings.TrimSpace(c.Telegram.BotToken) == "" {
			return fmt.Errorf("TELEGRAM_ENABLED=true exige TELEGRAM_BOT_TOKEN")
		}
		if !strings.Contains(c.Telegram.BotToken, ":") {
			return fmt.Errorf("TELEGRAM_BOT_TOKEN com formato inválido")
		}
		if strings.TrimSpace(c.Telegram.ChatID) == "" {
			return fmt.Errorf("TELEGRAM_ENABLED=true exige TELEGRAM_CHAT_ID")
		}
		if c.Telegram.APIBase == "" {
			return fmt.Errorf("TELEGRAM_API_BASE não configurada")
		}
	}
	return nil
}

func validEnvironment(v string) bool {
	if v == "" || v == "." || v == ".." {
		return false
	}
	for _, r := range v {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func parseBool(v string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "y", "on", "sim", "s":
		return true, nil
	case "0", "false", "no", "off", "nao", "não", "n":
		return false, nil
	default:
		return false, fmt.Errorf("valor booleano inválido %q", v)
	}
}

// RedactedChatID hides most of the chat id in logs.
func (c Config) RedactedChatID() string {
	id := strings.TrimSpace(c.Telegram.ChatID)
	if len(id) <= 4 {
		if id == "" {
			return ""
		}
		return "***"
	}
	return "***" + id[len(id)-3:]
}
