package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"time"
)

// HashStandingSnapshot returns a deterministic hash of normalized standing
// data. Fetch timestamps and LastSeenAt are deliberately excluded.
func HashStandingSnapshot(snapshot StandingSnapshot) string {
	rows := make([]TournamentStanding, len(snapshot.Standings))
	copy(rows, snapshot.Standings)
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].StandingKey != rows[j].StandingKey {
			return rows[i].StandingKey < rows[j].StandingKey
		}
		return rows[i].PlayerKey < rows[j].PlayerKey
	})
	for i := range rows {
		rows[i].Source = normalizeHashText(rows[i].Source)
		rows[i].TournamentID = normalizeHashText(rows[i].TournamentID)
		rows[i].StandingID = normalizeHashText(rows[i].StandingID)
		rows[i].StandingKey = normalizeHashText(rows[i].StandingKey)
		rows[i].Group = normalizeHashText(rows[i].Group)
		rows[i].PlayerID = normalizeHashText(rows[i].PlayerID)
		rows[i].PlayerKey = normalizeHashText(rows[i].PlayerKey)
		rows[i].PlayerName = normalizeHashText(rows[i].PlayerName)
		rows[i].Team = normalizeHashText(rows[i].Team)
		rows[i].Partner = normalizeHashText(rows[i].Partner)
		rows[i].Status = normalizeHashText(rows[i].Status)
		rows[i].URL = strings.TrimSpace(rows[i].URL)
		rows[i].LastSeenAt = time.Time{}
	}
	payload := struct {
		Source     string
		Tournament string
		Complete   bool
		Points     bool
		Standings  []TournamentStanding
	}{
		Source: snapshot.Source, Tournament: snapshot.TournamentID, Complete: snapshot.Complete,
		Points: snapshot.PointsExplicit, Standings: rows,
	}
	encoded, _ := json.Marshal(payload)
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func normalizeHashText(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}
