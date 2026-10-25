package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/gpu/urna-watch/internal/chart"
	"github.com/gpu/urna-watch/internal/domain"
	"github.com/gpu/urna-watch/internal/format"
)

const (
	headerLineCount = 5
	footerLineCount = 3
)

// View renders the dashboard. It also records mouse hitboxes, which is why it
// has a pointer receiver.
func (m *Model) View() string {
	if m.width <= 0 {
		m.width = 100
	}
	if m.height <= 0 {
		m.height = 30
	}
	m.hitboxes = nil
	m.visibleCandRows = 0

	header := m.headerLines()
	footer := m.footerLines()
	bodyHeight := m.height - len(header) - len(footer)
	if bodyHeight < 3 {
		bodyHeight = 3
	}

	var body []string
	switch {
	case m.showHelp:
		body = m.helpBody(bodyHeight)
	case m.showUFSelect:
		body = m.ufBody(bodyHeight)
	case m.showMap:
		body = m.mapBody(bodyHeight)
	case m.showDetails:
		body = m.detailsBody(bodyHeight)
	case m.width >= 100:
		body = m.wideBody(bodyHeight)
	default:
		body = m.stackedBody(bodyHeight)
	}
	if len(body) < bodyHeight {
		for len(body) < bodyHeight {
			body = append(body, "")
		}
	}
	if len(body) > bodyHeight {
		body = body[:bodyHeight]
	}

	lines := make([]string, 0, m.height)
	lines = append(lines, header...)
	lines = append(lines, body...)
	lines = append(lines, footer...)
	for len(lines) < m.height {
		lines = append(lines, "")
	}
	if len(lines) > m.height {
		// Keep the header and footer when the terminal is extremely small.
		lines = append(lines[:headerLineCount], lines[len(lines)-footerLineCount:]...)
		if len(lines) > m.height {
			lines = lines[:m.height]
		}
	}
	for i := range lines {
		lines[i] = fit(lines[i], m.width)
	}
	return strings.Join(lines, "\n")
}

func (m *Model) headerLines() []string {
	s := m.status
	election := "aguardando configuração oficial"
	cargo := ""
	scope := ""
	round := ""
	view := m.viewSnapshot()
	if view != nil {
		election = view.Election.ElectionName
		cargo = view.Election.ContestName
		scope = view.Election.ScopeName
		round = view.Election.RoundLabel()
	} else if s.Ref.ElectionCode != "" {
		election = "Eleição " + s.Ref.ElectionCode
		cargo = "cargo " + s.Ref.ContestCode
		scope = s.Ref.Scope.Key()
	}

	state, style := m.collectionState()
	lastSuccess := s.Poll.LastSuccess
	if lastSuccess.IsZero() {
		lastSuccess = s.Source.LastSuccess
	}
	age := format.Relative(lastSuccess, time.Now())
	next := "—"
	if s.Paused {
		next = "pausado"
	} else if !s.Poll.NextRun.IsZero() {
		next = format.Relative(s.Poll.NextRun, time.Now())
	}
	http := "—"
	if s.Source.LastAttempt.IsZero() {
		http = "sem requisição"
	} else if s.Source.NotModified {
		http = "HTTP 304"
	} else if s.Source.StatusCode > 0 {
		http = fmt.Sprintf("HTTP %d", s.Source.StatusCode)
	}
	telegram := m.telegramStatus()

	line1 := titleStyle.Render("URNA WATCH") + "  " + mutedStyle.Render("· acompanhamento oficial") + "  " + fit(election, max(10, m.width-42))
	line2Suffix := ""
	if s.UFView != nil {
		switch {
		case s.UFView.Loading:
			scope = "carregando " + s.UFView.Name + " (" + strings.ToUpper(s.UFView.UF) + ")"
			line2Suffix = "  ·  visão por UF (consolidado permanece ativo)"
		case s.UFView.Snapshot != nil:
			line2Suffix = "  ·  visão por UF (não altera o consolidado)"
		}
	}
	line2 := mutedStyle.Render(cargo+" · "+scope+" · "+round+line2Suffix) + "  " + mutedStyle.Render("·  fonte: TSE")
	line3 := style.Render(state) + "  " + mutedStyle.Render("· coleta "+age+" · próxima "+next+" · "+http+" · "+telegram)
	line4 := ""
	if s.UFView != nil && s.UFView.Error != "" {
		line4 = badStyle.Render("⚠ UF " + strings.ToUpper(s.UFView.UF) + ": " + fit(s.UFView.Error, m.width-12))
	} else if s.Error != "" {
		line4 = badStyle.Render("⚠ " + fit(s.Error, m.width-3))
	} else if s.Source.URL != "" {
		line4 = mutedStyle.Render("fonte: " + fit(s.Source.URL, m.width-8))
	} else {
		line4 = mutedStyle.Render("fonte: " + s.BaseURL + "/" + s.Environment)
	}
	return []string{fit(line1, m.width), fit(line2, m.width), fit(line3, m.width), fit(line4, m.width), ""}
}

