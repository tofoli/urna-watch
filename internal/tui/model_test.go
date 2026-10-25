package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/gpu/urna-watch/internal/config"
	"github.com/gpu/urna-watch/internal/domain"
	"github.com/gpu/urna-watch/internal/notification"
	"github.com/gpu/urna-watch/internal/observability"
	"github.com/gpu/urna-watch/internal/polling"
)

type fakeSource struct {
	st         observability.Status
	paused     int
	resumed    int
	refreshed  int
	selectedUF string
	clearUF    int
	mapLoads   int
}

func (f *fakeSource) Status() observability.Status { return f.st }
func (f *fakeSource) Pause()                       { f.paused++ }
func (f *fakeSource) Resume()                      { f.resumed++ }
func (f *fakeSource) Refresh()                     { f.refreshed++ }
func (f *fakeSource) SelectUF(uf string)           { f.selectedUF = uf }
func (f *fakeSource) ClearUF()                     { f.clearUF++; f.selectedUF = "" }
func (f *fakeSource) LoadMap()                     { f.mapLoads++ }

func uiSnapshot() *domain.Snapshot {
	info := domain.ElectionInfo{
		Cycle:        "ele2026",
		ElectionCode: "6258",
		ElectionName: "Eleição Ordinária Federal - 2026 2º Turno",
		Round:        2,
		ContestCode:  "1",
		ContestName:  "Presidente",
		Level:        "federal",
		Scope:        domain.Scope{Kind: domain.ScopeBrazil, UF: "br"},
		ScopeName:    "Brasil",
	}
	s := &domain.Snapshot{
		Election:            info,
		CapturedAt:          time.Now(),
		SourceVersion:       "3948414",
		TotalizedAt:         time.Date(2026, 10, 25, 17, 6, 27, 0, time.UTC),
		VotesPublished:      true,
		SectionsTotal:       domain.Known(499248),
		SectionsCounted:     domain.Known(1225),
		SectionsPercentNano: 245369035,
		Totals: domain.Totals{
			TotalVotes: domain.Known(345054), ValidVotes: domain.Known(335743),
			BlankVotes: domain.Known(4158), NullVotes: domain.Known(5153),
		},
		Candidates: []domain.Candidate{
			{Number: "13", Name: "LUIZ INÁCIO LULA DA SILVA", BallotName: "LULA", PartyAcronym: "PT", Votes: domain.Known(193574), PercentNano: 57655409048, PercentText: "57,66"},
			{Number: "22", Name: "FLAVIO NANTES BOLSONARO", BallotName: "FLAVIO BOLSONARO", PartyAcronym: "PL", Votes: domain.Known(142169), PercentNano: 42344590952, PercentText: "42,34"},
			{Number: "27", Name: "CLARIANA BARAO", BallotName: "CLARIANA", PartyAcronym: "DC", Votes: domain.Known(40043), PercentNano: 11930000, PercentText: "0,03"},
		},
		SourceURL: "https://example.test/u.json",
	}
	s.SortCandidates()
	return s
}

func newFakeModel() (*Model, *fakeSource) {
	snap := uiSnapshot()
	src := &fakeSource{st: observability.Status{
		Version:          "test",
		StartedAt:        time.Now().Add(-time.Minute),
		Snapshot:         snap,
		PreviousSnapshot: nil,
		Events: []domain.EventRecord{
			{ID: "a", DetectedAt: time.Now().Add(-time.Minute), Summary: "2 candidatos atualizados", NotificationStatus: domain.NotifySent},
			{ID: "b", DetectedAt: time.Now().Add(-2 * time.Minute), Summary: "linha de base criada", NotificationStatus: domain.NotifyDisabled},
		},
		Poll: polling.Metrics{
			Started: true, LastSuccess: time.Now().Add(-20 * time.Second),
			NextRun: time.Now().Add(90 * time.Second), Interval: 120 * time.Second, Successes: 1,
		},
		Telegram:    notificationStatsDisabled(),
		Source:      observability.SourceStatus{StatusCode: 200, LastAttempt: time.Now(), LastSuccess: time.Now()},
		StatePath:   "data/state.json",
		BaseURL:     "https://resultados.tse.jus.br",
		Environment: "oficial",
		Timezone:    "America/Sao_Paulo",
	}}
	m := New(src, config.Default())
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return m, src
}

