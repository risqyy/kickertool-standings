package domain

import "time"

// TournamentRefreshJob describes an in-process, administrator-triggered result
// refresh. A successful job is not a successful crawl of the entire source.
type TournamentRefreshJob struct {
	ID           string     `json:"id"`
	TournamentID uint       `json:"tournamentId"`
	State        string     `json:"state"`
	StartedAt    time.Time  `json:"startedAt"`
	FinishedAt   *time.Time `json:"finishedAt"`
}
