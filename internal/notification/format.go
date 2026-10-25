// Package notification builds conservative, source-grounded messages and
// manages the durable Telegram outbox.
package notification

import (
	"fmt"
	"strings"
	"time"

	"github.com/gpu/urna-watch/internal/domain"
	"github.com/gpu/urna-watch/internal/format"
)

// Formatter renders Telegram HTML messages. Every label and value comes from
// the normalized snapshot; nothing is inferred.
type Formatter struct {
	MaxCandidates int
	MaxChanges    int
}

func (f Formatter) maxCandidates() int {
	if f.MaxCandidates <= 0 {
		return 10
	}
	return f.MaxCandidates
}

func (f Formatter) maxChanges() int {
	if f.MaxChanges <= 0 {
		return 15
	}
	return f.MaxChanges
}

func header(title string) string {
	return "📊 <b>Urna Watch — " + title + "</b>"
}

func (f Formatter) contextBlock(s *domain.Snapshot) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<b>Eleição:</b> %s\n", format.EscapeHTML(firstNonEmpty(s.Election.ElectionName, "não informada")))
	fmt.Fprintf(&b, "<b>Abrangência:</b> %s\n", format.EscapeHTML(s.Election.ScopeName))
	fmt.Fprintf(&b, "<b>Cargo:</b> %s\n", format.EscapeHTML(s.Election.ContestName))
	fmt.Fprintf(&b, "<b>Turno:</b> %s\n", format.EscapeHTML(s.Election.RoundLabel()))
	updated := "não informado pela fonte"
	if !s.TotalizedAt.IsZero() {
		updated = format.DateTime(s.TotalizedAt) + " (horário de Brasília)"
	}
	fmt.Fprintf(&b, "<b>Atualização da fonte:</b> %s", updated)
	return b.String()
}

// Baseline renders the single initial summary.
func (f Formatter) Baseline(s *domain.Snapshot) string {
	var b strings.Builder
	b.WriteString(header("Linha de base"))
	b.WriteString("\n\n")
	b.WriteString(f.contextBlock(s))
	b.WriteString("\n\n<b>Resultados publicados</b>\n")
	b.WriteString(f.candidatesBlock(s))
	b.WriteString("\n\n")
	b.WriteString(f.progressBlock(s))
	b.WriteString("\n\n")
	b.WriteString(f.sourceBlock(s))
	return b.String()
}

// Update renders one detected change set.
func (f Formatter) Update(set domain.ChangeSet) string {
	s := set.Current
	var b strings.Builder
	if set.Revision {
		b.WriteString(header("Revisão de dados publicados"))
	} else if s.IsFinal {
		b.WriteString(header("Totalização final"))
	} else {
		b.WriteString(header("Atualização da apuração"))
	}
	b.WriteString("\n\n")
	b.WriteString(f.contextBlock(s))
	b.WriteString("\n\n<b>Resultados publicados</b>\n")
	b.WriteString(f.candidatesBlock(s))
	if changes := f.changesBlock(set); changes != "" {
		b.WriteString("\n\n<b>Mudanças desde a última atualização</b>\n")
		b.WriteString(changes)
	}
	b.WriteString("\n\n")
	b.WriteString(f.progressBlock(s))
	b.WriteString("\n\n")
	b.WriteString(f.sourceBlock(s))
	return b.String()
}

