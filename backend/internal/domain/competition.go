package domain

type Discipline struct {
	SourceID     string
	TournamentID string
	Name         string
	ShortName    string
	EntryType    string
}

type Stage struct {
	SourceID     string
	DisciplineID string
	TournamentID string
	State        string
}

type StandingGroup struct {
	SourceID     string
	StageID      string
	DisciplineID string
	TournamentID string
	Name         string
	State        string
}

type Entry struct {
	SourceID     string
	TournamentID string
	Name         string
	EntryType    string
}

type EntryMembership struct {
	EntryID      string
	PlayerID     string
	PlayerName   string
	TournamentID string
}

type StandingAllocation struct {
	StandingID  string
	EntryID     string
	PlayerID    string
	PointsCents int64
}

type GroupStanding struct {
	SourceID                     string
	TournamentID                 string
	DisciplineID                 string
	StageID                      string
	GroupID                      string
	EntryID                      string
	EntryName                    string
	PlayerID                     string
	PlayerName                   string
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
	URL                          string
}
