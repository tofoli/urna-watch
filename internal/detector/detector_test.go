package detector

import (
	"testing"
	"time"

	"github.com/gpu/urna-watch/internal/domain"
)

func baseSnapshot() *domain.Snapshot {
	info := domain.ElectionInfo{
		Cycle:        "ele2026",
		ElectionCode: "6258",
		ElectionName: "Eleição Ordinária Federal - 2026 2º Turno",
		Round:        2,
		ContestCode:  "1",
		ContestName:  "Presidente",
		Scope:        domain.Scope{Kind: domain.ScopeBrazil, UF: "br"},
		ScopeName:    "Brasil",
	}
	s := &domain.Snapshot{
		Election:            info,
		CapturedAt:          time.Date(2026, 10, 25, 17, 0, 0, 0, time.UTC),
		SourceVersion:       "100",
		GeneratedAt:         time.Date(2026, 10, 25, 16, 59, 0, 0, time.UTC),
		TotalizedAt:         time.Date(2026, 10, 25, 16, 59, 0, 0, time.UTC),
		VotesPublished:      true,
		StateText:           "parcial",
		SectionsTotal:       domain.Known(1000),
		SectionsCounted:     domain.Known(100),
		SectionsPercentNano: 10_000_000_000,
		Totals: domain.Totals{
			TotalVotes:   domain.Known(1000),
			ValidVotes:   domain.Known(950),
			NominalVotes: domain.Known(950),
			BlankVotes:   domain.Known(30),
			NullVotes:    domain.Known(20),
			Turnout:      domain.Known(1000),
			Abstention:   domain.Known(200),
		},
		Candidates: []domain.Candidate{
			{Number: "13", Name: "LULA", BallotName: "LULA", PartyAcronym: "PT", Votes: domain.Known(600), PercentNano: 63_157_894_736},
			{Number: "22", Name: "FLAVIO", BallotName: "FLAVIO", PartyAcronym: "PL", Votes: domain.Known(350), PercentNano: 36_842_105_263},
		},
		SourceURL: "https://example.test/u.json",
	}
	s.SortCandidates()
	s.ID = s.Fingerprint()
	return s
}

func clone(s *domain.Snapshot) *domain.Snapshot {
	c := *s
	c.Candidates = append([]domain.Candidate(nil), s.Candidates...)
	return &c
}

func TestBaseline(t *testing.T) {
	set := Compare(nil, baseSnapshot())
	if !set.Baseline || !set.Changed {
		t.Fatalf("set = %+v", set)
	}
	if len(set.Items) != 0 {
		t.Fatalf("baseline items = %+v", set.Items)
	}
}

func TestNoChangeAndMetadataOnly(t *testing.T) {
	prev := baseSnapshot()
	cur := clone(prev)
	cur.CapturedAt = cur.CapturedAt.Add(time.Minute)
	cur.GeneratedAt = cur.GeneratedAt.Add(time.Minute)
	cur.TotalizedAt = cur.TotalizedAt.Add(time.Minute)
	cur.SourceVersion = "101"
	set := Compare(prev, cur)
	if set.Changed || len(set.Items) != 0 {
		t.Fatalf("metadata-only produced changes: %+v", set.Items)
	}
}

func TestVotesChangeIsDeterministic(t *testing.T) {
	prev := baseSnapshot()
	cur := clone(prev)
	cur.SourceVersion = "101"
	cur.Candidates[0].Votes = domain.Known(650)
	cur.Candidates[0].PercentNano = 65_000_000_000
	set := Compare(prev, cur)
	if !set.Changed || set.Revision {
		t.Fatalf("set = %+v", set)
	}
	if len(set.Items) != 1 || set.Items[0].Kind != domain.KindVotes || set.Items[0].Delta != 50 {
		t.Fatalf("items = %+v", set.Items)
	}
	if set.Items[0].Previous != "600" || set.Items[0].Current != "650" {
		t.Fatalf("values = %+v", set.Items[0])
	}
	if set.EventID() != Compare(prev, cur).EventID() {
		t.Fatal("event id is not deterministic")
	}
	other := clone(cur)
	other.Candidates[0].Votes = domain.Known(651)
	if set.EventID() == Compare(prev, other).EventID() {
		t.Fatal("different changes must have different ids")
	}
}

