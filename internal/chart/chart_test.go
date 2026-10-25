package chart

import (
	"bytes"
	"errors"
	"image/png"
	"testing"
	"time"

	"github.com/gpu/urna-watch/internal/domain"
)

func sample() *domain.Snapshot {
	s := &domain.Snapshot{
		Election: domain.ElectionInfo{
			Cycle: "ele2026", ElectionCode: "6258", Round: 2,
			ContestName: "Presidente", ScopeName: "Brasil",
		},
		TotalizedAt:         time.Date(2026, 10, 25, 19, 10, 32, 0, time.UTC),
		IsFinal:             false,
		SectionsCounted:     domain.Known(8218),
		SectionsTotal:       domain.Known(499248),
		SectionsPercentNano: 1646078080,
		Candidates: []domain.Candidate{
			{Number: "13", Name: "LUIZ INÁCIO LULA DA SILVA", BallotName: "LULA", PartyAcronym: "PT", Votes: domain.Known(1068370), PercentNano: 52130000000, PercentText: "52,13"},
			{Number: "22", Name: "FLAVIO NANTES BOLSONARO", BallotName: "FLAVIO BOLSONARO", PartyAcronym: "PL", Votes: domain.Known(981000), PercentNano: 47870000000, PercentText: "47,87"},
		},
	}
	s.SortCandidates()
	return s
}

func TestRenderPNG(t *testing.T) {
	data, err := Render(sample())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatal("not a PNG")
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != canvasWidth || img.Bounds().Dy() < 400 {
		t.Fatalf("bad dimensions: %v", img.Bounds())
	}
}

func TestRenderNoData(t *testing.T) {
	if _, err := Render(nil); !errors.Is(err, ErrNoData) {
		t.Fatalf("nil: %v", err)
	}
	s := sample()
	for i := range s.Candidates {
		s.Candidates[i].Votes = domain.Known(0)
		s.Candidates[i].PercentNano = 0
	}
	if _, err := Render(s); !errors.Is(err, ErrNoData) {
		t.Fatalf("zero: %v", err)
	}
}

func TestRenderMapPNG(t *testing.T) {
	main := sample()
	states := map[string]*domain.Snapshot{
		"sp": {Candidates: []domain.Candidate{
			{Number: "13", Votes: domain.Known(1000), PercentNano: 60000000000},
			{Number: "22", Votes: domain.Known(500), PercentNano: 40000000000},
		}},
		"rj": {Candidates: []domain.Candidate{
			{Number: "22", Votes: domain.Known(800), PercentNano: 55000000000},
			{Number: "13", Votes: domain.Known(700), PercentNano: 45000000000},
		}},
	}
	data, err := RenderMap(main, states)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatal("not a PNG")
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != dashboardWidth || img.Bounds().Dy() != dashboardHeight {
		t.Fatalf("bad dimensions: %v", img.Bounds())
	}
}

func TestRenderMapNoWinners(t *testing.T) {
	if _, err := RenderMap(sample(), map[string]*domain.Snapshot{}); !errors.Is(err, ErrNoData) {
		t.Fatalf("empty: %v", err)
	}
	zero := &domain.Snapshot{Candidates: []domain.Candidate{{Number: "13", Votes: domain.Known(0)}}}
	if _, err := RenderMap(sample(), map[string]*domain.Snapshot{"sp": zero}); !errors.Is(err, ErrNoData) {
		t.Fatalf("zero: %v", err)
	}
}

func TestBrazilMapHasAllStates(t *testing.T) {
	shapes, err := loadBrazilMap()
	if err != nil {
		t.Fatal(err)
	}
	if len(shapes) != 27 {
		t.Fatalf("states = %d", len(shapes))
	}
}
