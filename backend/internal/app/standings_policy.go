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
	if domain.IsTournamentCanceledStatus(status) {
		return StandingSyncDecision{Reason: "canceled"}
	}

	live := tournament.IsLive || domain.IsTournamentLiveStatus(status)
	if live {
		return StandingSyncDecision{ShouldSync: true, Reason: "live"}
	}
	if tournament.PreviousIsLive || tournament.StatusTransition == "live_to_not_live" {
		return StandingSyncDecision{ShouldSync: true, Reason: "live_to_not_live_transition"}
	}

	completed := domain.IsTournamentCompleted(tournament, now)
	upcoming := tournament.Date != nil && !domain.TournamentDateBeforeToday(tournament.Date, now) && !completed
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
	return domain.IsTournamentCanceledStatus(status)
}

func isLiveStatus(status string) bool {
	return domain.IsTournamentLiveStatus(status)
}

func isCompletedStatus(status string) bool {
	return domain.IsTournamentCompletedStatus(status)
}

func tournamentDateBeforeToday(value *time.Time, now time.Time) bool {
	return domain.TournamentDateBeforeToday(value, now)
}
