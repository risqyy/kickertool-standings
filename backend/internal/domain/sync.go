package domain

import "time"

type SyncResult struct {
	Found                  int
	Inserted               int
	Updated                int
	Unchanged              int
	Invalid                int
	TournamentsProcessed   int
	TournamentsSucceeded   int
	TournamentsFailed      int
	TournamentsSkipped     int
	StandingsFound         int
	PlayersInserted        int
	PlayersUpdated         int
	StandingsInserted      int
	StandingsUpdated       int
	StandingsUnchanged     int
	AggregatesRecalculated int
	StartedAt              time.Time
	FinishedAt             time.Time
}

type TournamentListFilter struct {
	Query    string
	Included *bool
	State    string
	Source   string
	DateFrom *time.Time
	DateTo   *time.Time
	Page     int
	Limit    int
	Sort     string
	Desc     bool
}

type TournamentAdminRow struct {
	Tournament
	StandingCount     int
	PlayerCount       int
	StandingsComplete bool
	LastSyncError     bool
	InclusionVersion  int64
}

type TournamentPage struct {
	Items      []TournamentAdminRow
	Page       int
	Limit      int
	Total      int64
	LastSyncAt *time.Time
}

type TournamentInclusionChange struct {
	Tournament TournamentAdminRow
	Changed    bool
	AuditID    uint
}

type Dashboard struct {
	TournamentCount         int64
	IncludedTournamentCount int64
	ExcludedTournamentCount int64
	PlayerCount             int64
	LastSyncAt              *time.Time
}

func (r SyncResult) Duration() time.Duration {
	return r.FinishedAt.Sub(r.StartedAt)
}