func (m *Model) collectionState() (string, interface{ Render(...string) string }) {
	s := m.status
	switch {
	case s.Paused:
		return "PAUSADO", warnStyle
	case s.Poll.Running:
		return "COLETANDO", accentStyle
	case s.Poll.ConsecutiveFailures > 0:
		return "FALHA", badStyle
	case s.Poll.LastSuccess.IsZero():
		return "AGUARDANDO", warnStyle
	case s.Poll.Interval > 0 && time.Since(s.Poll.LastSuccess) > 3*s.Poll.Interval:
		return "DESATUALIZADO", warnStyle
	default:
		return "OK", okStyle
	}
}

func (m *Model) telegramStatus() string {
	t := m.status.Telegram
	if !t.Enabled {
		return "Telegram desativado"
	}
	switch {
	case t.Pending > 0:
		return fmt.Sprintf("Telegram %d pendente(s)", t.Pending)
	case t.LastErrMsg != "" && time.Since(t.LastError) < 10*time.Minute:
		return "Telegram com erro recente"
	case !t.LastSent.IsZero():
		return "Telegram em dia"
	default:
		return "Telegram pronto"
	}
}

// wideBody renders the candidate table on the left and aggregate/event panels
// on the right.
func (m *Model) wideBody(height int) []string {
	if height < 6 {
		return m.stackedBody(height)
	}
	leftWidth := int(float64(m.width) * 0.63)
	if leftWidth < 60 {
		leftWidth = 60
	}
	if leftWidth > 112 {
		leftWidth = 112
	}
	rightWidth := m.width - leftWidth - 1
	if rightWidth < 40 {
		rightWidth = 40
		leftWidth = m.width - rightWidth - 1
	}
	if leftWidth < 40 || rightWidth < 34 {
		return m.stackedBody(height)
	}

	mapHeight := 0
	totalsHeight := 15
	eventsHeight := height - totalsHeight - 1
	if eventsHeight < 5 {
		totalsHeight = height - 6
		eventsHeight = 5
	}
	if m.mapAvailable() {
		totalsHeight = 10
		eventsHeight = 4
		mapHeight = height - totalsHeight - eventsHeight - 2
		if mapHeight < 8 {
			totalsHeight = 8
			eventsHeight = 4
			mapHeight = height - totalsHeight - eventsHeight - 2
		}
		if mapHeight < 6 {
			mapHeight = 0
			totalsHeight = 15
			eventsHeight = height - totalsHeight - 1
			if eventsHeight < 5 {
				totalsHeight = height - 6
				eventsHeight = 5
			}
		}
	}

	candHeight := height
	historyHeight := 0
	if m.historyAvailable() && height >= 18 {
		desired := len(m.status.Snapshot.Candidates) + 3
		if desired < 6 {
			desired = 6
		}
		if maxCand := height - 9; desired > maxCand {
			desired = maxCand
		}
		if desired < 6 {
			desired = 6
		}
		if candidateHistory := height - desired - 1; candidateHistory >= 8 {
			candHeight = desired
			historyHeight = candidateHistory
		}
	}
	leftPanel, candRows, visible := m.candidatesPanel(leftWidth, candHeight)
	m.visibleCandRows = visible
	left := append([]string(nil), leftPanel...)
	if historyHeight > 0 {
		left = append(left, "")
		left = append(left, m.historyPanel(leftWidth, historyHeight)...)
	}
	var rightMap []string
	if mapHeight > 0 {
		rightMap = m.mapPanel(rightWidth, mapHeight)
	}
	rightTotals := m.totalsPanel(rightWidth, totalsHeight, historyHeight == 0)
	rightEvents, eventRows := m.eventsPanel(rightWidth, eventsHeight)
	totalsStart := 0
	if mapHeight > 0 {
		totalsStart = mapHeight + 1
	}
	eventBase := totalsStart + totalsHeight + 1

	out := make([]string, 0, height)
	for i := 0; i < height; i++ {
		l := ""
		if i < len(left) {
			l = left[i]
		}
		r := ""
		switch {
		case mapHeight > 0 && i < mapHeight:
			r = rightMap[i]
		case mapHeight > 0 && i == mapHeight:
			r = ""
		case i < totalsStart+totalsHeight:
			idx := i - totalsStart
			if idx >= 0 && idx < len(rightTotals) {
				r = rightTotals[idx]
			}
		case i == totalsStart+totalsHeight:
			r = ""
		default:
			idx := i - eventBase
			if idx >= 0 && idx < len(rightEvents) {
				r = rightEvents[idx]
			}
		}
		if candidate, ok := candRows[i]; ok {
			y := headerLineCount + i
			m.hitboxes = append(m.hitboxes, hitbox{x1: 0, x2: leftWidth - 1, y: y, kind: hitCandidate, index: candidate})
		}
		if idx := i - eventBase; idx >= 0 {
			if rel, ok := eventRows[idx]; ok {
				y := headerLineCount + i
				m.hitboxes = append(m.hitboxes, hitbox{x1: leftWidth + 1, x2: m.width - 1, y: y, kind: hitEvent, index: rel})
			}
		}
		out = append(out, padRight(l, leftWidth)+" "+padRight(r, rightWidth))
	}
	return out
}

