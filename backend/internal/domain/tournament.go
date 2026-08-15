package domain

import "time"

const (
	KickertoolAPISource  = "kickertool_api"
	KickertoolHTMLSource = "kickertool_html"
)

// Tournament is the source-independent representation persisted by the crawler.
// SourceID is optional; SourceKey is always populated and is the deterministic
// identity fallback when the source has no stable identifier.
type Tournament struct {
	ID                                    uint
	Source                                string
	SourceID                              string
	SourceKey                             string
	Name                                  string
	Date                                  *time.Time
	StartTime                             *time.Time
	EndTime                               *time.Time
	Venue                                 string
	City                                  string
	Country                               string
	Organizer                             string
	Status                                string
	EntryType                             string
	IsLive                                bool
	PreviousStatus                        string
	PreviousIsLive                        bool
	StatusTransition                      string
	StatusTransitionAt                    *time.Time
	Participants                          *int
	URL                                   string
	LastSeenAt                            time.Time
	CreatedAt                             time.Time
	UpdatedAt                             time.Time
	StandingsSyncedAt                     *time.Time
	StandingsHash                         string
	StandingsSyncComplete                 bool
	LastStandingsSyncFailed               bool
	FinalizedAt                           *time.Time
	ConsecutiveIdenticalCompleteSnapshots int
	IncludedInRanking                     bool
	InclusionUpdatedAt                    *time.Time
	InclusionVersion                      int64
	InclusionReason                       string
}

func (t Tournament) Identity() string {
	if t.SourceID != "" {
		return t.Source + ":" + t.SourceID
	}
	return t.Source + ":" + t.SourceKey
}
