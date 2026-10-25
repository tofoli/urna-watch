// Package detector compares normalized snapshots. It never inspects raw JSON,
// so property order, whitespace and HTTP metadata cannot cause false events.
package detector

import (
	"sort"
	"strings"

	"github.com/tofoli/urna-watch/internal/domain"
	"github.com/tofoli/urna-watch/internal/format"
)

// Compare returns the meaningful differences between prev and cur.
func Compare(prev, cur *domain.Snapshot) domain.ChangeSet {
	set := domain.ChangeSet{
		Previous: prev,
		Current:  cur,
		Detected: cur.CapturedAt,
	}
	if cur == nil {
		return set
	}
	if prev == nil {
		set.Baseline = true
		set.Changed = true
		return set
	}

	prevByNumber := make(map[string]domain.Candidate, len(prev.Candidates))
	for _, c := range prev.Candidates {
		prevByNumber[c.Number] = c
	}
	curByNumber := make(map[string]domain.Candidate, len(cur.Candidates))
	for _, c := range cur.Candidates {
		curByNumber[c.Number] = c
	}

	for _, c := range cur.Candidates {
		p, ok := prevByNumber[c.Number]
		if !ok {
			set.Items = append(set.Items, domain.Change{
				Kind:            domain.KindCandidateIn,
				CandidateNumber: c.Number,
				CandidateName:   c.DisplayName(),
				Current:         format.Votes(c.Votes.OrZero()) + " votos",
				CurrentPosition: c.Position,
			})
			continue
		}
		votesChanged := p.Votes.OrZero() != c.Votes.OrZero()
		percentChanged := p.PercentNano != c.PercentNano
		positionChanged := p.Position != c.Position
		if votesChanged {
			delta := c.Votes.OrZero() - p.Votes.OrZero()
			if delta < 0 {
				set.Revision = true
			}
			set.Items = append(set.Items, domain.Change{
				Kind:             domain.KindVotes,
				CandidateNumber:  c.Number,
				CandidateName:    c.DisplayName(),
				Previous:         format.Votes(p.Votes.OrZero()),
				Current:          format.Votes(c.Votes.OrZero()),
				Delta:            delta,
				PreviousPosition: p.Position,
				CurrentPosition:  c.Position,
			})
		} else if percentChanged {
			set.Items = append(set.Items, domain.Change{
				Kind:             domain.KindPercent,
				CandidateNumber:  c.Number,
				CandidateName:    c.DisplayName(),
				Previous:         format.PercentNano(p.PercentNano) + "%",
				Current:          format.PercentNano(c.PercentNano) + "%",
				PreviousPosition: p.Position,
				CurrentPosition:  c.Position,
			})
		} else if positionChanged {
			set.Items = append(set.Items, domain.Change{
				Kind:             domain.KindPosition,
				CandidateNumber:  c.Number,
				CandidateName:    c.DisplayName(),
				Previous:         positionLabel(p.Position),
				Current:          positionLabel(c.Position),
				PreviousPosition: p.Position,
				CurrentPosition:  c.Position,
			})
		}
		if statusLabel(p) != statusLabel(c) {
			set.Items = append(set.Items, domain.Change{
				Kind:            domain.KindStatus,
				CandidateNumber: c.Number,
				CandidateName:   c.DisplayName(),
				Previous:        statusLabel(p),
				Current:         statusLabel(c),
			})
		}
	}
	for _, p := range prev.Candidates {
		if _, ok := curByNumber[p.Number]; !ok {
			set.Revision = true
			set.Items = append(set.Items, domain.Change{
				Kind:             domain.KindCandidateOut,
				CandidateNumber:  p.Number,
				CandidateName:    p.DisplayName(),
				Previous:         format.Votes(p.Votes.OrZero()) + " votos",
				PreviousPosition: p.Position,
			})
		}
	}

	addIntChange(&set, domain.KindSections, "seções totalizadas",
		prev.SectionsCounted, cur.SectionsCounted, false)
	addIntChange(&set, domain.KindSections, "total de seções",
		prev.SectionsTotal, cur.SectionsTotal, false)
	if prev.SectionsPercentNano != cur.SectionsPercentNano {
		set.Items = append(set.Items, domain.Change{
			Kind:     domain.KindSections,
			Previous: format.PercentNano(prev.SectionsPercentNano) + "%",
			Current:  format.PercentNano(cur.SectionsPercentNano) + "%",
		})
		if cur.SectionsPercentNano < prev.SectionsPercentNano {
			set.Revision = true
		}
	}
	addMeasureChange(&set, "votação total", prev.Totals.TotalVotes, cur.Totals.TotalVotes, true)
	addMeasureChange(&set, "votos válidos", prev.Totals.ValidVotes, cur.Totals.ValidVotes, true)
	addMeasureChange(&set, "votos brancos", prev.Totals.BlankVotes, cur.Totals.BlankVotes, true)
	addMeasureChange(&set, "votos nulos", prev.Totals.NullVotes, cur.Totals.NullVotes, true)
	addMeasureChange(&set, "votos nominais", prev.Totals.NominalVotes, cur.Totals.NominalVotes, true)
	addMeasureChange(&set, "comparecimento", prev.Totals.Turnout, cur.Totals.Turnout, true)
	// Abstention is derived from electorate - turnout, so it normally decreases
	// while turnout grows. A decrease is not a revision of published counts.
	addMeasureChange(&set, "abstenção", prev.Totals.Abstention, cur.Totals.Abstention, false)

	if prev.VotesPublished != cur.VotesPublished {
		set.Items = append(set.Items, domain.Change{
			Kind:     domain.KindPublication,
			Previous: publicationLabel(prev.VotesPublished),
			Current:  publicationLabel(cur.VotesPublished),
		})
	}
	if prev.IsFinal != cur.IsFinal {
		if prev.IsFinal && !cur.IsFinal {
			set.Revision = true
		}
		set.Items = append(set.Items, domain.Change{
			Kind:     domain.KindFinal,
			Previous: finalLabel(prev.IsFinal),
			Current:  finalLabel(cur.IsFinal),
		})
	}

	set.Changed = len(set.Items) > 0
	sortItems(set.Items)
	return set
}

