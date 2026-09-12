package gormrepo

import (
	"context"
	"errors"
	"kickertool-ranking/internal/app"
	"kickertool-ranking/internal/domain"
	"testing"
	"time"
)

type recheckSource struct {
	tournament domain.Tournament
	snapshot   domain.StandingSnapshot
	err        error
	calls      int
}

func (s *recheckSource) FetchTournaments(context.Context) ([]domain.Tournament, error) {
	return []domain.Tournament{s.tournament}, nil
}
func (s *recheckSource) FetchStandings(context.Context, domain.Tournament) (domain.StandingSnapshot, error) {
	s.calls++
	return s.snapshot, s.err
}

func TestFinalizedRecheckCyclePreservesIdentityCorrectionsAndLastGoodRanking(t *testing.T) {
	ctx := context.Background()
	clock := &mutableRepositoryClock{now: time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)}
	repo, db := testRepoWithClock(t, clock)
	date := clock.now.Add(-48 * time.Hour)
	tournament := tournament("recheck", "Recheck")
	tournament.Status = "finished"
	tournament.Date = &date
	source := &recheckSource{tournament: tournament, snapshot: mergeSnapshot(mergeStanding("recheck", "standing", "player", "Old Name", 1000, 4, 2))}
	crawler := app.NewCrawler(source, repo, clock, nil, app.WithStandings(source, repo))
	crawl := func() {
		t.Helper()
		if result, err := crawler.Crawl(ctx); err != nil {
			t.Fatalf("crawl=%+v err=%v", result, err)
		}
	}
	crawl()
	clock.now = clock.now.Add(15 * time.Minute)
	crawl()
	state, err := repo.FindBySourceID(ctx, tournament.Source, tournament.SourceID)
	if err != nil || state.FinalizedAt == nil {
		t.Fatalf("not finalized: %+v %v", state, err)
	}
	last := *state.StandingsSyncedAt
	// Merge the imported identity, then add an independent administrator correction.
	var oldPlayer PlayerModel
	if err := db.Where("canonical_name_key = ?", domain.PlayerKey("Old Name")).First(&oldPlayer).Error; err != nil {
		t.Fatal(err)
	}
	target, err := repo.CreateManualPlayer(ctx, domain.PlayerCreationInput{DisplayName: "Canonical Name", Administrator: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.MergePlayers(ctx, oldPlayer.ID, target.Player.ID, domain.PlayerMergeOptions{Actor: "test", Reason: "same person"}); err != nil {
		t.Fatal(err)
	}
	correction := domain.ManualRankingCorrectionInput{PlayerID: target.Player.ID, EffectiveDate: date, EffectiveYear: 2026, PointsCentsDelta: 100, Reason: "manual bonus", Administrator: "test"}
	preview, err := repo.PreviewManualRankingCorrection(ctx, correction)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateManualRankingCorrection(ctx, correction, preview.ExpectedVersion); err != nil {
		t.Fatal(err)
	}
	// Explicit exclusion survives discovery and source correction; re-include before ranking checks.
	excluded, err := repo.SetTournamentRankingInclusion(ctx, state.ID, false, 1, "manual exclusion")
	if err != nil {
		t.Fatal(err)
	}
	source.snapshot = mergeSnapshot(mergeStanding("recheck", "standing", "player", "Old Name", 1200, 4, 5))
	clock.now = last.Add(app.FinalizedStandingRecheckInterval - time.Nanosecond)
	crawl()
	if source.calls != 2 {
		t.Fatal("finalized tournament fetched before due time")
	}
	clock.now = last.Add(app.FinalizedStandingRecheckInterval)
	crawl()
	state, err = repo.FindBySourceID(ctx, tournament.Source, tournament.SourceID)
	if err != nil || state.IncludedInRanking || state.FinalizedAt != nil {
		t.Fatalf("recheck lost inclusion or failed to revoke finalization: %+v %v", state, err)
	}
	if _, err := repo.SetTournamentRankingInclusion(ctx, state.ID, true, excluded.Tournament.InclusionVersion, "include again"); err != nil {
		t.Fatal(err)
	}
	assertRanking := func() {
		t.Helper()
		for _, annual := range []bool{false, true} {
			ranking, err := repo.ListPlayerRanking(ctx)
			if annual {
				ranking, err = repo.ListPlayerRankingForYear(ctx, 2026)
			}
			if err != nil || len(ranking) != 1 {
				t.Fatalf("annual=%v ranking=%+v err=%v", annual, ranking, err)
			}
			row := ranking[0]
			if row.PlayerName != "Canonical Name" || row.TotalPointsCents == nil || *row.TotalPointsCents != 1300 || row.GamesPlayed == nil || *row.GamesPlayed != 4 || row.GoalDifference == nil || *row.GoalDifference != 5 || row.TournamentCount != 1 {
				t.Fatalf("annual=%v wrong ranking: %+v", annual, row)
			}
		}
	}
	assertRanking()
	clock.now = clock.now.Add(15 * time.Minute)
	crawl() // Stable corrected result finalizes again.
	last = clock.now
	clock.now = last.Add(app.FinalizedStandingRecheckInterval)
	crawl() // Unchanged daily recheck.
	assertRanking()
	var standingCount, allocationCount int64
	db.Model(&StandingModel{}).Count(&standingCount)
	db.Model(&AllocationModel{}).Count(&allocationCount)
	if standingCount != 1 || allocationCount != 1 {
		t.Fatalf("duplicate results: standings=%d allocations=%d", standingCount, allocationCount)
	}
	last = clock.now
	clock.now = last.Add(app.FinalizedStandingRecheckInterval)
	for _, failure := range []string{"fetch", "incomplete"} {
		source.err = nil
		source.snapshot.Complete = true
		if failure == "fetch" {
			source.err = errors.New("source unavailable")
		} else {
			source.snapshot.Complete = false
		}
		result, err := crawler.Crawl(ctx)
		if err == nil || result.TournamentsFailed != 1 {
			t.Fatalf("failed recheck=%+v %v", result, err)
		}
		state, err = repo.FindBySourceID(ctx, tournament.Source, tournament.SourceID)
		if err != nil || !state.StandingsSyncComplete || !state.LastStandingsSyncFailed || !state.StandingsSyncedAt.Equal(last) {
			t.Fatalf("last complete state lost: %+v %v", state, err)
		}
		syncAt, err := repo.LastSyncAt(ctx)
		if err != nil || syncAt == nil || !syncAt.Equal(last) {
			t.Fatalf("failed recheck advanced global success: %v %v", syncAt, err)
		}
		assertRanking()
		clock.now = clock.now.Add(15 * time.Minute)
	}
	source.err = nil
	source.snapshot.Complete = true
	before := source.calls
	crawl()
	if source.calls != before+1 {
		t.Fatal("failed recheck was not retried next tick")
	}
	assertRanking()
}
