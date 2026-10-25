// Package observability contains structured logging and the read-only status
// model shared by the TUI and the optional HTTP status endpoint.
package observability

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// NewLogger builds a sanitized structured logger. Keys that commonly carry
// credentials are redacted defensively even though the project never logs
// them intentionally.
func NewLogger(level, format string, w io.Writer) (*slog.Logger, error) {
	var lvl slog.Level
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "", "info":
		lvl = slog.LevelInfo
	case "debug":
		lvl = slog.LevelDebug
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		return nil, fmt.Errorf("nível de log inválido: %q", level)
	}
	opts := &slog.HandlerOptions{
		Level: lvl,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			key := strings.ToLower(a.Key)
			if strings.Contains(key, "token") || key == "authorization" || key == "secret" || key == "password" {
				return slog.String(a.Key, "[redacted]")
			}
			if a.Value.Kind() == slog.KindString {
				v := a.Value.String()
				if strings.Contains(v, "/bot") && strings.Contains(v, ":") {
					return slog.String(a.Key, "[redacted]")
				}
			}
			return a
		},
	}
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "text":
		return slog.New(slog.NewTextHandler(w, opts)), nil
	case "json":
		return slog.New(slog.NewJSONHandler(w, opts)), nil
	default:
		return nil, fmt.Errorf("formato de log inválido: %q", format)
	}
}
