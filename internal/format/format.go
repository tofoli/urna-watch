// Package format centralizes human-readable formatting so that the TUI,
// notifications and CLI render identical values.
package format

import (
	"fmt"
	"html"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Votes formats an integer with Brazilian thousands separators.
func Votes(v int64) string {
	neg := v < 0
	if neg {
		v = -v
	}
	s := fmt.Sprintf("%d", v)
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(r)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

// PercentNano formats a nano-percent value with two decimal places.
func PercentNano(v int64) string {
	neg := v < 0
	if neg {
		v = -v
	}
	whole := v / 1_000_000_000
	// Round to two decimals (half up) exactly as the official UI does, using
	// integer arithmetic. The exact nano value is kept for comparisons.
	frac := (v%1_000_000_000 + 5_000_000) / 10_000_000
	if frac >= 100 {
		whole++
		frac -= 100
	}
	if neg {
		return fmt.Sprintf("-%d,%02d", whole, frac)
	}
	return fmt.Sprintf("%d,%02d", whole, frac)
}

// DateTime formats a timestamp in the source's local zone.
func DateTime(t time.Time) string {
	if t.IsZero() {
		return "não informado"
	}
	return t.Format("02/01/2006 15:04:05")
}

// Date formats only the date.
func Date(t time.Time) string {
	if t.IsZero() {
		return "não informado"
	}
	return t.Format("02/01/2006")
}

// DurationShort renders a compact, human duration.
func DurationShort(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	switch {
	case d < time.Second:
		return "agora"
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// Relative renders "há 12s" or "em 12s" for a timestamp.
func Relative(t, now time.Time) string {
	if t.IsZero() {
		return "sem registro"
	}
	d := now.Sub(t)
	if d < 0 {
		return "em " + DurationShort(d)
	}
	if d < time.Second {
		return "agora"
	}
	return "há " + DurationShort(d)
}

// Truncate shortens a string to at most n runes, adding an ellipsis.
func Truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

// CleanText removes terminal control characters and bidi overrides from
// external text. TSE data is untrusted input; names/status must never be able
// to inject ANSI sequences or rewrite the terminal.
func CleanText(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\r', '\t':
			return ' '
		case '\u202a', '\u202b', '\u202c', '\u202d', '\u202e',
			'\u2066', '\u2067', '\u2068', '\u2069':
			return -1
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}

// EscapeHTML escapes text for Telegram's HTML parse mode.
func EscapeHTML(s string) string { return html.EscapeString(CleanText(s)) }

// List joins values with a comma.
func List(values []string) string { return strings.Join(values, ", ") }
