package observability

import (
	"time"

	"github.com/gpu/urna-watch/internal/domain"
	"github.com/gpu/urna-watch/internal/notification"
	"github.com/gpu/urna-watch/internal/polling"
)

// SourceStatus is the last observed state of the official source request.
type SourceStatus struct {
	URL          string    `json:"url"`
	StatusCode   int       `json:"statusCode,omitempty"`
	ETag         string    `json:"etag,omitempty"`
	LastModified string    `json:"lastModified,omitempty"`
	NotModified  bool      `json:"notModified"`
	LastAttempt  time.Time `json:"lastAttempt,omitempty"`
	LastSuccess  time.Time `json:"lastSuccess,omitempty"`
	LastError    string    `json:"lastError,omitempty"`
	Latency      string    `json:"latency,omitempty"`
}

// UFView is an on-demand, read-only breakdown of the main federal election by
// state. It never affects detection, persistence or notifications.
type UFView struct {
	UF        string           `json:"uf"`
	Name      string           `json:"name"`
	Loading   bool             `json:"loading"`
	Snapshot  *domain.Snapshot `json:"snapshot,omitempty"`
	Error     string           `json:"error,omitempty"`
	FetchedAt time.Time        `json:"fetchedAt,omitempty"`
}

// MapState is the leading candidate in one state, derived only from the
// official per-UF result file.
type MapState struct {
	UF           string `json:"uf"`
	WinnerNumber string `json:"winnerNumber,omitempty"`
	WinnerName   string `json:"winnerName,omitempty"`
	WinnerParty  string `json:"winnerParty,omitempty"`
	Votes        int64  `json:"votes,omitempty"`
	PercentNano  int64  `json:"percentNano,omitempty"`
	PercentText  string `json:"percentText,omitempty"`
	Counted      bool   `json:"counted"`
}

// MapView is the on-demand state map used by the TUI. It is presentation-only
// and never affects the monitored Brazil snapshot or notifications.
type MapView struct {
	Loading   bool                `json:"loading"`
	Error     string              `json:"error,omitempty"`
	States    map[string]MapState `json:"states,omitempty"`
	FetchedAt time.Time           `json:"fetchedAt,omitempty"`
}

// Status is the immutable view consumed by presentation layers. The collection
// loop builds it while holding its own lock; readers never touch live state.
type Status struct {
	Version          string                `json:"version"`
	StartedAt        time.Time             `json:"startedAt"`
	Ref              domain.ElectionRef    `json:"ref"`
	Snapshot         *domain.Snapshot      `json:"snapshot,omitempty"`
	PreviousSnapshot *domain.Snapshot      `json:"previousSnapshot,omitempty"`
	UFView           *UFView               `json:"ufView,omitempty"`
	Map              *MapView              `json:"map,omitempty"`
	History          []domain.HistoryPoint `json:"history,omitempty"`
	Events           []domain.EventRecord  `json:"events"`
	Poll             polling.Metrics       `json:"poll"`
	Telegram         notification.Stats    `json:"telegram"`
	Source           SourceStatus          `json:"source"`
	Paused           bool                  `json:"paused"`
	StatePath        string                `json:"statePath"`
	ConfigPath       string                `json:"configPath,omitempty"`
	BaseURL          string                `json:"baseUrl"`
	Environment      string                `json:"environment"`
	Timezone         string                `json:"timezone"`
	Error            string                `json:"error,omitempty"`
	ShuttingDown     bool                  `json:"shuttingDown"`
}