func addIntChange(set *domain.ChangeSet, kind domain.ChangeKind, label string, prev, cur domain.Measure, allowDecrease bool) {
	if !prev.Known || !cur.Known || prev.Value == cur.Value {
		return
	}
	if !allowDecrease && cur.Value < prev.Value {
		set.Revision = true
	}
	set.Items = append(set.Items, domain.Change{
		Kind:     kind,
		Previous: format.Votes(prev.Value) + " " + label,
		Current:  format.Votes(cur.Value) + " " + label,
		Delta:    cur.Value - prev.Value,
	})
}

func addMeasureChange(set *domain.ChangeSet, label string, prev, cur domain.Measure, revisionOnDecrease bool) {
	if !prev.Known || !cur.Known || prev.Value == cur.Value {
		return
	}
	if revisionOnDecrease && cur.Value < prev.Value {
		set.Revision = true
	}
	set.Items = append(set.Items, domain.Change{
		Kind:     domain.KindTotals,
		Previous: format.Votes(prev.Value) + " " + label,
		Current:  format.Votes(cur.Value) + " " + label,
		Delta:    cur.Value - prev.Value,
	})
}

func statusLabel(c domain.Candidate) string {
	parts := []string{}
	if s := strings.TrimSpace(c.Status); s != "" {
		parts = append(parts, s)
	}
	if c.Elected != nil {
		if *c.Elected {
			parts = append(parts, "eleito")
		} else if !strings.Contains(strings.ToLower(c.Status), "turno") {
			parts = append(parts, "não eleito")
		}
	}
	if len(parts) == 0 {
		return "situação não informada"
	}
	return strings.Join(parts, " — ")
}

func positionLabel(p int) string {
	if p <= 0 {
		return "sem posição"
	}
	return "#" + itoa(p)
}

func publicationLabel(v bool) string {
	if v {
		return "votação divulgada"
	}
	return "votação ainda não divulgada"
}

func finalLabel(v bool) string {
	if v {
		return "totalização final informada pela fonte"
	}
	return "em apuração"
}

func sortItems(items []domain.Change) {
	order := map[domain.ChangeKind]int{
		domain.KindCandidateIn: 0, domain.KindCandidateOut: 1,
		domain.KindVotes: 2, domain.KindPercent: 3, domain.KindPosition: 4, domain.KindStatus: 5,
		domain.KindSections: 6, domain.KindTotals: 7, domain.KindPublication: 8, domain.KindFinal: 9,
	}
	sort.SliceStable(items, func(i, j int) bool {
		oi, oj := order[items[i].Kind], order[items[j].Kind]
		if oi != oj {
			return oi < oj
		}
		if items[i].CurrentPosition != items[j].CurrentPosition {
			return items[i].CurrentPosition < items[j].CurrentPosition
		}
		return items[i].CandidateNumber < items[j].CandidateNumber
	})
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var b [8]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}

// Summarize builds a short single-line description for logs and the UI.
func Summarize(set domain.ChangeSet) string {
	if set.Baseline {
		return "linha de base criada"
	}
	if !set.Changed {
		return "sem alterações relevantes"
	}
	counts := map[domain.ChangeKind]int{}
	for _, item := range set.Items {
		counts[item.Kind]++
	}
	parts := []string{}
	if counts[domain.KindVotes] > 0 {
		parts = append(parts, plural(counts[domain.KindVotes], "candidato atualizado", "candidatos atualizados"))
	}
	if counts[domain.KindSections] > 0 {
		parts = append(parts, "progresso de seções")
	}
	if counts[domain.KindTotals] > 0 {
		parts = append(parts, "totais atualizados")
	}
	if counts[domain.KindFinal] > 0 {
		parts = append(parts, "estado final alterado")
	}
	if counts[domain.KindPublication] > 0 {
		parts = append(parts, "divulgação de votos alterada")
	}
	if counts[domain.KindStatus] > 0 || counts[domain.KindCandidateIn] > 0 || counts[domain.KindCandidateOut] > 0 {
		parts = append(parts, "situação de candidatos alterada")
	}
	if set.Revision {
		parts = append(parts, "revisão de dados publicados")
	}
	if len(parts) == 0 {
		return "alteração detectada"
	}
	return strings.Join(parts, "; ")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return itoa(n) + " " + many
}
