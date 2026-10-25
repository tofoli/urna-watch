// Package notification builds conservative, source-grounded messages and
// manages the durable Telegram outbox.
package notification

import (
	"fmt"
	"strings"
	"time"

	"github.com/tofoli/urna-watch/internal/domain"
	"github.com/tofoli/urna-watch/internal/format"
)

// Formatter renders Telegram HTML messages. The message is intentionally
// short: the official page link is the place for full details.
type Formatter struct {
	MaxCandidates int
	MaxChanges    int
}

func (f Formatter) maxCandidates() int {
	if f.MaxCandidates <= 0 {
		return 5
	}
	return f.MaxCandidates
}

func (f Formatter) maxChanges() int {
	if f.MaxChanges <= 0 {
		return 6
	}
	return f.MaxChanges
}

func header(title string) string {
	return "📊 <b>Urna Watch — " + title + "</b>"
}

func (f Formatter) contextBlock(s *domain.Snapshot) string {
	year := electionYear(s)
	title := s.Election.ContestName
	if year != "" {
		title += " · " + year
	}
	title += " · " + s.Election.RoundLabel()
	line2 := s.Election.ScopeName
	if !s.TotalizedAt.IsZero() {
		line2 += " · atualizado " + format.DateTime(s.TotalizedAt)
	}
	return "🗳️ <b>" + format.EscapeHTML(title) + "</b>\n" + format.EscapeHTML(line2)
}

// Baseline renders the single initial summary.
func (f Formatter) Baseline(s *domain.Snapshot) string {
	var b strings.Builder
	b.WriteString(header("primeiro snapshot"))
	b.WriteString("\n\n")
	b.WriteString(f.contextBlock(s))
	b.WriteString("\n\n")
	b.WriteString(f.candidatesBlock(s))
	b.WriteString("\n\n")
	b.WriteString(f.progressBlock(s))
	if link := f.sourceBlock(s); link != "" {
		b.WriteString("\n\n")
		b.WriteString(link)
	}
	return b.String()
}

// Update renders one detected change set.
func (f Formatter) Update(set domain.ChangeSet) string {
	s := set.Current
	title := "atualização"
	if set.Revision {
		title = "revisão de dados"
	} else if s.IsFinal {
		title = "totalização final"
	}
	var b strings.Builder
	b.WriteString(header(title))
	b.WriteString("\n\n")
	b.WriteString(f.contextBlock(s))
	b.WriteString("\n\n")
	b.WriteString(f.candidatesBlock(s))
	if changes := f.changesBlock(set); changes != "" {
		b.WriteString("\n\n<b>Mudanças</b>\n")
		b.WriteString(changes)
	}
	b.WriteString("\n\n")
	b.WriteString(f.progressBlock(s))
	if link := f.sourceBlock(s); link != "" {
		b.WriteString("\n\n")
		b.WriteString(link)
	}
	return b.String()
}

