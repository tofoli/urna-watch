package tse

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tofoli/urna-watch/internal/domain"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "tests", "fixtures", "tse", name))
	if err != nil {
		t.Fatalf("ler fixture %s: %v", name, err)
	}
	return data
}

func election6258() domain.ElectionInfo {
	return domain.ElectionInfo{
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
}

func TestParseResultRunoff2026(t *testing.T) {
	now := time.Date(2026, 10, 25, 18, 0, 0, 0, time.UTC)
	snap, err := ParseResult(fixture(t, "br-c0001-e006258-u.json"), election6258(), "https://example.test/u.json", now)
	if err != nil {
		t.Fatalf("ParseResult: %v", err)
	}
	if snap.Election.ElectionName != election6258().ElectionName {
		t.Fatalf("election name = %q", snap.Election.ElectionName)
	}
	if snap.SourceVersion != "3948414" {
		t.Fatalf("source version = %q", snap.SourceVersion)
	}
	if snap.IsFinal {
		t.Fatal("fixture is partial, IsFinal must be false")
	}
	if !snap.VotesPublished {
		t.Fatal("votes should be published")
	}
	if !snap.SectionsTotal.Known || snap.SectionsTotal.Value != 499248 {
		t.Fatalf("sections total = %+v", snap.SectionsTotal)
	}
	if snap.SectionsCounted.Value != 1225 {
		t.Fatalf("sections counted = %+v", snap.SectionsCounted)
	}
	if snap.SectionsPercentNano != 245369035 {
		t.Fatalf("sections percent = %d", snap.SectionsPercentNano)
	}
	if !snap.Totals.TotalVotes.Known || snap.Totals.TotalVotes.Value != 345054 {
		t.Fatalf("total votes = %+v", snap.Totals.TotalVotes)
	}
	if snap.Totals.ValidVotes.Value != 335743 || snap.Totals.BlankVotes.Value != 4158 || snap.Totals.NullVotes.Value != 5153 {
		t.Fatalf("totals = %+v", snap.Totals)
	}
	if snap.Totals.Electorate.Value != 158745502 || snap.Totals.Turnout.Value != 345054 || snap.Totals.Abstention.Value != 337841 {
		t.Fatalf("electorate totals = %+v", snap.Totals)
	}
	if len(snap.Candidates) != 2 {
		t.Fatalf("candidates = %d", len(snap.Candidates))
	}
	first, second := snap.Candidates[0], snap.Candidates[1]
	if first.Number != "13" || first.Name != "LUIZ INÁCIO LULA DA SILVA" || first.Votes.Value != 193574 {
		t.Fatalf("first candidate = %+v", first)
	}
	if first.PartyAcronym != "PT" || first.PercentNano != 57655409048 || first.PercentText != "57,66" {
		t.Fatalf("first candidate metadata = %+v", first)
	}
	if second.Number != "22" || second.Votes.Value != 142169 || second.PartyAcronym != "PL" {
		t.Fatalf("second candidate = %+v", second)
	}
	if first.Position != 1 || second.Position != 2 {
		t.Fatalf("positions = %d,%d", first.Position, second.Position)
	}
	if first.Elected == nil || *first.Elected {
		t.Fatalf("partial runoff candidate must not be marked elected: %+v", first.Elected)
	}
	if snap.TotalizedAt.Format("02/01/2006 15:04:05") != "25/10/2026 17:06:27" {
		t.Fatalf("totalized at = %s", snap.TotalizedAt)
	}
	if _, offset := snap.TotalizedAt.Zone(); offset != -3*60*60 {
		t.Fatalf("expected America/Sao_Paulo offset, got %d", offset)
	}
	if snap.ID == "" || len(snap.ID) != 32 {
		t.Fatalf("id = %q", snap.ID)
	}
}

func TestParseResultFinalFirstRound(t *testing.T) {
	info := election6258()
	info.ElectionCode = "6257"
	info.Round = 1
	info.ElectionName = "Eleição Ordinária Federal - 2026 1º Turno"
	snap, err := ParseResult(fixture(t, "br-c0001-e006257-u.json"), info, "https://example.test/u.json", time.Now())
	if err != nil {
		t.Fatalf("ParseResult: %v", err)
	}
	if !snap.IsFinal {
		t.Fatal("fixture is final")
	}
	if len(snap.Candidates) < 5 {
		t.Fatalf("candidates = %d", len(snap.Candidates))
	}
	// The two first-round qualifiers carry source status "2º turno" and e=s.
	// They must not be reported as elected.
	for _, c := range snap.Candidates[:2] {
		if c.Elected != nil && *c.Elected {
			t.Fatalf("candidate %s wrongly marked elected", c.Number)
		}
	}
	if snap.Candidates[0].Number != "22" || snap.Candidates[0].Votes.Value != 56104503 {
		t.Fatalf("first candidate = %+v", snap.Candidates[0])
	}
	if snap.Candidates[1].Number != "13" || snap.Candidates[1].Votes.Value != 53879538 {
		t.Fatalf("second candidate = %+v", snap.Candidates[1])
	}
}

func TestParseResultRejectsInvalidBodies(t *testing.T) {
	cases := map[string][]byte{
		"not json":       []byte("{"),
		"empty object":   []byte(`{}`),
		"no candidates":  []byte(`{"idg":"1","carg":[{"cd":"1","agr":[]}]}`),
		"bad vote":       mutateJSON(t, fixture(t, "br-c0001-e006258-u.json"), func(m map[string]any) { firstCandidate(m)["vap"] = "oops" }),
		"bad percent":    mutateJSON(t, fixture(t, "br-c0001-e006258-u.json"), func(m map[string]any) { firstCandidate(m)["pvapn"] = "x" }),
		"final pending":  mutateJSON(t, fixture(t, "br-c0001-e006258-u.json"), func(m map[string]any) { m["tf"] = "s" }),
		"negative votes": mutateJSON(t, fixture(t, "br-c0001-e006258-u.json"), func(m map[string]any) { firstCandidate(m)["vap"] = "-1" }),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseResult(body, election6258(), "https://example.test/u.json", time.Now()); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestParseResultRejectsMissingIDG(t *testing.T) {
	body := mutateJSON(t, fixture(t, "br-c0001-e006258-u.json"), func(m map[string]any) { delete(m, "idg") })
	if _, err := ParseResult(body, election6258(), "", time.Now()); err == nil || !strings.Contains(err.Error(), "idg") {
		t.Fatalf("expected idg error, got %v", err)
	}
}

func TestParseNanoPercentExact(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"57,655409048", 57655409048},
		{"57.655409048", 57655409048},
		{"0,245369035", 245369035},
		{"100", 100_000_000_000},
		{"0", 0},
	}
	for _, tc := range cases {
		got, ok, err := parseNanoPercent(tc.in)
		if err != nil || !ok {
			t.Fatalf("%q: ok=%v err=%v", tc.in, ok, err)
		}
		if got != tc.want {
			t.Fatalf("%q = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func firstCandidate(m map[string]any) map[string]any {
	cargos := m["carg"].([]any)
	cargo := cargos[0].(map[string]any)
	agr := cargo["agr"].([]any)
	party := agr[0].(map[string]any)["par"].([]any)[0].(map[string]any)
	return party["cand"].([]any)[0].(map[string]any)
}

func mutateJSON(t *testing.T, body []byte, fn func(map[string]any)) []byte {
	t.Helper()
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		t.Fatal(err)
	}
	fn(root)
	out, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestParseMunicipalZeroVotes(t *testing.T) {
	info := domain.ElectionInfo{
		Cycle: "ele2026", ElectionCode: "6284", ElectionName: "Eleição Suplementar - Narandiba",
		Round: 1, ContestCode: "11", ContestName: "Prefeito", Level: "suplementar",
		Scope: domain.Scope{Kind: domain.ScopeMunicipality, UF: "sp", Municipality: "67458"}, ScopeName: "NARANDIBA (SP)",
	}
	snap, err := ParseResult(fixture(t, "sp67458-c0011-e006284-u.json"), info, "https://example.test/m.json", time.Now())
	if err != nil {
		t.Fatalf("ParseResult: %v", err)
	}
	if snap.SectionsCounted.Value != 0 || snap.SectionsTotal.Value != 13 {
		t.Fatalf("sections = %+v", snap)
	}
	if snap.Totals.TotalVotes.Value != 0 {
		t.Fatalf("total votes = %+v", snap.Totals.TotalVotes)
	}
	if len(snap.Candidates) == 0 {
		t.Fatal("candidates")
	}
	for _, c := range snap.Candidates {
		if c.Votes.Value != 0 {
			t.Fatalf("candidate %s = %+v", c.Number, c.Votes)
		}
	}
	if snap.IsFinal {
		t.Fatal("must not be final")
	}
	// dt/ht are empty in this source file; generation time is used.
	if snap.TotalizedAt.IsZero() {
		t.Fatal("totalized fallback missing")
	}
}

func TestParseSanitizesTerminalControls(t *testing.T) {
	body := mutateJSON(t, fixture(t, "br-c0001-e006258-u.json"), func(m map[string]any) {
		c := firstCandidate(m)
		c["nm"] = "LULA\u001b[31m"
		c["nmu"] = "LULA\u202eRTL"
		c["st"] = "Eleito\u0007"
	})
	snap, err := ParseResult(body, election6258(), "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range snap.Candidates {
		if strings.ContainsRune(c.Name, '\x1b') || strings.ContainsRune(c.BallotName, '\u202e') || strings.ContainsRune(c.Status, '\x07') {
			t.Fatalf("control characters survived: %+v", c)
		}
	}
}
