// Package domain contains the stable internal model used by every component.
// It deliberately has no dependency on the TSE wire format, on the terminal or
// on the Telegram API.
package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"time"
)

// Measure represents an optional numeric value. It distinguishes an absent
// field (Known=false) from a real zero (Known=true, Value=0).
type Measure struct {
	Value int64 `json:"value"`
	Known bool  `json:"known"`
}

func Known(v int64) Measure { return Measure{Value: v, Known: true} }

func (m Measure) OrZero() int64 {
	if !m.Known {
		return 0
	}
	return m.Value
}

// ScopeKind identifies the geographic granularity of a result file.
type ScopeKind string

const (
	ScopeBrazil       ScopeKind = "br"
	ScopeState        ScopeKind = "uf"
	ScopeMunicipality ScopeKind = "municipality"
)

// Scope is the geographic scope requested from the source.
type Scope struct {
	Kind         ScopeKind `json:"kind"`
	UF           string    `json:"uf,omitempty"`
	Municipality string    `json:"municipality,omitempty"`
}

// Key returns a stable key for maps and cache entries.
func (s Scope) Key() string {
	parts := []string{string(s.Kind)}
	if s.UF != "" {
		parts = append(parts, strings.ToLower(s.UF))
	}
	if s.Municipality != "" {
		parts = append(parts, s.Municipality)
	}
	return strings.Join(parts, ":")
}

// ElectionRef is the user-selected slice of an election.
type ElectionRef struct {
	Cycle        string `json:"cycle"`
	ElectionCode string `json:"electionCode"`
	Round        int    `json:"round,omitempty"`
	ContestCode  string `json:"contestCode"`
	Scope        Scope  `json:"scope"`
	ContestName  string `json:"contestName,omitempty"`
	ElectionName string `json:"electionName,omitempty"`
	Level        string `json:"level,omitempty"`
}

// Key identifies the monitored result independently of its data version.
func (r ElectionRef) Key() string {
	cycle := r.Cycle
	if cycle == "" {
		cycle = "-"
	}
	return strings.Join([]string{
		cycle, r.ElectionCode, r.ContestCode, r.Scope.Key(),
	}, "|")
}

// ElectionInfo is official metadata resolved from the TSE election config.
type ElectionInfo struct {
	Cycle        string `json:"cycle"`
	ElectionCode string `json:"electionCode"`
	ElectionName string `json:"electionName"`
	Round        int    `json:"round"`
	ContestCode  string `json:"contestCode"`
	ContestName  string `json:"contestName"`
	Level        string `json:"level,omitempty"`
	Scope        Scope  `json:"scope"`
	ScopeName    string `json:"scopeName"`
	SourcePage   string `json:"sourcePage"`
}

// Candidate is one result row as published by the source.
type Candidate struct {
	Number       string  `json:"number"`
	Name         string  `json:"name"`
	BallotName   string  `json:"ballotName,omitempty"`
	Party        string  `json:"party,omitempty"`
	PartyAcronym string  `json:"partyAcronym,omitempty"`
	Votes        Measure `json:"votes"`
	PercentNano  int64   `json:"percentNano,omitempty"`
	PercentText  string  `json:"percentText,omitempty"`
	Status       string  `json:"status,omitempty"`
	// Elected is true only when the source explicitly reports an elected
	// status. A candidate qualified for a runoff is not marked elected.
	Elected  *bool `json:"elected,omitempty"`
	Position int   `json:"position"`
}

func (c Candidate) DisplayName() string {
	if strings.TrimSpace(c.BallotName) != "" {
		return c.BallotName
	}
	return c.Name
}

// Totals contains vote and turnout aggregates. Every field is optional because
// availability differs between election types and source files.
type Totals struct {
	TotalVotes    Measure `json:"totalVotes"`
	ValidVotes    Measure `json:"validVotes"`
	NominalVotes  Measure `json:"nominalVotes"`
	BlankVotes    Measure `json:"blankVotes"`
	NullVotes     Measure `json:"nullVotes"`
	Electorate    Measure `json:"electorate"`
	Turnout       Measure `json:"turnout"`
	Abstention    Measure `json:"abstention"`
	CountedVoters Measure `json:"countedVoters"`
}

