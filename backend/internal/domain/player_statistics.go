package domain

import "time"

type PlayerListFilter struct {
	Query, State string
	Page, Limit  int
}

type PlayerSummary struct {
	ID                 uint   `json:"id"`
	DisplayName        string `json:"displayName"`
	Active             bool   `json:"active"`
	MergedIntoPlayerID *uint  `json:"mergedIntoPlayerId"`
}

type PlayerPage struct {
	Items []PlayerSummary `json:"items"`
	Page  int             `json:"page"`
	Limit int             `json:"limit"`
	Total int64           `json:"total"`
}

// Metrics are the source values. Only Reason=counted contributes to the total;
// explicit manual corrections are listed independently.
type PlayerTournamentContribution struct {
	ID                 uint       `json:"id"`
	TournamentID       uint       `json:"tournamentId"`
	Name               string     `json:"name"`
	Date               *time.Time `json:"date"`
	Source             string     `json:"source"`
	SourceID           string     `json:"sourceId"`
	StandingRank       *int       `json:"standingRank"`
	StandingSourceID   *string    `json:"standingSourceId"`
	StandingKey        string     `json:"standingKey"`
	SourcePlayerName   string     `json:"sourcePlayerName"`
	URL                string     `json:"url"`
	Status             string     `json:"status"`
	Reason             string     `json:"reason"`
	GamesPlayed        *int       `json:"gamesPlayed"`
	TotalPointsCents   *int64     `json:"totalPointsCents"`
	PointsPerGameCents *int64     `json:"pointsPerGameCents"`
	GoalDifference     *int       `json:"goalDifference"`
}

type PlayerStatistics struct {
	RequestedPlayer PlayerSummary
	Player          PlayerProfile
	Tournaments     []PlayerTournamentContribution
	Corrections     []ManualRankingCorrection
	ComputedAt      time.Time
}
