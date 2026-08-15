package app

import (
	"testing"
	"time"

	"kickertool-ranking/internal/domain"
)

func policyTime() time.Time {
	return time.Date(2026, 8, 14, 12, 0, 0, 0, time.FixedZone("Europe/Berlin", 2*60*60))
}

func policyDate(year int, month time.Month, day int) *time.Time {
	value := time.Date(year, month, day, 0, 0, 0, 0, policyTime().Location())
	return &value
}

func TestShouldSyncStandingsStates(t *testing.T) {
	now := policyTime()
	old := domain.Tournament{Date: policyDate(2026, time.August, 13)}
	future := domain.Tournament{Date: policyDate(2026, time.August, 15)}

	tests := []struct {
		name       string
		tournament domain.Tournament
		wantSync   bool
		wantReason string
	}{
		{name: "never fully successful", tournament: old, wantSync: true, wantReason: "not_fully_synced"},
		{name: "live always", tournament: domain.Tournament{Date: future.Date, IsLive: true, FinalizedAt: timePtr(now)}, wantSync: true, wantReason: "live"},
		{name: "upcoming not live", tournament: future, wantReason: "upcoming_not_live"},
		{name: "live to ended transition", tournament: domain.Tournament{Date: old.Date, PreviousIsLive: true}, wantSync: true, wantReason: "live_to_not_live_transition"},
		{name: "failed retry", tournament: domain.Tournament{Date: old.Date, StandingsSyncedAt: timePtr(now), StandingsSyncComplete: false, LastStandingsSyncFailed: true}, wantSync: true, wantReason: "retry_after_failure"},
		{name: "incomplete retry", tournament: domain.Tournament{Date: old.Date, StandingsSyncedAt: timePtr(now), StandingsSyncComplete: false}, wantSync: true, wantReason: "not_fully_synced"},
		{name: "completed not finalized", tournament: domain.Tournament{Date: old.Date, StandingsSyncedAt: timePtr(now), StandingsSyncComplete: true, ConsecutiveIdenticalCompleteSnapshots: 1}, wantSync: true, wantReason: "completed_not_finalized"},
		{name: "finalized skip", tournament: domain.Tournament{Date: old.Date, StandingsSyncedAt: timePtr(now), StandingsSyncComplete: true, FinalizedAt: timePtr(now)}, wantReason: "completed_finalized"},
		{name: "canceled skip", tournament: domain.Tournament{Date: old.Date, Status: "abgesagt", IsLive: true}, wantReason: "canceled"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := ShouldSyncStandings(test.tournament, now)
			if decision.ShouldSync != test.wantSync || decision.Reason != test.wantReason {
				t.Fatalf("decision=%+v want sync=%v reason=%q", decision, test.wantSync, test.wantReason)
			}
		})
	}
}

func timePtr(value time.Time) *time.Time { return &value }
