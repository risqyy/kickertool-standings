package app

import (
	"strings"
	"time"

	"kickertool-ranking/internal/domain"
)

type StandingSyncDecision struct {
	ShouldSync bool
	Reason     string
}

// ShouldSyncStandings contains the status-based policy and intentionally has no
// persistence, HTTP, or logging dependencies.
func ShouldSyncStandings(tournament domain.Tournament, now time.Time) StandingSyncDecision {
	status := strings.ToLower(strings.TrimSpace(tournament.Status))
	if isCanceledStatus(status) {
		return StandingSyncDecision{Reason: "canceled"}
	}

	live := tournament.IsLive || isLiveStatus(status)
	if live {
		return StandingSyncDecision{ShouldSync: true, Reason: "live"}
	}
	if tournament.PreviousIsLive || tournament.StatusTransition == "live_to_not_live" {
		return StandingSyncDecision{ShouldSync: true, Reason: "live_to_not_live_transition"}
	}

	completed := isCompletedStatus(status) || tournamentDateBeforeToday(tournament.Date, now)
	upcoming := tournament.Date != nil && !tournamentDateBeforeToday(tournament.Date, now) && !completed
	if upcoming {
		if tournament.StandingsSyncedAt != nil && !tournament.StandingsSyncComplete {
			return StandingSyncDecision{ShouldSync: true, Reason: "retry_incomplete_initial_standing"}
		}
		return StandingSyncDecision{Reason: "upcoming_not_live"}
	}
	if !completed {
		return StandingSyncDecision{Reason: "status_or_date_not_completed"}
	}

	if tournament.FinalizedAt != nil && tournament.StandingsSyncComplete && !tournament.LastStandingsSyncFailed {
		return StandingSyncDecision{Reason: "completed_finalized"}
	}
	if tournament.LastStandingsSyncFailed {
		return StandingSyncDecision{ShouldSync: true, Reason: "retry_after_failure"}
	}
	if tournament.StandingsSyncedAt == nil || !tournament.StandingsSyncComplete {
		return StandingSyncDecision{ShouldSync: true, Reason: "not_fully_synced"}
	}
	return StandingSyncDecision{ShouldSync: true, Reason: "completed_not_finalized"}
}

func isCanceledStatus(status string) bool {
	for _, marker := range []string{"cancel", "abgesagt", "annulliert", "storniert"} {
		if strings.Contains(status, marker) {
			return true
		}
	}
	return false
}

func isLiveStatus(status string) bool {
	for _, marker := range []string{"live", "running", "ongoing", "in progress", "läuft", "laeuft", "started", "active"} {
		if strings.Contains(status, marker) {
			return true
		}
	}
	return false
}

func isCompletedStatus(status string) bool {
	for _, marker := range []string{"finished", "completed", "complete", "done", "closed", "ended", "beendet", "abgeschlossen"} {
		if strings.Contains(status, marker) {
			return true
		}
	}
	return false
}

func tournamentDateBeforeToday(value *time.Time, now time.Time) bool {
	if value == nil {
		return false
	}
	location, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		return false
	}
	event := value.In(location)
	today := now.In(location)
	eventDay := time.Date(event.Year(), event.Month(), event.Day(), 0, 0, 0, 0, location)
	todayDay := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, location)
	return eventDay.Before(todayDay)
}
