package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/gpu/urna-watch/internal/domain"
	"github.com/gpu/urna-watch/internal/format"
)

var (
	accentColor = lipgloss.AdaptiveColor{Light: "#0f766e", Dark: "#2dd4bf"}
	mutedColor  = lipgloss.AdaptiveColor{Light: "#64748b", Dark: "#94a3b8"}
	okColor     = lipgloss.AdaptiveColor{Light: "#15803d", Dark: "#4ade80"}
	warnColor   = lipgloss.AdaptiveColor{Light: "#a16207", Dark: "#facc15"}
	badColor    = lipgloss.AdaptiveColor{Light: "#b91c1c", Dark: "#f87171"}
	panelColor  = lipgloss.AdaptiveColor{Light: "#e2e8f0", Dark: "#334155"}
	selBg       = lipgloss.AdaptiveColor{Light: "#ccfbf1", Dark: "#134e4a"}
	selFg       = lipgloss.AdaptiveColor{Light: "#134e4a", Dark: "#ccfbf1"}
)

var (
	titleStyle    = lipgloss.NewStyle().Foreground(accentColor).Bold(true)
	mutedStyle    = lipgloss.NewStyle().Foreground(mutedColor)
	okStyle       = lipgloss.NewStyle().Foreground(okColor).Bold(true)
	warnStyle     = lipgloss.NewStyle().Foreground(warnColor).Bold(true)
	badStyle      = lipgloss.NewStyle().Foreground(badColor).Bold(true)
	accentStyle   = lipgloss.NewStyle().Foreground(accentColor)
	selectedStyle = lipgloss.NewStyle().Background(selBg).Foreground(selFg).Bold(true)
	modalStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accentColor).Padding(1, 2)
	ruleStyle     = lipgloss.NewStyle().Foreground(panelColor)
)

func widthOf(s string) int { return ansi.StringWidth(s) }

func fit(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if widthOf(s) <= width {
		return s
	}
	return ansi.Truncate(s, width, "…")
}

func padRight(s string, width int) string {
	s = fit(s, width)
	if d := width - widthOf(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

func padLeft(s string, width int) string {
	s = fit(s, width)
	if d := width - widthOf(s); d > 0 {
		return strings.Repeat(" ", d) + s
	}
	return s
}

func center(s string, width int) string {
	if width <= 0 {
		return ""
	}
	s = fit(s, width)
	d := width - widthOf(s)
	if d <= 0 {
		return s
	}
	left := d / 2
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", d-left)
}

func panelTitle(title string, width int) string {
	label := "┃ " + title + " "
	remaining := width - widthOf(label)
	if remaining < 0 {
		return fit(label, width)
	}
	return titleStyle.Render("┃ ") + titleStyle.Render(title) + " " + ruleStyle.Render(strings.Repeat("─", remaining))
}

func rule(width int) string {
	if width <= 0 {
		return ""
	}
	return ruleStyle.Render(strings.Repeat("─", width))
}

func progressBar(percentNano int64, width int) string {
	if width <= 2 {
		return ""
	}
	if percentNano < 0 {
		percentNano = 0
	}
	if percentNano > 100_000_000_000 {
		percentNano = 100_000_000_000
	}
	filled := int(percentNano * int64(width) / 100_000_000_000)
	if filled < 0 {
		filled = 0
	}
	if filled > width {
		filled = width
	}
	return accentStyle.Render(strings.Repeat("█", filled)) + mutedStyle.Render(strings.Repeat("░", width-filled))
}

func candidateStatus(c domain.Candidate) string {
	if strings.TrimSpace(c.Status) != "" {
		return strings.TrimSpace(c.Status)
	}
	if c.Elected != nil && *c.Elected {
		return "eleito"
	}
	return "—"
}

func notificationGlyph(s domain.NotificationStatus) string {
	switch s {
	case domain.NotifySent:
		return "✓"
	case domain.NotifyPending:
		return "⏳"
	case domain.NotifyFailed:
		return "✗"
	case domain.NotifySuppressed:
		return "·"
	case domain.NotifyDisabled:
		return "—"
	default:
		return "·"
	}
}

func notificationLabel(s domain.NotificationStatus) string {
	switch s {
	case domain.NotifySent:
		return "enviado"
	case domain.NotifyPending:
		return "pendente"
	case domain.NotifyFailed:
		return "falhou"
	case domain.NotifySuppressed:
		return "suprimido"
	case domain.NotifyDisabled:
		return "telegram desativado"
	default:
		return "sem status"
	}
}

func deltaText(cur, prev domain.Measure, hasPrev bool) string {
	if !hasPrev || !cur.Known || !prev.Known {
		return "—"
	}
	d := cur.Value - prev.Value
	if d == 0 {
		return "0"
	}
	sign := "+"
	if d < 0 {
		sign = "-"
		d = -d
	}
	return sign + format.Votes(d)
}