func TestPercentOnlyChange(t *testing.T) {
	prev := baseSnapshot()
	cur := clone(prev)
	cur.Candidates[1].PercentNano++
	set := Compare(prev, cur)
	if !set.Changed || len(set.Items) != 1 || set.Items[0].Kind != domain.KindPercent {
		t.Fatalf("items = %+v", set.Items)
	}
}

func TestSectionsAndTotals(t *testing.T) {
	prev := baseSnapshot()
	cur := clone(prev)
	cur.SectionsCounted = domain.Known(200)
	cur.SectionsPercentNano = 20_000_000_000
	cur.Totals.TotalVotes = domain.Known(2000)
	cur.Totals.ValidVotes = domain.Known(1900)
	set := Compare(prev, cur)
	if !set.Changed {
		t.Fatal("expected changes")
	}
	var sections, totals int
	for _, item := range set.Items {
		switch item.Kind {
		case domain.KindSections:
			sections++
		case domain.KindTotals:
			totals++
		}
	}
	if sections != 2 || totals != 2 {
		t.Fatalf("sections=%d totals=%d items=%+v", sections, totals, set.Items)
	}
}

func TestRevisionOnDecrease(t *testing.T) {
	prev := baseSnapshot()
	cur := clone(prev)
	cur.Candidates[0].Votes = domain.Known(500)
	cur.SectionsCounted = domain.Known(90)
	set := Compare(prev, cur)
	if !set.Revision {
		t.Fatal("expected revision")
	}
}

func TestFinalTransition(t *testing.T) {
	prev := baseSnapshot()
	cur := clone(prev)
	cur.IsFinal = true
	set := Compare(prev, cur)
	if !set.Changed {
		t.Fatal("expected change")
	}
	found := false
	for _, item := range set.Items {
		if item.Kind == domain.KindFinal {
			found = true
		}
	}
	if !found {
		t.Fatalf("no final item: %+v", set.Items)
	}
}

func TestCandidateAddedAndRemoved(t *testing.T) {
	prev := baseSnapshot()
	cur := clone(prev)
	cur.Candidates = cur.Candidates[:1]
	cur.SortCandidates()
	set := Compare(prev, cur)
	if !set.Revision {
		t.Fatal("removal should be a revision")
	}
	found := false
	for _, item := range set.Items {
		if item.Kind == domain.KindCandidateOut {
			found = true
		}
	}
	if !found {
		t.Fatalf("items = %+v", set.Items)
	}
}

func TestJSONOrderDoesNotMatter(t *testing.T) {
	prev := baseSnapshot()
	cur := clone(prev)
	// Reverse the slice but keep normalized positions. Detection uses candidate
	// numbers and assigned positions, not slice order.
	cur.Candidates[0], cur.Candidates[1] = cur.Candidates[1], cur.Candidates[0]
	set := Compare(prev, cur)
	if set.Changed {
		t.Fatalf("order-only change produced items: %+v", set.Items)
	}
}

func TestStatusChange(t *testing.T) {
	prev := baseSnapshot()
	cur := clone(prev)
	elected := true
	cur.Candidates[0].Status = "Eleito"
	cur.Candidates[0].Elected = &elected
	set := Compare(prev, cur)
	found := false
	for _, item := range set.Items {
		if item.Kind == domain.KindStatus {
			found = true
		}
	}
	if !found {
		t.Fatalf("items = %+v", set.Items)
	}
}

func TestSummarize(t *testing.T) {
	prev := baseSnapshot()
	cur := clone(prev)
	cur.Candidates[0].Votes = domain.Known(650)
	if got := Summarize(Compare(prev, cur)); got == "" || got == "sem alterações relevantes" {
		t.Fatalf("summary = %q", got)
	}
	if Summarize(domain.ChangeSet{}) != "sem alterações relevantes" {
		t.Fatal("empty summary")
	}
}

func TestAbstentionDecreaseIsNotRevision(t *testing.T) {
	prev := baseSnapshot()
	cur := clone(prev)
	cur.Totals.Turnout = domain.Known(1100)
	cur.Totals.Abstention = domain.Known(100)
	set := Compare(prev, cur)
	if !set.Changed {
		t.Fatal("expected totals change")
	}
	if set.Revision {
		t.Fatalf("derived abstention decrease wrongly marked as revision: %+v", set.Items)
	}
}
