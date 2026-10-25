package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/tofoli/urna-watch/internal/chart"
	"github.com/tofoli/urna-watch/internal/domain"
	"github.com/tofoli/urna-watch/internal/format"
)

type disputePoint struct {
	at   time.Time
	diff int64
}

func disputePair(s *domain.Snapshot) (domain.Candidate, domain.Candidate, bool) {
	if s == nil || len(s.Candidates) < 2 {
		return domain.Candidate{}, domain.Candidate{}, false
	}
	a, b := s.Candidates[0], s.Candidates[1]
	if a.Number > b.Number {
		a, b = b, a
	}
	return a, b, true
}

func disputeGap(s *domain.Snapshot) (leader domain.Candidate, gapPercent, gapVotes int64, ok bool) {
	if s == nil || len(s.Candidates) < 2 {
		return domain.Candidate{}, 0, 0, false
	}
	leader = s.Candidates[0]
	runner := s.Candidates[1]
	gapPercent = leader.PercentNano - runner.PercentNano
	gapVotes = leader.Votes.OrZero() - runner.Votes.OrZero()
	if gapPercent < 0 {
		gapPercent = -gapPercent
	}
	if gapVotes < 0 {
		gapVotes = -gapVotes
	}
	return leader, gapPercent, gapVotes, true
}

func (m *Model) historySeries() (domain.Candidate, domain.Candidate, []disputePoint, bool) {
	snap := m.status.Snapshot
	a, b, ok := disputePair(snap)
	if !ok {
		return domain.Candidate{}, domain.Candidate{}, nil, false
	}
	points := make([]disputePoint, 0, len(m.status.History)+1)
	find := func(p domain.HistoryPoint) (int64, bool) {
		var pa, pb *domain.HistoryCandidate
		for i := range p.Candidates {
			c := &p.Candidates[i]
			if c.Number == a.Number {
				pa = c
			}
			if c.Number == b.Number {
				pb = c
			}
		}
		if pa == nil || pb == nil {
			return 0, false
		}
		return pa.PercentNano - pb.PercentNano, true
	}
	for _, p := range m.status.History {
		if diff, ok := find(p); ok {
			points = append(points, disputePoint{at: p.HistoryAt(), diff: diff})
		}
	}
	current := snap.HistoryPoint()
	curDiff, curOK := find(current)
	if curOK {
		lastDiff := int64(1 << 62)
		var lastAt time.Time
		if len(points) > 0 {
			lastDiff = points[len(points)-1].diff
			lastAt = points[len(points)-1].at
		}
		if curDiff != lastDiff || !current.HistoryAt().Equal(lastAt) {
			points = append(points, disputePoint{at: current.HistoryAt(), diff: curDiff})
		}
	}
	return a, b, points, true
}

