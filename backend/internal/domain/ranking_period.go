package domain

// RankingMonth identifies a calendar month in RankingLocation (Europe/Berlin).
type RankingMonth struct {
	Year  int `json:"year"`
	Month int `json:"month"`
}
