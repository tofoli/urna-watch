package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ChangeKind classifies a normalized change.
type ChangeKind string

const (
	KindBaseline     ChangeKind = "baseline"
	KindVotes        ChangeKind = "votes"
	KindPercent      ChangeKind = "percent"
	KindPosition     ChangeKind = "position"
	KindStatus       ChangeKind = "status"
	KindCandidateIn  ChangeKind = "candidate_added"
	KindCandidateOut ChangeKind = "candidate_removed"
	KindSections     ChangeKind = "sections"
	KindTotals       ChangeKind = "totals"
	KindPublication  ChangeKind = "publication"
	KindFinal        ChangeKind = "final"
	KindRevision     ChangeKind = "revision"
)

// Change is one normalized difference. Previous/Current are already
// human-readable so that the formatter never has to guess about missing data.
type Change struct {
	Kind             ChangeKind `json:"kind"`
	CandidateNumber  string     `json:"candidateNumber,omitempty"`
	CandidateName    string     `json:"candidateName,omitempty"`
	Previous         string     `json:"previous,omitempty"`
	Current          string     `json:"current,omitempty"`
	Delta            int64      `json:"delta,omitempty"`
	PreviousPosition int        `json:"previousPosition,omitempty"`
	CurrentPosition  int        `json:"currentPosition,omitempty"`
}

// ChangeSet is the result of comparing two snapshots.
type ChangeSet struct {
	Baseline bool      `json:"baseline"`
	Changed  bool      `json:"changed"`
	Revision bool      `json:"revision"`
	Items    []Change  `json:"items"`
	Previous *Snapshot `json:"-"`
	Current  *Snapshot `json:"-"`
	Detected time.Time `json:"detectedAt"`
}

// EventID derives a deterministic identifier from the election, source data
// version and the normalized change fingerprint. Re-reading the same source
// generation therefore never creates a second logical event.
func (c ChangeSet) EventID() string {
	parts := make([]string, 0, len(c.Items))
	for _, item := range c.Items {
		parts = append(parts, strings.Join([]string{
			string(item.Kind), item.CandidateNumber, item.Previous, item.Current,
			fmt.Sprint(item.Delta), fmt.Sprint(item.PreviousPosition), fmt.Sprint(item.CurrentPosition),
		}, "|"))
	}
	sort.Strings(parts)
	payload := strings.Join([]string{
		c.Current.Election.Cycle,
		c.Current.Election.ElectionCode,
		c.Current.Election.ContestCode,
		c.Current.Election.Scope.Key(),
		c.Current.SourceVersion,
		strings.Join(parts, "\n"),
	}, "\x00")
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:16])
}

// NotificationStatus is the delivery state shown in the UI and persisted with
// each event. The source of truth for retries is the notification queue.
type NotificationStatus string

const (
	NotifyPending    NotificationStatus = "pending"
	NotifySent       NotificationStatus = "sent"
	NotifyFailed     NotificationStatus = "failed"
	NotifySuppressed NotificationStatus = "suppressed"
	NotifyDisabled   NotificationStatus = "disabled"
)

// EventRecord is the durable audit entry for one detected change.
type EventRecord struct {
	ID                 string             `json:"id"`
	DetectedAt         time.Time          `json:"detectedAt"`
	Baseline           bool               `json:"baseline"`
	Revision           bool               `json:"revision"`
	Summary            string             `json:"summary"`
	SourceVersion      string             `json:"sourceVersion"`
	ElectionKey        string             `json:"electionKey"`
	NotificationStatus NotificationStatus `json:"notificationStatus"`
	Items              []Change           `json:"items,omitempty"`
}
