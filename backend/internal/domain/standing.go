package domain

import "time"

type TournamentStanding struct {
	Source                       string
	TournamentID                 string
	EntryID                      string
	EntryName                    string
	StandingID                   string
	StandingKey                  string
	DisciplineID                 string
	StageID                      string
	Group                        string
	PlayerID                     string
	PlayerKey                    string
	PlayerName                   string
	Team                         string
	Partner                      string
	Rank                         *int
	Result                       *int
	Preliminary                  *int
	FinalResult                  *int
	PointsCents                  *int64
	PointsPerMatchCents          *int64
	CorrectedPointsPerMatchCents *int64
	HasCorrectedValue            *bool
	GamesPlayed                  *int
	GoalDifference               *int
	Status                       string
	Stats                        map[string]float64
	URL                          string
	LastSeenAt                   time.Time
}

type StandingSnapshot struct {
	Source         string
	TournamentID   string
	URL            string
	Standings      []TournamentStanding
	Complete       bool
	PointsExplicit bool
	FetchedAt      time.Time
	Disciplines    []Discipline
	Stages         []Stage
	Groups         []StandingGroup
	Entries        []Entry
	Memberships    []EntryMembership
	Allocations    []StandingAllocation
	GroupStandings []GroupStanding
}

func (s StandingSnapshot) IsComplete() bool { return s.Complete }

type StandingSyncResult struct {
	Found                  int
	PlayersInserted        int
	PlayersUpdated         int
	StandingsInserted      int
	StandingsUpdated       int
	StandingsUnchanged     int
	AggregatesRecalculated int
}

type PlayerAggregate struct {
	Source             string
	PlayerKey          string
	PlayerName         string
	TournamentCount    int
	TotalPointsCents   *int64
	GamesPlayed        *int
	GoalDifference     *int
	PointsPerGameCents *int64
	PointsAvailable    bool
	GamesAvailable     bool
	GoalsAvailable     bool
	Trend              RankingTrend
	RecalculatedAt     time.Time
}

// RankingTrend describes the change in canonical rank against the snapshot
// immediately before the most recent qualifying tournament in the scope.
// It is computed by the ranking read model and is intentionally not persisted
// with source standings or materialized aggregates.
type RankingTrend string

const (
	RankingTrendUp   RankingTrend = "up"
	RankingTrendDown RankingTrend = "down"
	RankingTrendSame RankingTrend = "same"
	RankingTrendNew  RankingTrend = "new"
)