func (m *Model) historyAvailable() bool {
	if m.status.UFView != nil && m.status.UFView.Snapshot != nil {
		return false
	}
	return m.status.Snapshot != nil && len(m.status.Snapshot.Candidates) >= 2
}

func (m *Model) mapAvailable() bool {
	s := m.status.Snapshot
	return s != nil && s.Election.Scope.Kind == domain.ScopeBrazil && strings.EqualFold(s.Election.Level, "federal")
}

func (m *Model) stackedBody(height int) []string {
	candHeight := int(float64(height) * 0.52)
	if candHeight < 8 {
		candHeight = 8
	}
	totalsHeight := 11
	eventsHeight := height - candHeight - totalsHeight - 2
	if eventsHeight < 4 {
		totalsHeight = 9
		eventsHeight = height - candHeight - totalsHeight - 2
	}
	if eventsHeight < 3 {
		candHeight = height - totalsHeight - 5
		if candHeight < 6 {
			candHeight = 6
		}
		eventsHeight = height - candHeight - totalsHeight - 2
	}
	if eventsHeight < 1 {
		eventsHeight = 1
	}

	cand, candRows, visible := m.candidatesPanel(m.width, candHeight)
	m.visibleCandRows = visible
	totals := m.totalsPanel(m.width, totalsHeight, true)
	events, eventRows := m.eventsPanel(m.width, eventsHeight)

	out := make([]string, 0, height)
	out = append(out, cand...)
	for i := 0; i < len(cand); i++ {
		if idx, ok := candRows[i]; ok {
			m.hitboxes = append(m.hitboxes, hitbox{x1: 0, x2: m.width - 1, y: headerLineCount + i, kind: hitCandidate, index: idx})
		}
	}
	out = append(out, "")
	out = append(out, totals...)
	out = append(out, "")
	eventBase := len(out)
	out = append(out, events...)
	for i := range events {
		if idx, ok := eventRows[i]; ok {
			m.hitboxes = append(m.hitboxes, hitbox{x1: 0, x2: m.width - 1, y: headerLineCount + eventBase + i, kind: hitEvent, index: idx})
		}
	}
	for len(out) < height {
		out = append(out, "")
	}
	if len(out) > height {
		out = out[:height]
	}
	return out
}