func (f Formatter) candidatesBlock(s *domain.Snapshot) string {
	var b strings.Builder
	limit := f.maxCandidates()
	for i, c := range s.Candidates {
		if i >= limit {
			fmt.Fprintf(&b, "\n… e mais %d candidato(s). Consulte a fonte oficial.", len(s.Candidates)-limit)
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
		fmt.Fprintf(&b, "%d. %s%s — %s (%s%%)", c.Position, name, party, format.Votes(c.Votes.OrZero()), format.EscapeHTML(pct))
		if c.Status != "" {
			fmt.Fprintf(&b, " — %s", format.EscapeHTML(c.Status))
		}
		if i < limit-1 {
			b.WriteString("\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func (f Formatter) changesBlock(set domain.ChangeSet) string {
	if len(set.Items) == 0 {
		return ""
	}
	var b strings.Builder
	limit := f.maxChanges()
	shown := 0
	for _, item := range set.Items {
		if shown >= limit {
			fmt.Fprintf(&b, "… e mais %d alteração(ões).", len(set.Items)-limit)
			break
		}
		line := changeLine(item)
		if line == "" {
			continue
		}
		if shown > 0 {
			b.WriteString("\n")
		}
		b.WriteString("• ")
		b.WriteString(line)
		shown++
	}
	return b.String()
}

func changeLine(item domain.Change) string {
	switch item.Kind {
	case domain.KindVotes:
		name := format.EscapeHTML(item.CandidateName)
		sign := ""
		if item.Delta > 0 {
			sign = "+"
		}
		line := fmt.Sprintf("%s: %s → %s (%s%s votos)", name, item.Previous, item.Current, sign, format.Votes(item.Delta))
		if item.PreviousPosition > 0 && item.CurrentPosition > 0 && item.PreviousPosition != item.CurrentPosition {
			line += fmt.Sprintf("; posição %d → %d", item.PreviousPosition, item.CurrentPosition)
		}
		return line
	case domain.KindPercent:
		return fmt.Sprintf("%s: %s → %s", format.EscapeHTML(item.CandidateName), item.Previous, item.Current)
	case domain.KindPosition:
		return fmt.Sprintf("%s: posição %s → %s", format.EscapeHTML(item.CandidateName), item.Previous, item.Current)
	case domain.KindStatus:
		return fmt.Sprintf("%s: situação %s → %s", format.EscapeHTML(item.CandidateName), format.EscapeHTML(item.Previous), format.EscapeHTML(item.Current))
	case domain.KindCandidateIn:
		return fmt.Sprintf("%s entrou na lista (%s)", format.EscapeHTML(item.CandidateName), item.Current)
	case domain.KindCandidateOut:
		return fmt.Sprintf("%s saiu da lista (%s)", format.EscapeHTML(item.CandidateName), item.Previous)
	case domain.KindSections:
		if strings.Contains(item.Previous, "seções") {
			return fmt.Sprintf("%s → %s", item.Previous, item.Current)
		}
		return fmt.Sprintf("percentual de seções: %s → %s", item.Previous, item.Current)
	case domain.KindTotals:
		return fmt.Sprintf("%s → %s", item.Previous, item.Current)
	case domain.KindPublication:
		return fmt.Sprintf("%s → %s", item.Previous, item.Current)
	case domain.KindFinal:
		return fmt.Sprintf("estado: %s → %s", item.Previous, item.Current)
	default:
		return ""
	}
}

func (f Formatter) progressBlock(s *domain.Snapshot) string {
	var b strings.Builder
	b.WriteString("<b>Apuração:</b> ")
	b.WriteString(format.EscapeHTML(s.StateLabel()))
	if s.SectionsCounted.Known && s.SectionsTotal.Known {
		fmt.Fprintf(&b, "\nSeções totalizadas: %s de %s", format.Votes(s.SectionsCounted.Value), format.Votes(s.SectionsTotal.Value))
		if s.SectionsPercentNano > 0 || s.SectionsCounted.Value > 0 {
			fmt.Fprintf(&b, " (%s%%)", format.PercentNano(s.SectionsPercentNano))
		}
	}
	if s.Totals.ValidVotes.Known {
		fmt.Fprintf(&b, "\nVotos válidos: %s", format.Votes(s.Totals.ValidVotes.Value))
	}
	if s.Totals.BlankVotes.Known {
		fmt.Fprintf(&b, " · Brancos: %s", format.Votes(s.Totals.BlankVotes.Value))
	}
	if s.Totals.NullVotes.Known {
		fmt.Fprintf(&b, " · Nulos: %s", format.Votes(s.Totals.NullVotes.Value))
	}
	return b.String()
}

func (f Formatter) sourceBlock(s *domain.Snapshot) string {
	url := s.SourceURL
	if url == "" {
		url = s.Election.SourcePage
	}
	if url == "" {
		return "Fonte oficial: TSE"
	}
	return "Fonte oficial: " + format.EscapeHTML(url)
}

// Failure renders a persistent collection failure. Error text is already
// sanitized by the caller.
func (f Formatter) Failure(s *domain.Snapshot, errText string, since time.Time) string {
	var b strings.Builder
	b.WriteString(header("Falha persistente na coleta"))
	b.WriteString("\n\n")
	if s != nil {
		b.WriteString(f.contextBlock(s))
		b.WriteString("\n\n")
	}
	b.WriteString("<b>Última coleta bem-sucedida:</b> ")
	if since.IsZero() {
		b.WriteString("não registrada")
	} else {
		b.WriteString(format.DateTime(since))
	}
	b.WriteString("\n<b>Erro:</b> ")
	b.WriteString(format.EscapeHTML(errText))
	b.WriteString("\n\nOs dados exibidos continuam sendo o último snapshot válido. A coleta será retentada automaticamente.")
	return b.String()
}

// Recovery renders recovery after a failure streak.
func (f Formatter) Recovery(s *domain.Snapshot, failures int) string {
	var b strings.Builder
	b.WriteString(header("Coleta recuperada"))
	b.WriteString("\n\n")
	b.WriteString(f.contextBlock(s))
	fmt.Fprintf(&b, "\n\nA coleta voltou a responder após %d falha(s) consecutiva(s).", failures)
	b.WriteString("\n\n")
	b.WriteString(f.progressBlock(s))
	return b.String()
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
