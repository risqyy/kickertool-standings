package domain

import (
	"strings"
	"time"
	"unicode"
)

// RankingLocation is the policy timezone used when a tournament's calendar
// date determines whether it has finished and which ranking year it belongs
// to. Keeping it in the domain avoids adapters applying different date rules.
const RankingLocation = "Europe/Berlin"

// IsTournamentCanceledStatus reports whether a source status represents a
// canceled tournament.
func IsTournamentCanceledStatus(status string) bool {
	status = strings.ToLower(strings.TrimSpace(status))
	for _, marker := range []string{"cancel", "abgesagt", "annulliert", "storniert"} {
		if strings.Contains(status, marker) {
			return true
		}
	}
	return false
}

// IsTournamentLiveStatus reports whether a source status represents a live
// or in-progress tournament.
func IsTournamentLiveStatus(status string) bool {
	for _, marker := range []string{"live", "running", "ongoing", "in progress", "läuft", "laeuft", "started", "active"} {
		if hasTournamentStatusMarker(status, marker) {
			return true
		}
	}
	return false
}

// IsTournamentCompletedStatus reports completion based on the source status.
func IsTournamentCompletedStatus(status string) bool {
	for _, marker := range []string{"finished", "completed", "complete", "done", "closed", "ended", "beendet", "abgeschlossen"} {
		if hasTournamentStatusMarker(status, marker) {
			return true
		}
	}
	return false
}

// hasTournamentStatusMarker matches complete status words (or phrases), not
// arbitrary substrings. This keeps statuses such as "incomplete" and
// "inactive" from matching "complete" and "active" respectively. A small
// set of common negation words is also handled so that "not completed" does
// not become a completed status merely because it contains "completed".
func hasTournamentStatusMarker(status, marker string) bool {
	statusTokens := normalizeTournamentStatus(status)
	markerTokens := normalizeTournamentStatus(marker)
	if len(statusTokens) == 0 || len(markerTokens) == 0 || len(markerTokens) > len(statusTokens) {
		return false
	}

	for start := 0; start <= len(statusTokens)-len(markerTokens); start++ {
		if !sameTournamentStatusTokens(statusTokens[start:start+len(markerTokens)], markerTokens) {
			continue
		}
		if !tournamentStatusMarkerNegated(statusTokens, start) {
			return true
		}
	}
	return false
}

// normalizeTournamentStatus splits a source status into lowercase words.
// Punctuation and separators are boundaries, so "in-progress" remains
// compatible with the existing "in progress" marker without reintroducing
// substring matching.
func normalizeTournamentStatus(status string) []string {
	status = strings.ToLower(strings.TrimSpace(status))
	return strings.FieldsFunc(status, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
}

func sameTournamentStatusTokens(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func tournamentStatusMarkerNegated(tokens []string, markerStart int) bool {
	// Handle direct negation and common forms such as "not yet completed".
	// The scan stops at an unrelated word so that a negation in an earlier,
	// independent clause does not suppress a later positive status marker.
	for i := markerStart - 1; i >= 0 && markerStart-i <= 3; i-- {
		switch tokens[i] {
		case "not", "no", "never", "non", "un", "in", "nicht", "kein", "keine", "keinen", "keinem", "keiner":
			return true
		case "yet", "still", "currently", "mehr", "noch":
			continue
		default:
			return false
		}
	}
	return false
}

// TournamentDateBeforeToday applies the ranking policy timezone to a
// tournament date. A nil date is not considered in the past.
func TournamentDateBeforeToday(value *time.Time, now time.Time) bool {
	if value == nil {
		return false
	}
	location, err := time.LoadLocation(RankingLocation)
	if err != nil {
		return false
	}
	event := value.In(location)
	today := now.In(location)
	eventDay := time.Date(event.Year(), event.Month(), event.Day(), 0, 0, 0, 0, location)
	todayDay := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, location)
	return eventDay.Before(todayDay)
}

// IsTournamentCompleted is the canonical completion rule used by standings
// synchronization and period rankings. A canceled or live tournament is not
// completed even when its date is in the past; otherwise an explicit terminal
// status or a date before today is sufficient, matching the sync policy.
func IsTournamentCompleted(tournament Tournament, now time.Time) bool {
	status := strings.ToLower(strings.TrimSpace(tournament.Status))
	if IsTournamentCanceledStatus(status) || tournament.IsLive || IsTournamentLiveStatus(status) {
		return false
	}
	return IsTournamentCompletedStatus(status) || TournamentDateBeforeToday(tournament.Date, now)
}
