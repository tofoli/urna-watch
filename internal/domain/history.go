package domain

import "time"

// HistoryCandidate is the compact per-candidate slice stored in the local
// history. It keeps only the fields needed for the TUI dispute chart.
type HistoryCandidate struct {
	Number      string `json:"number"`
	Name        string `json:"name"`
	Party       string `json:"party,omitempty"`
	Votes       int64  `json:"votes"`
	PercentNano int64  `json:"percentNano"`
	Position    int    `json:"position"`
}

// HistoryPoint is one detected snapshot used by the local trend chart. The
// history is bounded by the store; it is not a full snapshot archive.
type HistoryPoint struct {
	DetectedAt          time.Time          `json:"detectedAt"`
	SourceAt            time.Time          `json:"sourceAt,omitempty"`
	SourceVersion       string             `json:"sourceVersion"`
	Fingerprint         string             `json:"fingerprint"`
	SectionsCounted     int64              `json:"sectionsCounted"`
	SectionsPercentNano int64              `json:"sectionsPercentNano"`
	Candidates          []HistoryCandidate `json:"candidates"`
}

// HistoryPoint builds a compact point for the local trend chart. Up to three
// leading candidates are kept; the TUI compares the first two.
func (s *Snapshot) HistoryPoint() HistoryPoint {
	p := HistoryPoint{
		DetectedAt:          s.CapturedAt,
		SourceAt:            s.TotalizedAt,
		SourceVersion:       s.SourceVersion,
		Fingerprint:         s.Fingerprint(),
		SectionsCounted:     s.SectionsCounted.OrZero(),
		SectionsPercentNano: s.SectionsPercentNano,
	}
	limit := len(s.Candidates)
	if limit > 3 {
		limit = 3
	}
	for _, c := range s.Candidates[:limit] {
		p.Candidates = append(p.Candidates, HistoryCandidate{
			Number:      c.Number,
			Name:        c.DisplayName(),
			Party:       c.PartyAcronym,
			Votes:       c.Votes.OrZero(),
			PercentNano: c.PercentNano,
			Position:    c.Position,
		})
	}
	return p
}

// HistoryAt returns the timestamp used to order/display the trend chart.
// Detection time is preferred because source totalization can stay constant
// across several updates.
func (p HistoryPoint) HistoryAt() time.Time {
	if !p.DetectedAt.IsZero() {
		return p.DetectedAt
	}
	return p.SourceAt
}
