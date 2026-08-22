package domain

import "time"

// ManualRankingCorrection is an additive, auditable adjustment to a player's
// ranking.  It deliberately lives beside (and never in) source standings.
// EffectiveDate controls the calendar year in period rankings.
type ManualRankingCorrection struct {
	ID                     uint
	PlayerID               uint
	PlayerKey              string
	PlayerName             string
	EffectiveDate          time.Time
	EffectiveYear          int
	TournamentCountDelta   int
	GamesPlayedDelta       int
	PointsCentsDelta       int64
	GoalDifferenceDelta    int
	Reason                 string
	Administrator          string
	CreatedAt              time.Time
	Status                 string
	RevokedAt              *time.Time
	RevokedBy              string
	RevocationReason       string
	Revision               int64
	Version                int64
	SupersedesCorrectionID *uint
	ReplacedByCorrectionID *uint
}

type ManualRankingCorrectionInput struct {
	PlayerID      uint
	EffectiveDate time.Time
	// EffectiveYear is the explicit calendar-year scope of the correction.
	// HTTP callers must provide it; repository callers from older integrations
	// may leave it zero and are normalized from EffectiveDate for compatibility.
	EffectiveYear        int
	TournamentCountDelta int
	GamesPlayedDelta     int
	PointsCentsDelta     int64
	GoalDifferenceDelta  int
	Reason               string
	Administrator        string
	ReplaceCorrectionID  uint
}

type ManualRankingCorrectionPreview struct {
	Player          PlayerProfile
	Correction      ManualRankingCorrection
	Before          PlayerAggregate
	After           PlayerAggregate
	ExpectedVersion int64
	Superseded      *ManualRankingCorrection
}

type ManualRankingCorrectionChange struct {
	Correction ManualRankingCorrection
	Before     PlayerAggregate
	After      PlayerAggregate
	Version    int64
	Superseded *ManualRankingCorrection
}

type ManualRankingCorrectionRevocation struct {
	Correction ManualRankingCorrection
	Before     PlayerAggregate
	After      PlayerAggregate
	Version    int64
}