func (m *Model) candidatesPanel(width, height int) ([]string, map[int]int, int) {
	rows := height - 3
	if rows < 1 {
		rows = 1
	}
	title := "CANDIDATOS"
	if m.status.UFView != nil && m.status.UFView.Snapshot != nil {
		title += " — " + strings.ToUpper(m.status.UFView.UF)
	}
	lines := []string{
		panelTitle(title, width),
		m.candidateHeader(width),
		rule(width),
	}
	rowMap := map[int]int{}
	snap := m.viewSnapshot()
	if snap == nil || len(snap.Candidates) == 0 {
		lines = append(lines, mutedStyle.Render(fit("  aguardando a primeira coleta válida…", width)))
	} else {
		prev := map[string]domain.Candidate{}
		if p := m.viewPrevious(); p != nil {
			for _, c := range p.Candidates {
				prev[c.Number] = c
			}
		}
		for i := 0; i < rows; i++ {
			idx := m.candScroll + i
			if idx >= len(snap.Candidates) {
				lines = append(lines, "")
				continue
			}
			c := snap.Candidates[idx]
			pc, ok := prev[c.Number]
			selected := idx == m.selected && m.focus == 0
			lines = append(lines, m.candidateRow(c, pc, ok, width, selected))
			rowMap[len(lines)-1] = idx
		}
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	return lines, rowMap, rows
}

func (m *Model) candidateHeader(width int) string {
	cols := candidateColumns(width)
	parts := []string{
		padLeft(" #", cols.pos),
		padRight("Nº", cols.number),
		padRight("CANDIDATO", cols.name),
	}
	if cols.party > 0 {
		parts = append(parts, padRight("PART.", cols.party))
	}
	parts = append(parts, padLeft("VOTOS", cols.votes), padLeft("%", cols.percent))
	if cols.delta > 0 {
		parts = append(parts, padLeft("Δ", cols.delta))
	}
	if cols.status > 0 {
		parts = append(parts, padRight("SITUAÇÃO", cols.status))
	}
	return mutedStyle.Render(fit("  "+strings.Join(parts, " "), width))
}

type columns struct {
	pos, number, name, party, votes, percent, delta, status int
}

func candidateColumns(width int) columns {
	c := columns{pos: 2, number: 4, votes: 12, percent: 6}
	if width >= 70 {
		c.party = 6
	}
	if width >= 82 {
		c.delta = 8
	}
	if width >= 92 {
		c.status = 11
	}
	cols := 5
	fixed := c.pos + c.number + c.votes + c.percent
	if c.party > 0 {
		cols++
		fixed += c.party
	}
	if c.delta > 0 {
		cols++
		fixed += c.delta
	}
	if c.status > 0 {
		cols++
		fixed += c.status
	}
	fixed += cols - 1 // separators
	c.name = width - 2 - fixed
	if c.name < 8 {
		c.name = 8
	}
	return c
}

func (m *Model) candidateRow(cur, prev domain.Candidate, hasPrev bool, width int, selected bool) string {
	cols := candidateColumns(width)
	status := candidateStatus(cur)
	parts := []string{
		padLeft(fmt.Sprintf("%d", cur.Position), cols.pos),
		padRight(cur.Number, cols.number),
		padRight(cur.DisplayName(), cols.name),
	}
	if cols.party > 0 {
		parts = append(parts, padRight(cur.PartyAcronym, cols.party))
	}
	pctText := cur.PercentText
	if pctText == "" {
		pctText = format.PercentNano(cur.PercentNano)
	}
	parts = append(parts,
		padLeft(format.Votes(cur.Votes.OrZero()), cols.votes),
		padLeft(pctText+"%", cols.percent),
	)
	if cols.delta > 0 {
		parts = append(parts, padLeft(deltaText(cur.Votes, prev.Votes, hasPrev), cols.delta))
	}
	if cols.status > 0 {
		parts = append(parts, padRight(status, cols.status))
	}
	plain := fit("  "+strings.Join(parts, " "), width)
	if selected {
		return selectedStyle.Render(padRight(plain, width))
	}
	if len(parts) > 1 {
		colored := "  " + accentStyle.Render(parts[0]) + " " + strings.Join(parts[1:], " ")
		return fit(colored, width)
	}
	return plain
}

func (m *Model) totalsPanel(width, height int, showGap bool) []string {
	title := "APURAÇÃO"
	if m.status.UFView != nil && m.status.UFView.Snapshot != nil {
		title = "APURAÇÃO — " + strings.ToUpper(m.status.UFView.UF)
	}
	lines := []string{panelTitle(title, width)}
	if m.status.UFView != nil {
		label := "  VISÃO POR UF: " + m.status.UFView.Name + " (" + strings.ToUpper(m.status.UFView.UF) + ")"
		if m.status.UFView.FetchedAt != (time.Time{}) {
			label += " · " + format.Relative(m.status.UFView.FetchedAt, time.Now())
		}
		if m.status.UFView.Loading {
			label = "  carregando visão por UF: " + m.status.UFView.Name
		}
		lines = append(lines, mutedStyle.Render(fit(label, width)))
	}
	snap := m.viewSnapshot()
	if snap == nil {
		lines = append(lines, mutedStyle.Render(fit("  sem snapshot válido", width)))
		lines = append(lines, "")
		lines = append(lines, mutedStyle.Render(fit("  a coleta inicial criará a linha de base;", width)))
		lines = append(lines, mutedStyle.Render(fit("  nenhum dado é inventado enquanto isso.", width)))
		return padLines(lines, height, width)
	}
	if showGap {
		if leader, gapP, gapV, ok := disputeGap(snap); ok {
			col := lipgloss.Color(chart.CandidateColorHex(leader))
			text := fmt.Sprintf("  %s +%s p.p. · +%s votos", leader.DisplayName(), format.PercentNano(gapP), format.Votes(gapV))
			lines = append(lines, lipgloss.NewStyle().Foreground(col).Bold(true).Render(fit(text, width)))
		}
	}
	sections := "  Seções: não informado"
	if snap.SectionsCounted.Known && snap.SectionsTotal.Known {
		sections = fmt.Sprintf("  Seções: %s / %s", format.Votes(snap.SectionsCounted.Value), format.Votes(snap.SectionsTotal.Value))
	}
	lines = append(lines, fit(sections, width))
	if snap.SectionsCounted.Known && snap.SectionsTotal.Known {
		lines = append(lines, "  "+progressBar(snap.SectionsPercentNano, max(4, width-4)))
		lines = append(lines, mutedStyle.Render(fit("  "+format.PercentNano(snap.SectionsPercentNano)+"% totalizado", width)))
	} else {
		lines = append(lines, mutedStyle.Render(fit("  progresso não informado", width)))
	}
	lines = append(lines, "")
	lines = append(lines, fit("  Votação: "+measure(snap.Totals.TotalVotes), width))
	lines = append(lines, fit("  Válidos: "+measure(snap.Totals.ValidVotes), width))
	lines = append(lines, fit("  Brancos: "+measure(snap.Totals.BlankVotes)+" · Nulos: "+measure(snap.Totals.NullVotes), width))
	lines = append(lines, fit("  Comparecimento: "+measure(snap.Totals.Turnout), width))
	lines = append(lines, fit("  Abstenção: "+measure(snap.Totals.Abstention), width))
	lines = append(lines, "")
	state := snap.StateLabel()
	if snap.IsFinal {
		lines = append(lines, "  "+okStyle.Render(fit(state, max(1, width-2))))
	} else {
		lines = append(lines, "  "+warnStyle.Render(fit(state, max(1, width-2))))
	}
	updated := "não informado"
	if !snap.TotalizedAt.IsZero() {
		updated = format.DateTime(snap.TotalizedAt)
	}
	lines = append(lines, mutedStyle.Render(fit("  Fonte: "+updated, width)))
	lines = append(lines, mutedStyle.Render(fit("  IDG: "+snap.SourceVersion, width)))
	lines = append(lines, mutedStyle.Render(fit("  "+m.telegramStatus(), width)))
	return padLines(lines, height, width)
}

func measure(m domain.Measure) string {
	if !m.Known {
		return "não informado"
	}
	return format.Votes(m.Value)
}

func (m *Model) eventsPanel(width, height int) ([]string, map[int]int) {
	lines := []string{panelTitle("EVENTOS", width)}
	rows := height - 2
	if rows < 1 {
		rows = 1
	}
	rowMap := map[int]int{}
	events := m.status.Events
	if len(events) == 0 {
		lines = append(lines, mutedStyle.Render(fit("  nenhuma alteração detectada ainda", width)))
	} else {
		for i := 0; i < rows; i++ {
			idx := m.eventScroll + i
			if idx >= len(events) {
				lines = append(lines, "")
				continue
			}
			e := events[idx]
			glyph := notificationGlyph(e.NotificationStatus)
			line := fmt.Sprintf("  %s  %s  %s", e.DetectedAt.Format("15:04:05"), glyph, e.Summary)
			line = fit(line, width)
			if m.focus == 1 && idx == m.eventScroll {
				line = selectedStyle.Render(padRight(line, width))
			} else if e.NotificationStatus == domain.NotifyFailed {
				line = badStyle.Render(line)
			}
			lines = append(lines, line)
			rowMap[len(lines)-1] = idx
		}
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	return lines, rowMap
}

func (m *Model) ufBody(height int) []string {
	m.clampUF()
	visible := m.ufVisibleRows()
	content := []string{
		titleStyle.Render("VISÃO POR UF"),
		mutedStyle.Render("Consolidado Brasil continua sendo monitorado e notificado."),
		"",
	}
	for i := m.ufScroll; i < len(ufCodes) && i < m.ufScroll+visible; i++ {
		label := "  " + ufLabel(ufCodes[i])
		if i == m.ufIndex {
			content = append(content, selectedStyle.Render(padRight(label, 48)))
		} else {
			content = append(content, label)
		}
	}
	content = append(content, "", mutedStyle.Render("↑/↓ escolher · enter carregar · esc fechar"))
	return centerBox(content, m.width, height)
}

func (m *Model) mapBody(height int) []string {
	title := panelTitle("MAPA — maioria por UF", m.width)
	mv := m.status.Map
	if mv == nil || mv.Loading {
		return padLines([]string{title, "", accentStyle.Render("  carregando os 27 arquivos de UF…"), "", mutedStyle.Render("m/esc fechar")}, height, m.width)
	}
	if mv.Error != "" {
		return padLines([]string{title, "", badStyle.Render("  " + fit(mv.Error, m.width-4)), "", mutedStyle.Render("m/esc fechar")}, height, m.width)
	}
	if m.status.Snapshot == nil {
		return padLines([]string{title, "", mutedStyle.Render("  sem snapshot consolidado"), ""}, height, m.width)
	}
	mapRows := height - 4
	if mapRows < 5 {
		mapRows = 5
	}
	width := min(m.width-4, 112)
	if width < 40 {
		width = 40
	}
	grid, err := m.renderMapGrid(width, mapRows)
	if err != nil {
		return padLines([]string{title, "", badStyle.Render("  " + fit(err.Error(), m.width-4)), "", mutedStyle.Render("m/esc fechar")}, height, m.width)
	}
	lines := []string{title}
	for _, line := range grid {
		lines = append(lines, center(line, m.width))
	}
	lines = append(lines, m.mapLegend(m.width, false))
	note := "Cinza = sem votos · cor = candidato com mais votos na UF · "
	if !mv.FetchedAt.IsZero() {
		note += "carregado " + format.Relative(mv.FetchedAt, time.Now())
	}
	lines = append(lines, mutedStyle.Render("  "+fit(note, m.width-4)))
	lines = append(lines, mutedStyle.Render("  m/esc fechar · o consolidado Brasil continua sendo monitorado"))
	return padLines(lines, height, m.width)
}

func (m *Model) mapPanel(width, height int) []string {
	title := panelTitle("MAPA — UF VENCEDORA", width)
	mv := m.status.Map
	if mv == nil || mv.Loading {
		return padLines([]string{title, mutedStyle.Render(fit("  carregando 27 UFs…", width)), ""}, height, width)
	}
	if mv.Error != "" {
		return padLines([]string{title, badStyle.Render(fit("  mapa indisponível", width)), mutedStyle.Render(fit("  m para detalhes", width))}, height, width)
	}
	if m.status.Snapshot == nil {
		return padLines([]string{title, mutedStyle.Render(fit("  sem snapshot", width))}, height, width)
	}
	rows := height - 2
	if rows < 3 {
		rows = 3
	}
	grid, err := m.renderMapGrid(width, rows)
	if err != nil {
		return padLines([]string{title, badStyle.Render(fit("  "+err.Error(), width))}, height, width)
	}
	lines := []string{title}
	for _, line := range grid {
		lines = append(lines, center(line, width))
	}
	lines = append(lines, m.mapLegend(width, true))
	return padLines(lines, height, width)
}

func (m *Model) mapColors() map[string]lipgloss.Color {
	colors := make(map[string]lipgloss.Color)
	if main := m.status.Snapshot; main != nil {
		for _, c := range main.Candidates {
			colors[c.Number] = lipgloss.Color(chart.CandidateColorHex(c))
		}
	}
	return colors
}

func (m *Model) renderMapGrid(width, rows int) ([]string, error) {
	if err := m.ensureMapGrid(width, rows*2); err != nil {
		return nil, err
	}
	mv := m.status.Map
	colors := m.mapColors()
	colorFor := func(uf string) lipgloss.Color {
		if uf == "" {
			return mapOcean
		}
		st, ok := mv.States[strings.ToLower(uf)]
		if !ok || !st.Counted || st.WinnerNumber == "" {
			return mapGray
		}
		if c, ok := colors[st.WinnerNumber]; ok {
			return c
		}
		return mapGray
	}
	cache := map[[2]string]string{}
	cell := func(top, bottom string) string {
		t, b := colorFor(top), colorFor(bottom)
		key := [2]string{string(t), string(b)}
		if s, ok := cache[key]; ok {
			return s
		}
		var s string
		if top == "" && bottom == "" {
			s = lipgloss.NewStyle().Background(mapOcean).Render(" ")
		} else {
			s = lipgloss.NewStyle().Foreground(t).Background(b).Render("▀")
		}
		cache[key] = s
		return s
	}
	lines := make([]string, 0, rows)
	for row := 0; row < rows && row*2+1 < len(m.mapGrid); row++ {
		var b strings.Builder
		topRow := m.mapGrid[row*2]
		bottomRow := m.mapGrid[row*2+1]
		for x := 0; x < width; x++ {
			var top, bottom string
			if x < len(topRow) {
				top = topRow[x]
			}
			if x < len(bottomRow) {
				bottom = bottomRow[x]
			}
			b.WriteString(cell(top, bottom))
		}
		lines = append(lines, b.String())
	}
	return lines, nil
}

func (m *Model) mapLegend(width int, compact bool) string {
	main := m.status.Snapshot
	if main == nil {
		return ""
	}
	colors := m.mapColors()
	parts := make([]string, 0, 4)
	for i, c := range main.Candidates {
		if i >= 4 {
			break
		}
		name := c.DisplayName()
		if compact {
			name = format.Truncate(name, 8)
		}
		pct := c.PercentText
		if pct == "" {
			pct = format.PercentNano(c.PercentNano)
		}
		swatch := lipgloss.NewStyle().Foreground(colors[c.Number]).Render("■")
		parts = append(parts, swatch+" "+name+" "+pct+"%")
	}
	return fit("  "+strings.Join(parts, "   "), width)
}

func (m *Model) helpBody(height int) []string {
	content := []string{
		titleStyle.Render("ATALHOS"),
		"",
		"↑/↓ ou j/k   selecionar candidato / rolar eventos",
		"tab          alternar foco entre candidatos e eventos",
		"enter        abrir detalhes do item selecionado",
		"p            pausar / retomar o polling",
		"r            solicitar atualização imediata",
		"c / e        focar candidatos / eventos",
		"u            abrir visão por UF (eleição federal)",
		"b            voltar ao consolidado Brasil",
		"m            mapa do Brasil por UF vencedora",
		"g / G        ir ao início / fim",
		"h ou ?       esta ajuda",
		"q            sair",
		"",
		"mouse: clique para selecionar; roda para rolar.",
		mutedStyle.Render("O polling e o Telegram continuam no processo principal."),
		mutedStyle.Render("Nenhum dado é inventado: a tela mostra apenas o último snapshot válido."),
		"",
		"esc          fechar",
	}
	return centerBox(content, m.width, height)
}

func (m *Model) detailsBody(height int) []string {
	var content []string
	switch m.detailKind {
	case detailsCandidate:
		content = m.candidateDetails()
	case detailsEvent:
		content = m.eventDetails()
	default:
		content = []string{"Sem detalhes."}
	}
	return centerBox(content, m.width, height)
}

func (m *Model) candidateDetails() []string {
	snap := m.viewSnapshot()
	if snap == nil || m.detailIndex < 0 || m.detailIndex >= len(snap.Candidates) {
		return []string{titleStyle.Render("CANDIDATO"), "", "Nenhum candidato selecionado."}
	}
	c := snap.Candidates[m.detailIndex]
	status := candidateStatus(c)
	party := c.Party
	if c.PartyAcronym != "" {
		if party != "" {
			party = c.PartyAcronym + " — " + party
		} else {
			party = c.PartyAcronym
		}
	}
	line := []string{
		titleStyle.Render("CANDIDATO"),
		"",
		fit(c.DisplayName(), max(20, min(64, m.width-12))),
		fmt.Sprintf("Nº %s · %s · posição %d", c.Number, party, c.Position),
		"",
		fmt.Sprintf("Votos: %s (%s%%)", format.Votes(c.Votes.OrZero()), c.PercentText),
		"Situação: " + status,
	}
	if prev := m.viewPrevious(); prev != nil {
		for _, p := range prev.Candidates {
			if p.Number == c.Number {
				line = append(line, "Variação de votos: "+deltaText(c.Votes, p.Votes, true))
				break
			}
		}
	}
	line = append(line,
		"",
		"Atualização da fonte: "+format.DateTime(snap.TotalizedAt),
		"Apuração: "+snap.StateLabel(),
	)
	if m.status.UFView != nil && m.status.UFView.Snapshot != nil {
		line = append(line, "Visão por UF: "+m.status.UFView.Name+" — o consolidado Brasil continua sendo monitorado")
	}
	if snap.SourceURL != "" {
		line = append(line, "Fonte: "+fit(snap.SourceURL, max(20, min(64, m.width-12))))
	}
	line = append(line, "", mutedStyle.Render("↑/↓ navegar · esc/enter fechar"))
	return line
}

func (m *Model) eventDetails() []string {
	events := m.status.Events
	if m.detailIndex < 0 || m.detailIndex >= len(events) {
		return []string{titleStyle.Render("EVENTO"), "", "Nenhum evento selecionado."}
	}
	e := events[m.detailIndex]
	lines := []string{
		titleStyle.Render("EVENTO"),
		"",
		"Detectado: " + format.DateTime(e.DetectedAt),
		"Resumo: " + e.Summary,
		"Notificação: " + notificationLabel(e.NotificationStatus),
		"Versão da fonte: " + e.SourceVersion,
	}
	if e.Revision {
		lines = append(lines, warnStyle.Render("Revisão de dados previamente publicados"))
	}
	lines = append(lines, "")
	limit := max(4, min(14, m.height-14))
	for i, item := range e.Items {
		if i >= limit {
			lines = append(lines, fmt.Sprintf("… e mais %d item(ns)", len(e.Items)-limit))
			break
		}
		lines = append(lines, eventItemLine(item))
	}
	lines = append(lines, "", mutedStyle.Render("↑/↓ navegar · esc/enter fechar"))
	return lines
}

func eventItemLine(item domain.Change) string {
	switch item.Kind {
	case domain.KindVotes:
		return fmt.Sprintf("• %s: %s → %s", item.CandidateName, item.Previous, item.Current)
	case domain.KindSections, domain.KindTotals, domain.KindPublication, domain.KindFinal:
		return fmt.Sprintf("• %s → %s", item.Previous, item.Current)
	case domain.KindStatus:
		return fmt.Sprintf("• %s: %s → %s", item.CandidateName, item.Previous, item.Current)
	case domain.KindCandidateIn:
		return "• entrou: " + item.CandidateName
	case domain.KindCandidateOut:
		return "• saiu: " + item.CandidateName
	default:
		return fmt.Sprintf("• %s: %s → %s", item.CandidateName, item.Previous, item.Current)
	}
}

func centerBox(content []string, screenWidth, height int) []string {
	if len(content) == 0 {
		return nil
	}
	innerWidth := min(66, max(24, screenWidth-8))
	wrapped := make([]string, 0, len(content))
	for _, line := range content {
		wrapped = append(wrapped, fit(line, innerWidth))
	}
	box := modalStyle.Width(innerWidth).Render(strings.Join(wrapped, "\n"))
	boxLines := strings.Split(box, "\n")
	top := (height - len(boxLines)) / 2
	if top < 0 {
		top = 0
	}
	out := make([]string, 0, height)
	for i := 0; i < top; i++ {
		out = append(out, "")
	}
	for _, line := range boxLines {
		out = append(out, center(line, screenWidth))
	}
	for len(out) < height {
		out = append(out, "")
	}
	if len(out) > height {
		out = out[:height]
	}
	return out
}

func (m *Model) footerLines() []string {
	help := "↑/↓ selecionar · tab painéis · u UF · b Brasil · m mapa · p pausar · r atualizar · enter detalhes · h ajuda · q sair"
	hint := "mouse: clique nas linhas · roda: rolagem · Δ = variação desde o snapshot anterior · fonte: JSON oficial do TSE"
	if m.width < 80 {
		help = "↑/↓ · tab · u UF · b BR · m mapa · p · r · enter · h · q"
		hint = "mouse: clique/roda · Δ = variação · fonte: TSE"
	}
	return []string{rule(m.width), mutedStyle.Render(fit(help, m.width)), mutedStyle.Render(fit(hint, m.width))}
}

func padLines(lines []string, height, width int) []string {
	for len(lines) < height {
		lines = append(lines, "")
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	for i := range lines {
		lines[i] = fit(lines[i], width)
	}
	return lines
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