func (m *Model) historyPanel(width, height int) []string {
	lines := []string{panelTitle("DISPUTA — BRASIL", width)}
	snap := m.status.Snapshot
	a, b, ok := disputePair(snap)
	if !ok {
		lines = append(lines, mutedStyle.Render(fit("  aguardando dois candidatos…", width)))
		return padLines(lines, height, width)
	}
	colorA := lipgloss.Color(chart.CandidateColorHex(a))
	colorB := lipgloss.Color(chart.CandidateColorHex(b))
	pctA := a.PercentText
	if pctA == "" {
		pctA = format.PercentNano(a.PercentNano)
	}
	pctB := b.PercentText
	if pctB == "" {
		pctB = format.PercentNano(b.PercentNano)
	}
	pcts := lipgloss.NewStyle().Foreground(colorA).Render(fit(a.DisplayName(), 16)) + " " + pctA + "%   " +
		lipgloss.NewStyle().Foreground(colorB).Render(fit(b.DisplayName(), 16)) + " " + pctB + "%"
	lines = append(lines, fit("  "+pcts, width))

	leader, gapP, gapV, hasGap := disputeGap(snap)
	if hasGap {
		leaderColor := lipgloss.Color(chart.CandidateColorHex(leader))
		gapText := fmt.Sprintf("%s +%s p.p. · +%s votos",
			leader.DisplayName(), format.PercentNano(gapP), format.Votes(gapV))
		lines = append(lines, lipgloss.NewStyle().Foreground(leaderColor).Bold(true).Render(fit("  "+gapText, width)))
	}

	a, b, points, ok := m.historySeries()
	if !ok || len(points) < 2 {
		lines = append(lines, mutedStyle.Render(fit("  histórico começa agora; aguarde novas atualizações", width)))
		return padLines(lines, height, width)
	}

	caption := "  Diferença " + format.Truncate(a.DisplayName(), 12) + " − " + format.Truncate(b.DisplayName(), 12) + " (p.p.)"
	lines = append(lines, mutedStyle.Render(fit(caption, width)))

	chartHeight := height - len(lines) - 1 // reserve footer
	if chartHeight < 3 {
		return padLines(lines, height, width)
	}
	chartWidth := width - 9
	if chartWidth < 12 {
		return padLines(lines, height, width)
	}
	values := resampleDiff(points, chartWidth)
	maxV, minV := int64(0), int64(0)
	for _, v := range values {
		if v > maxV {
			maxV = v
		}
		if v < minV {
			minV = v
		}
	}
	if maxV == 0 && minV == 0 {
		maxV = 100_000_000
		minV = -100_000_000
	}
	span := maxV - minV
	rowFor := func(v int64) int {
		return int((maxV - v) * int64(chartHeight-1) / span)
	}
	zeroRow := rowFor(0)
	grid := make([][]rune, chartHeight)
	colors := make([][]lipgloss.Color, chartHeight)
	for r := range grid {
		grid[r] = make([]rune, chartWidth)
		colors[r] = make([]lipgloss.Color, chartWidth)
	}
	colorA = lipgloss.Color(chart.CandidateColorHex(a))
	colorB = lipgloss.Color(chart.CandidateColorHex(b))
	for x, v := range values {
		y := rowFor(v)
		col := colorA
		if v < 0 {
			col = colorB
		}
		lo, hi := zeroRow, y
		if lo > hi {
			lo, hi = hi, lo
		}
		for r := lo; r <= hi; r++ {
			grid[r][x] = '█'
			colors[r][x] = col
		}
		if y == zeroRow {
			grid[y][x] = '•'
			colors[y][x] = lipgloss.Color("#94a3b8")
		}
	}
	cellCache := map[[2]string]string{}
	for r := 0; r < chartHeight; r++ {
		label := ""
		switch {
		case r == 0:
			label = signedPP(maxV)
		case r == zeroRow:
			label = "0,00"
		case r == chartHeight-1:
			label = signedPP(minV)
		}
		var bld strings.Builder
		bld.WriteString(padLeft(label, 7))
		bld.WriteString(" ")
		for x := 0; x < chartWidth; x++ {
			if ch := grid[r][x]; ch != 0 {
				key := [2]string{string(ch), string(colors[r][x])}
				cell, ok := cellCache[key]
				if !ok {
					cell = lipgloss.NewStyle().Foreground(colors[r][x]).Render(string(ch))
					cellCache[key] = cell
				}
				bld.WriteString(cell)
			} else if r == zeroRow {
				bld.WriteString(mutedStyle.Render("─"))
			} else {
				bld.WriteString(" ")
			}
		}
		lines = append(lines, fit(bld.String(), width))
	}
	from := points[0].at.Format("15:04")
	to := points[len(points)-1].at.Format("15:04")
	lines = append(lines, mutedStyle.Render(fit(fmt.Sprintf("  %s → %s · %d atualizações · fonte TSE", from, to, len(points)), width)))
	return padLines(lines, height, width)
}

func resampleDiff(points []disputePoint, cols int) []int64 {
	out := make([]int64, cols)
	if cols <= 0 || len(points) == 0 {
		return out
	}
	if len(points) == 1 || cols == 1 {
		for i := range out {
			out[i] = points[len(points)-1].diff
		}
		return out
	}
	last := len(points) - 1
	for x := 0; x < cols; x++ {
		t := int64(x) * int64(last)
		i0 := int(t / int64(cols-1))
		rem := t % int64(cols-1)
		if i0 >= last {
			out[x] = points[last].diff
			continue
		}
		d0 := points[i0].diff
		d1 := points[i0+1].diff
		out[x] = d0 + (d1-d0)*rem/int64(cols-1)
	}
	return out
}

func signedPP(nano int64) string {
	if nano < 0 {
		return "-" + format.PercentNano(-nano)
	}
	return "+" + format.PercentNano(nano)
}
