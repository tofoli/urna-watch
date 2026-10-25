// Package tui implements the Bubble Tea dashboard. It is a presentation layer
// only: every network operation happens in the application goroutine.
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gpu/urna-watch/internal/chart"
	"github.com/gpu/urna-watch/internal/config"
	"github.com/gpu/urna-watch/internal/domain"
	"github.com/gpu/urna-watch/internal/observability"
	"github.com/gpu/urna-watch/internal/tse"
)

// Source is implemented by the application.
type Source interface {
	Status() observability.Status
	Pause()
	Resume()
	Refresh()
	SelectUF(uf string)
	ClearUF()
	LoadMap()
}

type tickMsg time.Time

type hitKind int

const (
	hitCandidate hitKind = iota
	hitEvent
)

type hitbox struct {
	x1, x2, y int
	kind      hitKind
	index     int
}

type detailsKind int

const (
	detailsCandidate detailsKind = iota
	detailsEvent
)

// Model is the dashboard state.
type Model struct {
	src Source
	cfg config.Config

	width, height int
	status        observability.Status

	selected     int
	candScroll   int
	eventScroll  int
	focus        int // 0 = candidates, 1 = events
	showHelp     bool
	showDetails  bool
	showUFSelect bool
	showMap      bool
	mapGrid      [][]string
	mapGridKey   string
	detailKind   detailsKind
	detailIndex  int
	ufIndex      int
	ufScroll     int

	visibleCandRows int
	hitboxes        []hitbox
}

// New builds the model with an initial status snapshot.
func New(src Source, cfg config.Config) *Model {
	m := &Model{src: src, cfg: cfg, width: 100, height: 30}
	if src != nil {
		m.status = src.Status()
	}
	return m
}

// Run starts the Bubble Tea program and blocks until the user quits.
func Run(ctx context.Context, src Source, cfg config.Config) error {
	p := tea.NewProgram(
		New(src, cfg),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
		tea.WithContext(ctx),
	)
	_, err := p.Run()
	return err
}

func (m *Model) Init() tea.Cmd {
	return tickCmd()
}

func tickCmd() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		if msg.Width > 0 {
			m.width = msg.Width
		}
		if msg.Height > 0 {
			m.height = msg.Height
		}
		m.clampSelection()
		return m, nil

	case tickMsg:
		if m.src != nil {
			m.status = m.src.Status()
		}
		m.clampSelection()
		return m, tickCmd()

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.MouseMsg:
		return m.handleMouse(msg)
	}
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+c" {
		return m, tea.Quit
	}
	if m.showUFSelect {
		switch key {
		case "esc", "u":
			m.showUFSelect = false
		case "up", "k":
			m.ufIndex--
			m.clampUF()
		case "down", "j":
			m.ufIndex++
			m.clampUF()
		case "home", "g":
			m.ufIndex = 0
			m.ufScroll = 0
		case "end", "G":
			m.ufIndex = len(ufCodes) - 1
			m.clampUF()
		case "enter":
			m.chooseUF()
		}
		return m, nil
	}
	if m.showHelp {
		switch key {
		case "esc", "h", "?", "enter", "q":
			m.showHelp = false
			return m, nil
		}
		return m, nil
	}
	if m.showDetails {
		switch key {
		case "esc", "enter", "q":
			m.showDetails = false
		case "up", "k":
			m.moveDetail(-1)
		case "down", "j":
			m.moveDetail(1)
		}
		return m, nil
	}
	switch key {
	case "q":
		return m, tea.Quit
	case "p":
		if m.status.Paused {
			if m.src != nil {
				m.src.Resume()
			}
		} else if m.src != nil {
			m.src.Pause()
		}
		m.status.Paused = !m.status.Paused
	case "r":
		if m.src != nil {
			m.src.Refresh()
		}
	case "h", "?":
		m.showHelp = true
	case "u":
		if m.canShowUF() {
			m.openUFSelect()
		}
	case "b":
		if m.status.UFView != nil && m.src != nil {
			m.src.ClearUF()
		}
	case "m":
		if m.showMap {
			m.showMap = false
			break
		}
		m.showMap = true
		if m.src != nil && (m.status.Map == nil || m.status.Map.Error != "" || (len(m.status.Map.States) == 0 && !m.status.Map.Loading)) {
			m.src.LoadMap()
		}
	case "tab", "shift+tab":
		m.focus = 1 - m.focus
	case "c":
		m.focus = 0
	case "e":
		m.focus = 1
	case "up", "k":
		m.move(-1)
	case "down", "j":
		m.move(1)
	case "pgup":
		m.move(-10)
	case "pgdown":
		m.move(10)
	case "home", "g":
		m.jumpTop()
	case "end", "G":
		m.jumpBottom()
	case "enter":
		m.openDetails()
	case "esc":
		if m.showMap {
			m.showMap = false
		} else {
			m.focus = 0
		}
	}
	return m, nil
}

