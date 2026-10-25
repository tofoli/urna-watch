package notification

import (
	"strings"
	"testing"
	"time"

	"github.com/gpu/urna-watch/internal/domain"
	"github.com/gpu/urna-watch/internal/telegram"
)

func sampleSnapshot() *domain.Snapshot {
	info := domain.ElectionInfo{
		Cycle:        "ele2026",
		ElectionCode: "6258",
		ElectionName: "Eleição Ordinária Federal - 2026 2º Turno",
		Round:        2,
		ContestCode:  "1",
		ContestName:  "Presidente",
		Scope:        domain.Scope{Kind: domain.ScopeBrazil, UF: "br"},
		ScopeName:    "Brasil",
		SourcePage:   "https://resultados.tse.jus.br/oficial/app/index.html",
	}
	s := &domain.Snapshot{
		Election:            info,
		CapturedAt:          time.Date(2026, 10, 25, 18, 0, 0, 0, time.UTC),
		SourceVersion:       "3948414",
		GeneratedAt:         time.Date(2026, 10, 25, 17, 6, 31, 0, time.UTC),
		TotalizedAt:         time.Date(2026, 10, 25, 17, 6, 27, 0, time.UTC),
		VotesPublished:      true,
		SectionsTotal:       domain.Known(499248),
		SectionsCounted:     domain.Known(1225),
		SectionsPercentNano: 245369035,
		Totals: domain.Totals{
			TotalVotes:   domain.Known(345054),
			ValidVotes:   domain.Known(335743),
			NominalVotes: domain.Known(335743),
			BlankVotes:   domain.Known(4158),
			NullVotes:    domain.Known(5153),
		},
		Candidates: []domain.Candidate{
			{Number: "13", Name: "LUIZ INÁCIO LULA DA SILVA", BallotName: "LULA", PartyAcronym: "PT", Votes: domain.Known(193574), PercentNano: 57655409048, PercentText: "57,66"},
			{Number: "22", Name: "FLAVIO NANTES BOLSONARO", BallotName: "FLAVIO BOLSONARO", PartyAcronym: "PL", Votes: domain.Known(142169), PercentNano: 42344590952, PercentText: "42,34"},
		},
		SourceURL: "https://resultados.tse.jus.br/oficial/ele2026/6258/dados/br/br-c0001-e006258-u.json",
	}
	s.SortCandidates()
	return s
}

func TestBaselineMessageUsesSourceValues(t *testing.T) {
	f := Formatter{}
	msg := f.Baseline(sampleSnapshot())
	for _, want := range []string{
		"Linha de base", "Eleição Ordinária Federal - 2026 2º Turno", "Presidente",
		"Brasil", "2º turno", "25/10/2026 17:06:27", "LULA", "PT",
		"193.574", "57,66", "FLAVIO BOLSONARO", "142.169", "42,34",
		"1.225 de 499.248", "335.743", "Fonte oficial:", "Em apuração",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message missing %q:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "eleito") {
		t.Fatalf("partial data must not claim elected:\n%s", msg)
	}
}

func TestUpdateMessageAndRevision(t *testing.T) {
	prev := sampleSnapshot()
	cur := sampleSnapshot()
	cur.SourceVersion = "3949999"
	cur.Candidates[0].Votes = domain.Known(200000)
	cur.Candidates[0].PercentText = "58,00"
	cur.Candidates[0].PercentNano = 58000000000
	cur.Candidates[1].Votes = domain.Known(140000)
	set := domain.ChangeSet{
		Changed:  true,
		Revision: false,
		Previous: prev,
		Current:  cur,
		Items: []domain.Change{
			{Kind: domain.KindVotes, CandidateNumber: "13", CandidateName: "LULA", Previous: "193.574", Current: "200.000", Delta: 6426},
			{Kind: domain.KindVotes, CandidateNumber: "22", CandidateName: "FLAVIO BOLSONARO", Previous: "142.169", Current: "140.000", Delta: -2169},
		},
	}
	msg := Formatter{}.Update(set)
	for _, want := range []string{"Atualização da apuração", "Mudanças desde a última atualização", "LULA", "193.574 → 200.000", "+6.426 votos", "FLAVIO BOLSONARO", "142.169 → 140.000", "-2.169 votos"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message missing %q:\n%s", want, msg)
		}
	}
	set.Revision = true
	if !strings.Contains(Formatter{}.Update(set), "Revisão de dados publicados") {
		t.Fatal("revision header missing")
	}
	set.Revision = false
	cur.IsFinal = true
	if !strings.Contains(Formatter{}.Update(set), "Totalização final") {
		t.Fatal("final header missing")
	}
}

func TestMessageEscapesHTML(t *testing.T) {
	s := sampleSnapshot()
	s.Candidates[0].BallotName = `<b>LULA</b> & Cia`
	msg := Formatter{}.Baseline(s)
	if strings.Contains(msg, "<b>LULA</b>") {
		t.Fatalf("unsafe HTML leaked:\n%s", msg)
	}
	if !strings.Contains(msg, "&lt;b&gt;LULA&lt;/b&gt; &amp; Cia") {
		t.Fatalf("escaped name missing:\n%s", msg)
	}
}

func TestMessageDoesNotInventTimestamp(t *testing.T) {
	s := sampleSnapshot()
	s.TotalizedAt = time.Time{}
	msg := Formatter{}.Baseline(s)
	if !strings.Contains(msg, "não informado pela fonte") {
		t.Fatalf("message = %s", msg)
	}
}

func TestFailureAndRecovery(t *testing.T) {
	s := sampleSnapshot()
	msg := Formatter{}.Failure(s, "HTTP <500> & erro", time.Date(2026, 10, 25, 17, 0, 0, 0, time.UTC))
	if !strings.Contains(msg, "Falha persistente") || !strings.Contains(msg, "HTTP &lt;500&gt; &amp; erro") {
		t.Fatalf("message = %s", msg)
	}
	rec := Formatter{}.Recovery(s, 4)
	if !strings.Contains(rec, "Coleta recuperada") || !strings.Contains(rec, "4 falha") {
		t.Fatalf("message = %s", rec)
	}
}

func TestBuildRowsDeterministic(t *testing.T) {
	now := time.Date(2026, 10, 25, 18, 0, 0, 0, time.UTC)
	text := strings.Repeat("linha de teste com acentuação\n", 400)
	rows1 := BuildRows("abc", text, now)
	rows2 := BuildRows("abc", text, now)
	if len(rows1) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(rows1))
	}
	if len(rows1) != len(rows2) {
		t.Fatal("non-deterministic chunk count")
	}
	seen := map[string]bool{}
	for i := range rows1 {
		if rows1[i].ID != rows2[i].ID {
			t.Fatal("non-deterministic id")
		}
		if seen[rows1[i].ID] {
			t.Fatal("duplicate id")
		}
		seen[rows1[i].ID] = true
		if rows1[i].Chunk != i+1 || rows1[i].Chunks != len(rows1) {
			t.Fatalf("chunk metadata = %+v", rows1[i])
		}
		if telegram.UTF16Length(rows1[i].Text) > telegram.MaxMessageLength {
			t.Fatalf("chunk too long: %d", telegram.UTF16Length(rows1[i].Text))
		}
		if rows1[i].State != domain.NotifyPending {
			t.Fatalf("state = %q", rows1[i].State)
		}
	}
}
