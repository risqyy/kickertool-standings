package app

import (
	"context"
	"errors"
	"kickertool-ranking/internal/domain"
	"testing"
	"time"
)

type statusTrackingRepo struct {
	fakeRepo
	last         *time.Time
	statusErr    error
	statusWrites int
}

func (r *statusTrackingRepo) RecordSuccessfulCrawl(_ context.Context, at time.Time) error {
	r.statusWrites++
	if r.statusErr != nil {
		return r.statusErr
	}
	r.last = &at
	return nil
}
func (r *statusTrackingRepo) LastSyncAt(context.Context) (*time.Time, error) { return r.last, nil }

type snapshotSource struct {
	snapshot domain.StandingSnapshot
	err      error
}

func (s snapshotSource) FetchStandings(context.Context, domain.Tournament) (domain.StandingSnapshot, error) {
	return s.snapshot, s.err
}

func TestCrawlSuccessStatusAdvancesOnUnchangedAndEmptyDiscovery(t *testing.T) {
	repo := &statusTrackingRepo{}
	clock := &fixedClock{now: time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)}
	crawler := NewCrawler(fakeSource{}, repo, clock, nil)
	for i := 0; i < 2; i++ {
		previous := repo.last
		result, err := crawler.Crawl(context.Background())
		if err != nil || repo.last == nil || !repo.last.Equal(result.FinishedAt) {
			t.Fatalf("result=%+v last=%v err=%v", result, repo.last, err)
		}
		if previous != nil && !repo.last.After(*previous) {
			t.Fatal("unchanged successful run did not advance status")
		}
	}
}

func TestCrawlFailuresNeverAdvanceSuccessfulStatus(t *testing.T) {
	tournament := domain.Tournament{Source: domain.KickertoolAPISource, SourceID: "old", Name: "Old", URL: "https://example.test/old", Status: "finished"}
	failure := errors.New("unavailable")
	for _, name := range []string{"discovery", "invalid", "persist tournaments", "standings", "incomplete", "canceled", "persist status"} {
		t.Run(name, func(t *testing.T) {
			previous := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
			repo := &statusTrackingRepo{last: &previous}
			source := fakeSource{tournaments: []domain.Tournament{tournament}}
			standings := snapshotSource{snapshot: domain.StandingSnapshot{Complete: true}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch name {
			case "discovery":
				source.err = failure
			case "invalid":
				source.tournaments = append(source.tournaments, domain.Tournament{})
			case "persist tournaments":
				repo.err = failure
			case "standings":
				standings.err = failure
			case "incomplete":
				standings.snapshot.Complete = false
			case "canceled":
				cancel()
			case "persist status":
				repo.statusErr = failure
			}
			standingRepo := &fakeStandingRepo{}
			crawler := NewCrawler(source, repo, &fixedClock{now: previous.Add(time.Hour)}, nil, WithStandings(standings, standingRepo))
			result, err := crawler.Crawl(ctx)
			if err == nil || !repo.last.Equal(previous) {
				t.Fatalf("failure advanced status: result=%+v last=%v err=%v", result, repo.last, err)
			}
			if name != "persist status" && repo.statusWrites != 0 {
				t.Fatal("failed crawl attempted to persist success")
			}
			if name == "incomplete" && standingRepo.calls != 0 {
				t.Fatal("incomplete snapshot reached persistence")
			}
		})
	}
}