func notificationStatsDisabled() notification.Stats {
	return notification.Stats{}
}

func render(m *Model) []string {
	return strings.Split(m.View(), "\n")
}

func TestViewWideAndNarrow(t *testing.T) {
	m, _ := newFakeModel()
	for _, size := range [][2]int{{120, 40}, {100, 30}, {80, 24}, {40, 12}} {
		m.width, m.height = size[0], size[1]
		lines := render(m)
		if len(lines) != size[1] {
			t.Fatalf("%dx%d: lines = %d", size[0], size[1], len(lines))
		}
		for i, line := range lines {
			if w := ansi.StringWidth(line); w > size[0] {
				t.Fatalf("%dx%d line %d width %d: %q", size[0], size[1], i, w, ansi.Strip(line))
			}
		}
		view := strings.Join(lines, "\n")
		if !strings.Contains(view, "URNA WATCH") || !strings.Contains(view, "LULA") {
			t.Fatalf("%dx%d missing content:\n%s", size[0], size[1], view)
		}
	}
}

func TestKeyboardControls(t *testing.T) {
	m, src := newFakeModel()
	m.focus = 0
	m.selected = 0
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.selected != 1 {
		t.Fatalf("selected = %d", m.selected)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	if src.paused != 1 {
		t.Fatalf("paused = %d", src.paused)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	if src.resumed != 1 {
		t.Fatalf("resumed = %d", src.resumed)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if src.refreshed != 1 {
		t.Fatalf("refreshed = %d", src.refreshed)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.focus != 1 {
		t.Fatal("tab focus")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	if !m.showHelp {
		t.Fatal("help not opened")
	}
	if !strings.Contains(m.View(), "ATALHOS") {
		t.Fatal("help missing")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.showHelp {
		t.Fatal("help not closed")
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("quit command missing")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("cmd = %T", cmd())
	}
}

func TestMouseSelectionAndDetails(t *testing.T) {
	m, _ := newFakeModel()
	m.View()
	var target *hitbox
	for i := range m.hitboxes {
		if m.hitboxes[i].kind == hitCandidate && m.hitboxes[i].index == 1 {
			target = &m.hitboxes[i]
			break
		}
	}
	if target == nil {
		t.Fatal("candidate hitbox not found")
	}
	m.Update(tea.MouseMsg{X: target.x1 + 2, Y: target.y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if m.selected != 1 || m.focus != 0 {
		t.Fatalf("selected=%d focus=%d", m.selected, m.focus)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.showDetails {
		t.Fatal("details not opened")
	}
	view := m.View()
	if !strings.Contains(view, "CANDIDATO") || !strings.Contains(view, "FLAVIO BOLSONARO") {
		t.Fatalf("details view:\n%s", view)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.showDetails {
		t.Fatal("details not closed")
	}

	m.focus = 1
	m.eventScroll = 0
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.showDetails || m.detailKind != detailsEvent {
		t.Fatal("event details not opened")
	}
	if !strings.Contains(m.View(), "EVENTO") {
		t.Fatal("event details missing")
	}
}

func TestMouseWheel(t *testing.T) {
	m, _ := newFakeModel()
	m.View()
	var y int
	for _, h := range m.hitboxes {
		if h.kind == hitCandidate {
			y = h.y
			break
		}
	}
	if y == 0 {
		t.Fatal("no hitbox")
	}
	m.Update(tea.MouseMsg{X: 5, Y: y, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	if m.selected == 0 {
		t.Fatal("wheel did not move selection")
	}
}

func TestDetailsNavigation(t *testing.T) {
	m, _ := newFakeModel()
	m.selected = 0
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.showDetails {
		t.Fatal("no details")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.detailIndex != 1 || m.selected != 1 {
		t.Fatalf("detailIndex=%d selected=%d", m.detailIndex, m.selected)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.detailIndex != 0 {
		t.Fatalf("detailIndex=%d", m.detailIndex)
	}
}

func TestEmptySnapshotView(t *testing.T) {
	src := &fakeSource{st: observability.Status{
		StartedAt: time.Now(),
		Poll:      polling.Metrics{Interval: 120 * time.Second},
		Telegram:  notificationStatsDisabled(),
		BaseURL:   "https://resultados.tse.jus.br",
		Source:    observability.SourceStatus{LastError: "sem dados"},
		Error:     "sem dados",
	}}
	m := New(src, config.Default())
	m.width, m.height = 110, 30
	view := m.View()
	if !strings.Contains(view, "aguardando") && !strings.Contains(view, "Aguardando") {
		t.Fatalf("view:\n%s", view)
	}
}

func TestUFSelectorAndUFView(t *testing.T) {
	m, src := newFakeModel()
	m.View()
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	if !m.showUFSelect {
		t.Fatal("UF selector did not open")
	}
	view := m.View()
	if !strings.Contains(view, "VISÃO POR UF") || !strings.Contains(view, "Brasil (consolidado)") {
		t.Fatalf("selector view:\n%s", view)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.showUFSelect {
		t.Fatal("selector did not close")
	}
	if src.selectedUF != "ac" {
		t.Fatalf("selected UF = %q", src.selectedUF)
	}

	base := uiSnapshot()
	ufSnap := *base
	ufSnap.Election = base.Election
	ufSnap.Election.Scope = domain.Scope{Kind: domain.ScopeState, UF: "sp"}
	ufSnap.Election.ScopeName = "São Paulo (SP)"
	ufSnap.Candidates = append([]domain.Candidate(nil), base.Candidates...)
	ufSnap.Candidates[0].BallotName = "LULA SP"
	src.st.UFView = &observability.UFView{UF: "sp", Name: "São Paulo", Snapshot: &ufSnap}
	m.Update(tickMsg(time.Now()))
	view = m.View()
	if !strings.Contains(view, "LULA SP") || !strings.Contains(view, "VISÃO POR UF") {
		t.Fatalf("UF view missing:\n%s", view)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	if src.clearUF != 1 {
		t.Fatalf("clearUF = %d", src.clearUF)
	}
}

func TestMapView(t *testing.T) {
	m, src := newFakeModel()
	m.View()
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	if !m.showMap {
		t.Fatal("map view did not open")
	}
	if src.mapLoads != 1 {
		t.Fatalf("loadMap = %d", src.mapLoads)
	}
	if !strings.Contains(m.View(), "MAPA") || !strings.Contains(m.View(), "carregando") {
		t.Fatalf("loading map view:\n%s", m.View())
	}
	src.st.Map = &observability.MapView{
		FetchedAt: time.Now(),
		States: map[string]observability.MapState{
			"sp": {UF: "SP", Counted: true, WinnerNumber: "13", PercentText: "60,00"},
			"rj": {UF: "RJ", Counted: true, WinnerNumber: "22", PercentText: "55,00"},
		},
	}
	m.Update(tickMsg(time.Now()))
	view := m.View()
	if !strings.Contains(view, "MAPA") || !strings.Contains(view, "LULA") || !strings.Contains(view, "FLAVIO BOLSONARO") {
		t.Fatalf("map legend missing:\n%s", view)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.showMap {
		t.Fatal("map view did not close")
	}
}

func TestAutoLoadMapOnStartup(t *testing.T) {
	m, src := newFakeModel()
	cmd := m.autoLoadMapCmd()
	if cmd == nil {
		t.Fatal("expected automatic map load")
	}
	cmd()
	if src.mapLoads != 1 {
		t.Fatalf("mapLoads = %d", src.mapLoads)
	}
	if cmd2 := m.autoLoadMapCmd(); cmd2 != nil {
		t.Fatal("auto load requested twice for the same source version")
	}
}

func TestMapAppearsOnInitialWideScreen(t *testing.T) {
	m, src := newFakeModel()
	src.st.Map = &observability.MapView{
		FetchedAt: time.Now(),
		States: map[string]observability.MapState{
			"sp": {UF: "SP", Counted: true, WinnerNumber: "13", PercentText: "60,00"},
			"rj": {UF: "RJ", Counted: true, WinnerNumber: "22", PercentText: "55,00"},
		},
	}
	m.width, m.height = 120, 40
	m.Update(tickMsg(time.Now()))
	view := m.View()
	if !strings.Contains(view, "MAPA — UF VENCEDORA") {
		t.Fatalf("initial map missing:\n%s", view)
	}
	if !strings.Contains(view, "LULA") || !strings.Contains(view, "FLAVIO BOLSONARO") {
		t.Fatalf("map legend missing:\n%s", view)
	}
}