func (f Formatter) candidatesBlock(s *domain.Snapshot) string {
	var b strings.Builder
	limit := f.maxCandidates()
	for i, c := range s.Candidates {
		if i >= limit {
			fmt.Fprintf(&b, "\n… e mais %d candidato(s)", len(s.Candidates)-limit)
			break
		}
		name := format.EscapeHTML(c.DisplayName())
		party := ""
		if c.PartyAcronym != "" {
			party = " (" + format.EscapeHTML(c.PartyAcronym) + ")"
		}
		pct := c.PercentText
		if pct == "" {
			pct = format.PercentNano(c.PercentNano)
		}
		fmt.Fprintf(&b, "%d. <b>%s</b>%s — %s%% · %s votos",
			c.Position, name, party, format.EscapeHTML(pct), format.Votes(c.Votes.OrZero()))
		if status := candidateSuffix(c); status != "" {
			b.WriteString(" · ")
			b.WriteString(status)
		}
		if i < limit-1 {
			b.WriteString("\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func candidateSuffix(c domain.Candidate) string {
	if c.Elected != nil && *c.Elected {
		return "✅ eleito"
	}
	if strings.TrimSpace(c.Status) != "" {
		return format.EscapeHTML(strings.TrimSpace(c.Status))
	}
	return ""
}

type changeLine struct {
	order   int
	text    string
	hasText bool
}

func (f Formatter) changesBlock(set domain.ChangeSet) string {
	if !set.Changed || set.Current == nil {
		return ""
	}
	current := make(map[string]domain.Candidate, len(set.Current.Candidates))
	for _, c := range set.Current.Candidates {
		current[c.Number] = c
	}
	lines := make([]changeLine, 0, len(set.Items))
	seen := map[string]int{}
	add := func(key, text string) {
		if text == "" {
			return
		}
		if idx, ok := seen[key]; ok {
			lines[idx].text = text
			lines[idx].hasText = true
			return
		}
		seen[key] = len(lines)
		lines = append(lines, changeLine{order: len(lines), text: text, hasText: true})
	}
	for _, item := range set.Items {
		switch item.Kind {
		case domain.KindVotes:
			c := current[item.CandidateNumber]
			pct := c.PercentText
			if pct == "" {
				pct = format.PercentNano(c.PercentNano)
			}
			sign := "+"
			delta := item.Delta
			if delta < 0 {
				sign = "-"
				delta = -delta
			}
			add("vote-"+item.CandidateNumber, fmt.Sprintf("• %s %s%s (%s%%)",
				format.EscapeHTML(item.CandidateName), sign, format.Votes(delta), format.EscapeHTML(pct)))
		case domain.KindPercent:
			add("pct-"+item.CandidateNumber, fmt.Sprintf("• %s: %s → %s",
				format.EscapeHTML(item.CandidateName), item.Previous, item.Current))
		case domain.KindCandidateIn:
			add("in-"+item.CandidateNumber, "• entrou: "+format.EscapeHTML(item.CandidateName))
		case domain.KindCandidateOut:
			add("out-"+item.CandidateNumber, "• saiu: "+format.EscapeHTML(item.CandidateName))
		case domain.KindStatus:
			if item.Previous == "situação não informada" && item.Current == "" {
				continue
			}
			add("status-"+item.CandidateNumber, fmt.Sprintf("• %s: %s → %s",
				format.EscapeHTML(item.CandidateName), format.EscapeHTML(item.Previous), format.EscapeHTML(item.Current)))
		}
	}
	if set.Previous != nil && set.Previous.SectionsCounted.Known && set.Current.SectionsCounted.Known {
		prev := set.Previous.SectionsCounted.Value
		cur := set.Current.SectionsCounted.Value
		if prev != cur {
			delta := cur - prev
			sign := "+"
			if delta < 0 {
				sign = "-"
				delta = -delta
			}
			add("sections", fmt.Sprintf("• Seções %s%s (%s%%)", sign, format.Votes(delta), format.PercentNano(set.Current.SectionsPercentNano)))
		}
	}
	shown := 0
	var b strings.Builder
	for _, line := range lines {
		if !line.hasText {
			continue
		}
		if shown >= f.maxChanges() {
			fmt.Fprintf(&b, "\n… e mais %d mudança(s)", len(lines)-shown)
			break
		}
		if shown > 0 {
			b.WriteString("\n")
		}
		b.WriteString(line.text)
		shown++
	}
	if shown == 0 && set.Revision {
		return "• O TSE revisou números já publicados."
	}
	return b.String()
}

func (f Formatter) progressBlock(s *domain.Snapshot) string {
	if s.IsFinal {
		return "🏁 <b>Totalização final</b> informada pelo TSE"
	}
	if s.SectionsCounted.Known && s.SectionsTotal.Known && s.SectionsTotal.Value > 0 {
		return fmt.Sprintf("<b>Apuração:</b> %s%% das seções totalizadas (%s de %s)",
			format.PercentNano(s.SectionsPercentNano),
			format.Votes(s.SectionsCounted.Value),
			format.Votes(s.SectionsTotal.Value))
	}
	return "<b>Apuração:</b> em andamento (progresso não informado)"
}

func (f Formatter) sourceBlock(s *domain.Snapshot) string {
	url := strings.TrimSpace(s.Election.SourcePage)
	if url == "" {
		return ""
	}
	return `<a href="` + format.EscapeHTML(url) + `">Ver no site do TSE</a>`
}

// Failure renders a persistent collection failure.
func (f Formatter) Failure(s *domain.Snapshot, errText string, since time.Time) string {
	var b strings.Builder
	b.WriteString(header("falha na coleta"))
	b.WriteString("\n\n")
	b.WriteString("Não foi possível atualizar os resultados")
	if !since.IsZero() {
		b.WriteString(" desde " + format.DateTime(since))
	}
	b.WriteString(".\nO último dado válido continua disponível.")
	if strings.TrimSpace(errText) != "" {
		b.WriteString("\n\n<b>Motivo:</b> " + format.EscapeHTML(strings.TrimSpace(errText)))
	}
	if s != nil {
		if link := f.sourceBlock(s); link != "" {
			b.WriteString("\n\n")
			b.WriteString(link)
		}
	}
	return b.String()
}

// Recovery renders recovery after a failure streak.
func (f Formatter) Recovery(s *domain.Snapshot, failures int) string {
	var b strings.Builder
	b.WriteString(header("coleta recuperada"))
	b.WriteString("\n\n")
	fmt.Fprintf(&b, "A coleta voltou a responder após %d falha(s).\n\n", failures)
	b.WriteString(f.contextBlock(s))
	b.WriteString("\n\n")
	b.WriteString(f.candidatesBlock(s))
	b.WriteString("\n\n")
	b.WriteString(f.progressBlock(s))
	if link := f.sourceBlock(s); link != "" {
		b.WriteString("\n\n")
		b.WriteString(link)
	}
	return b.String()
}

func electionYear(s *domain.Snapshot) string {
	if s == nil {
		return ""
	}
	if !s.TotalizedAt.IsZero() {
		return fmt.Sprintf("%d", s.TotalizedAt.Year())
	}
	if !s.GeneratedAt.IsZero() {
		return fmt.Sprintf("%d", s.GeneratedAt.Year())
	}
	cycle := strings.TrimPrefix(s.Election.Cycle, "ele")
	if len(cycle) == 4 {
		return cycle
	}
	return ""
}
