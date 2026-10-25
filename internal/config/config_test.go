package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultIsValid(t *testing.T) {
	cfg := Default()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default invalid: %v", err)
	}
	if cfg.Scope().Kind != "br" || cfg.Scope().Municipality != "" {
		t.Fatalf("scope = %+v", cfg.Scope())
	}
}

func TestLoadFromEnvironment(t *testing.T) {
	t.Setenv("URNA_ELECTION", "6259")
	t.Setenv("URNA_CONTEST", "3")
	t.Setenv("URNA_UF", "PI")
	t.Setenv("URNA_POLL_INTERVAL", "45s")
	t.Setenv("URNA_TUI", "false")
	t.Setenv("TELEGRAM_ENABLED", "false")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Election != "6259" || cfg.Contest != "3" || cfg.UF != "pi" {
		t.Fatalf("cfg = %+v", cfg)
	}
	if cfg.PollInterval != 45*time.Second || cfg.TUI {
		t.Fatalf("cfg = %+v", cfg)
	}
	if cfg.Scope().Kind != "uf" || cfg.Scope().UF != "pi" {
		t.Fatalf("scope = %+v", cfg.Scope())
	}
}

func TestLoadJSONFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	body := `{
		"election": "6260",
		"contest": "3",
		"uf": "am",
		"pollInterval": "90s",
		"statePath": "/tmp/urna-test-state.json",
		"telegram": {"enabled": false, "baseline": false}
	}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Election != "6260" || cfg.PollInterval != 90*time.Second || cfg.Telegram.Baseline {
		t.Fatalf("cfg = %+v", cfg)
	}
	if cfg.ConfigPath != path {
		t.Fatalf("config path = %q", cfg.ConfigPath)
	}
}

func TestLoadRejectsUnknownJSONField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"nope": true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected error")
	}
}

func TestValidationErrors(t *testing.T) {
	cases := map[string]func(*Config){
		"short interval":     func(c *Config) { c.PollInterval = time.Second },
		"bad uf":             func(c *Config) { c.UF = "zzz" },
		"municipality w/ br": func(c *Config) { c.Municipality = "12345" },
		"bad timeout":        func(c *Config) { c.HTTPTimeout = time.Second },
		"bad backoff":        func(c *Config) { c.MinBackoff = 0 },
		"bad log":            func(c *Config) { c.LogLevel = "verbose" },
		"bad environment":    func(c *Config) { c.TSEEnvironment = "../x" },
		"bad timezone":       func(c *Config) { c.Timezone = "Mars/Base" },
		"telegram no token": func(c *Config) {
			c.Telegram.Enabled = true
			c.Telegram.ChatID = "-1"
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := Default()
			mutate(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestTelegramRedaction(t *testing.T) {
	cfg := Default()
	cfg.Telegram.ChatID = "-100123456789"
	got := cfg.RedactedChatID()
	if got != "***789" {
		t.Fatalf("got %q", got)
	}
}

func TestMunicipalityScopeIsPadded(t *testing.T) {
	cfg := Default()
	cfg.UF = "sp"
	cfg.Municipality = "67458"
	scope := cfg.Scope()
	if scope.Kind != "municipality" || scope.Municipality != "67458" {
		t.Fatalf("scope = %+v", scope)
	}
	cfg.Municipality = "1120"
	if got := cfg.Scope().Municipality; got != "01120" {
		t.Fatalf("municipality = %q", got)
	}
}

func TestChartsDefaultAndEnv(t *testing.T) {
	cfg := Default()
	if !cfg.Telegram.Charts {
		t.Fatal("charts should default to true")
	}
	t.Setenv("TELEGRAM_CHARTS", "false")
	loaded, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Telegram.Charts {
		t.Fatal("TELEGRAM_CHARTS=false ignored")
	}
	if !loaded.Telegram.Maps {
		t.Fatal("maps should default to true")
	}
}