func (m *Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.showUFSelect || m.showMap {
		return m, nil
	}
	if m.showHelp || m.showDetails {
		if msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress {
			m.showHelp = false
			m.showDetails = false
		}
		return m, nil
	}
	if tea.MouseEvent(msg).IsWheel() {
		delta := -1
		if msg.Button == tea.MouseButtonWheelDown {
			delta = 1
		}
		for _, h := range m.hitboxes {
			if msg.Y != h.y || msg.X < h.x1 || msg.X > h.x2 {
				continue
			}
			if h.kind == hitCandidate {
				m.focus = 0
				m.selected = h.index
				m.move(delta)
			} else {
				m.focus = 1
				m.eventScroll += delta
				m.clampEventScroll()
			}
			return m, nil
		}
		if m.focus == 0 {
			m.move(delta)
		} else {
			m.eventScroll += delta
			m.clampEventScroll()
		}
		return m, nil
	}
	if msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress {
		for _, h := range m.hitboxes {
			if msg.Y != h.y || msg.X < h.x1 || msg.X > h.x2 {
				continue
			}
			if h.kind == hitCandidate {
				m.focus = 0
				m.selected = h.index
			} else {
				m.focus = 1
				m.ensureEventVisible(h.index)
			}
			return m, nil
		}
	}
	return m, nil
}

func (m *Model) viewSnapshot() *domain.Snapshot {
	if m.status.UFView != nil && m.status.UFView.Snapshot != nil {
		return m.status.UFView.Snapshot
	}
	return m.status.Snapshot
}

func (m *Model) viewPrevious() *domain.Snapshot {
	if m.status.UFView != nil && m.status.UFView.Snapshot != nil {
		return nil
	}
	return m.status.PreviousSnapshot
}

func (m *Model) canShowUF() bool {
	s := m.status.Snapshot
	return s != nil && s.Election.Scope.Kind == domain.ScopeBrazil && strings.EqualFold(s.Election.Level, "federal")
}

func (m *Model) openUFSelect() {
	m.showUFSelect = true
	m.ufIndex = 0
	if m.status.UFView != nil {
		for i, code := range ufCodes {
			if code == m.status.UFView.UF {
				m.ufIndex = i
				break
			}
		}
	}
	m.clampUF()
}

func (m *Model) clampUF() {
	if m.ufIndex < 0 {
		m.ufIndex = 0
	}
	if m.ufIndex >= len(ufCodes) {
		m.ufIndex = len(ufCodes) - 1
	}
	if m.ufScroll < 0 {
		m.ufScroll = 0
	}
	if m.ufIndex < m.ufScroll {
		m.ufScroll = m.ufIndex
	}
	visible := m.ufVisibleRows()
	if visible > 0 && m.ufIndex >= m.ufScroll+visible {
		m.ufScroll = m.ufIndex - visible + 1
	}
	if maxScroll := len(ufCodes) - visible; maxScroll >= 0 && m.ufScroll > maxScroll {
		m.ufScroll = maxScroll
	}
}

func (m *Model) ufVisibleRows() int {
	rows := m.height - 14
	if rows < 6 {
		rows = 6
	}
	if rows > len(ufCodes) {
		rows = len(ufCodes)
	}
	return rows
}