// Snapshot is a normalized, validated source result. It contains only fields
// that are meaningful for change detection and presentation.
type Snapshot struct {
	ID             string       `json:"id"`
	Election       ElectionInfo `json:"election"`
	CapturedAt     time.Time    `json:"capturedAt"`
	SourceVersion  string       `json:"sourceVersion"`
	GeneratedAt    time.Time    `json:"generatedAt"`
	TotalizedAt    time.Time    `json:"totalizedAt"`
	IsFinal        bool         `json:"isFinal"`
	VotesPublished bool         `json:"votesPublished"`
	StateText      string       `json:"stateText"`

	SectionsTotal       Measure `json:"sectionsTotal"`
	SectionsCounted     Measure `json:"sectionsCounted"`
	SectionsPercentNano int64   `json:"sectionsPercentNano"`
	SectionsPercentText string  `json:"sectionsPercentText,omitempty"`

	Totals     Totals      `json:"totals"`
	Candidates []Candidate `json:"candidates"`

	SourceURL string `json:"sourceUrl"`
}

// SortCandidates orders candidates by published votes (descending), then by
// candidate number, and assigns positions. JSON object order is never used.
func (s *Snapshot) SortCandidates() {
	sort.SliceStable(s.Candidates, func(i, j int) bool {
		vi, vj := s.Candidates[i].Votes.OrZero(), s.Candidates[j].Votes.OrZero()
		if vi != vj {
			return vi > vj
		}
		return s.Candidates[i].Number < s.Candidates[j].Number
	})
	for i := range s.Candidates {
		s.Candidates[i].Position = i + 1
	}
}

// Fingerprint hashes only the fields that represent published results. It is
// stable across JSON key order, whitespace, transient HTTP metadata and local
// capture time.
func (s *Snapshot) Fingerprint() string {
	type candidate struct {
		N string `json:"n"`
		V int64  `json:"v"`
		P int64  `json:"p"`
		E *bool  `json:"e,omitempty"`
		S string `json:"s,omitempty"`
		X int    `json:"x"`
	}
	payload := struct {
		Election  string      `json:"election"`
		Contest   string      `json:"contest"`
		Scope     string      `json:"scope"`
		Version   string      `json:"version"`
		Final     bool        `json:"final"`
		Published bool        `json:"published"`
		Sections  int64       `json:"sections"`
		Counted   int64       `json:"counted"`
		Pct       int64       `json:"pct"`
		Total     int64       `json:"total"`
		Valid     int64       `json:"valid"`
		Nominal   int64       `json:"nominal"`
		Blank     int64       `json:"blank"`
		Null      int64       `json:"null"`
		Cands     []candidate `json:"cands"`
	}{
		Election:  s.Election.ElectionCode,
		Contest:   s.Election.ContestCode,
		Scope:     s.Election.Scope.Key(),
		Version:   s.SourceVersion,
		Final:     s.IsFinal,
		Published: s.VotesPublished,
		Sections:  s.SectionsTotal.OrZero(),
		Counted:   s.SectionsCounted.OrZero(),
		Pct:       s.SectionsPercentNano,
		Total:     s.Totals.TotalVotes.OrZero(),
		Valid:     s.Totals.ValidVotes.OrZero(),
		Nominal:   s.Totals.NominalVotes.OrZero(),
		Blank:     s.Totals.BlankVotes.OrZero(),
		Null:      s.Totals.NullVotes.OrZero(),
	}
	for _, c := range s.Candidates {
		var elected *bool
		if c.Elected != nil {
			v := *c.Elected
			elected = &v
		}
		payload.Cands = append(payload.Cands, candidate{
			N: c.Number,
			V: c.Votes.OrZero(),
			P: c.PercentNano,
			E: elected,
			S: c.Status,
			X: c.Position,
		})
	}
	sort.Slice(payload.Cands, func(i, j int) bool { return payload.Cands[i].N < payload.Cands[j].N })
	b, _ := json.Marshal(payload)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// RoundLabel returns a human label for the round.
func (r ElectionInfo) RoundLabel() string {
	switch r.Round {
	case 1:
		return "1º turno"
	case 2:
		return "2º turno"
	case 0:
		return "turno não informado"
	default:
		return "turno " + itoa(r.Round)
	}
}

// StateLabel reports only what the source allows us to say.
func (s Snapshot) StateLabel() string {
	if s.IsFinal {
		return "Totalização final informada pela fonte"
	}
	return "Em apuração (totalização não final)"
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
