package domain

import "testing"

func TestIsTournamentCompletedStatus(t *testing.T) {
	for _, status := range []string{
		"finished",
		"completed",
		"complete",
		"done",
		"closed",
		"ended",
		"beendet",
		"abgeschlossen",
	} {
		t.Run(status, func(t *testing.T) {
			if !IsTournamentCompletedStatus(status) {
				t.Fatalf("status %q should be completed", status)
			}
		})
	}
}

func TestIsTournamentCompletedStatusDoesNotMatchNegatedOrPartialStatuses(t *testing.T) {
	for _, status := range []string{
		"incomplete",
		"not completed",
		"not-completed",
		"not yet finished",
		"unfinished",
		"incomplete tournament",
	} {
		t.Run(status, func(t *testing.T) {
			if IsTournamentCompletedStatus(status) {
				t.Fatalf("status %q should not be completed", status)
			}
		})
	}
}

func TestIsTournamentLiveStatusDoesNotMatchInactive(t *testing.T) {
	for _, status := range []string{"inactive", "not active", "in-active"} {
		t.Run(status, func(t *testing.T) {
			if IsTournamentLiveStatus(status) {
				t.Fatalf("status %q should not be live", status)
			}
		})
	}

	for _, status := range []string{"live", "running", "in progress", "active"} {
		t.Run(status, func(t *testing.T) {
			if !IsTournamentLiveStatus(status) {
				t.Fatalf("status %q should be live", status)
			}
		})
	}
}