func (m *Model) chooseUF() {
	if m.ufIndex < 0 || m.ufIndex >= len(ufCodes) {
		return
	}
	code := ufCodes[m.ufIndex]
	if m.src != nil {
		if code == "br" {
			m.src.ClearUF()
		} else {
			m.src.SelectUF(code)
		}
	}
	m.showUFSelect = false
}

var ufCodes = []string{
	"br", "ac", "al", "ap", "am", "ba", "ce", "df", "es", "go", "ma", "mt", "ms", "mg",
	"pa", "pb", "pr", "pe", "pi", "rj", "rn", "rs", "ro", "rr", "sc", "sp", "se", "to",
}

func ufLabel(code string) string {
	if code == "br" {
		return "Brasil (consolidado)"
	}
	return tse.StateName(code) + " (" + strings.ToUpper(code) + ")"
}

func (m *Model) ensureMapGrid(width, logicalHeight int) error {
	mv := m.status.Map
	if mv == nil {
		return nil
	}
	key := fmt.Sprintf("%d:%d:%d", width, logicalHeight, mv.FetchedAt.UnixNano())
	if key == m.mapGridKey && m.mapGrid != nil {
		return nil
	}
	grid, err := chart.RasterStates(width, logicalHeight)
	if err != nil {
		return err
	}
	m.mapGrid = grid
	m.mapGridKey = key
	return nil
}

func (m *Model) candidateCount() int {
	s := m.viewSnapshot()
	if s == nil {
		return 0
	}
	return len(s.Candidates)
}

func (m *Model) eventCount() int { return len(m.status.Events) }

func (m *Model) move(delta int) {
	if m.focus == 0 {
		n := m.candidateCount()
		if n == 0 {
			return
		}
		m.selected += delta
		m.clampSelection()
		return
	}
	m.eventScroll += delta
	m.clampEventScroll()
}

func (m *Model) jumpTop() {
	if m.focus == 0 {
		m.selected = 0
		m.candScroll = 0
	} else {
		m.eventScroll = 0
	}
}

func (m *Model) jumpBottom() {
	if m.focus == 0 {
		n := m.candidateCount()
		if n == 0 {
			return
		}
		m.selected = n - 1
		m.clampSelection()
		return
	}
	m.eventScroll = m.eventCount() - 1
	m.clampEventScroll()
}

func (m *Model) clampSelection() {
	n := m.candidateCount()
	if n == 0 {
		m.selected = 0
		m.candScroll = 0
		return
	}
	if m.selected < 0 {
		m.selected = 0
	}
	if m.selected >= n {
		m.selected = n - 1
	}
	if m.visibleCandRows > 0 {
		if m.selected < m.candScroll {
			m.candScroll = m.selected
		}
		if m.selected >= m.candScroll+m.visibleCandRows {
			m.candScroll = m.selected - m.visibleCandRows + 1
		}
	}
	if m.candScroll < 0 {
		m.candScroll = 0
	}
	if m.candScroll > n-1 {
		m.candScroll = n - 1
	}
	m.clampEventScroll()
}

func (m *Model) clampEventScroll() {
	n := m.eventCount()
	if n == 0 {
		m.eventScroll = 0
		return
	}
	if m.eventScroll < 0 {
		m.eventScroll = 0
	}
	if m.eventScroll > n-1 {
		m.eventScroll = n - 1
	}
}

func (m *Model) ensureEventVisible(index int) {
	m.eventScroll = index
	m.clampEventScroll()
}

func (m *Model) openDetails() {
	if m.focus == 0 && m.candidateCount() > 0 {
		m.detailKind = detailsCandidate
		m.detailIndex = m.selected
		m.showDetails = true
		return
	}
	if m.focus == 1 && m.eventCount() > 0 {
		m.detailKind = detailsEvent
		m.detailIndex = m.eventScroll
		m.showDetails = true
	}
}

func (m *Model) moveDetail(delta int) {
	if m.detailKind == detailsCandidate {
		n := m.candidateCount()
		if n == 0 {
			return
		}
		m.detailIndex = (m.detailIndex + delta + n) % n
		m.selected = m.detailIndex
		m.clampSelection()
		return
	}
	n := m.eventCount()
	if n == 0 {
		return
	}
	m.detailIndex = (m.detailIndex + delta + n) % n
	m.eventScroll = m.detailIndex
}
