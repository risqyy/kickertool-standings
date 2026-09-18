package integration

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"kickertool-ranking/internal/adapters/gormrepo"
	"kickertool-ranking/internal/app"
	"kickertool-ranking/internal/domain"
	"kickertool-ranking/internal/ports"
)

type refreshSource struct {
	snapshot         domain.StandingSnapshot
	err              error
	block            chan struct{}
	entered          chan struct{}
	calls            atomic.Int32
	listings         atomic.Int32
	discoveryBlock   chan struct{}
	discoveryEntered chan struct{}
}

func (s *refreshSource) SourceName() string { return domain.KickertoolAPISource }
func (s *refreshSource) FetchTournaments(ctx context.Context) ([]domain.Tournament, error) {
	s.listings.Add(1)
	if s.discoveryEntered != nil {
		s.discoveryEntered <- struct{}{}
	}
	if s.discoveryBlock != nil {
		select {
		case <-s.discoveryBlock:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return nil, nil
}
func (s *refreshSource) FetchStandings(ctx context.Context, _ domain.Tournament) (domain.StandingSnapshot, error) {
	s.calls.Add(1)
	if s.entered != nil {
		s.entered <- struct{}{}
	}
	if s.block != nil {
		select {
		case <-s.block:
		case <-ctx.Done():
			return domain.StandingSnapshot{}, ctx.Err()
		}
	}
	return s.snapshot, s.err
}

func TestSingleTournamentRefreshRetainsDataAndGlobalSuccess(t *testing.T) {
	ctx := context.Background()
	clock := &recheckClock{now: time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)}
	repo, db, err := gormrepo.OpenSQLite(filepath.Join(t.TempDir(), "refresh.db"), clock)
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	date := clock.now.Add(-48 * time.Hour)
	tournament := domain.Tournament{Source: domain.KickertoolAPISource, SourceID: "target", Name: "Target", URL: "https://example.test/target", Date: &date, Status: "finished", EntryType: "monster_dyp"}
	other := tournament
	other.SourceID = "other"
	other.Name = "Other"
	if _, err := repo.UpsertMany(ctx, []domain.Tournament{tournament, other}); err != nil {
		t.Fatal(err)
	}
	points, games, goals := int64(1000), 4, 2
	snapshot := domain.StandingSnapshot{Source: tournament.Source, TournamentID: "target", Complete: true, Standings: []domain.TournamentStanding{{Source: tournament.Source, TournamentID: "target", StandingKey: "p1", PlayerID: "p1", PlayerName: "Player", PointsCents: &points, GamesPlayed: &games, GoalDifference: &goals}}}
	for i := 0; i < 2; i++ {
		if _, err := repo.UpsertStandingSnapshot(ctx, snapshot); err != nil {
			t.Fatal(err)
		}
	}
	stored, _ := repo.FindBySourceID(ctx, tournament.Source, "target")
	if stored.FinalizedAt == nil {
		t.Fatal("fixture not finalized")
	}
	players, _ := repo.SearchPlayers(ctx, "Player")
	target, err := repo.CreateManualPlayer(ctx, domain.PlayerCreationInput{DisplayName: "Canonical", Administrator: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.MergePlayers(ctx, players[0].ID, target.Player.ID, domain.PlayerMergeOptions{Actor: "test", Reason: "same person"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateManualRankingCorrection(ctx, domain.ManualRankingCorrectionInput{PlayerID: target.Player.ID, EffectiveDate: date, PointsCentsDelta: 200, Reason: "manual", Administrator: "test"}, -1); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SetTournamentRankingInclusion(ctx, stored.ID, false, stored.InclusionVersion, "keep excluded"); err != nil {
		t.Fatal(err)
	}
	lastSuccess := clock.now.Add(-time.Hour)
	if err := repo.RecordSuccessfulCrawl(ctx, lastSuccess); err != nil {
		t.Fatal(err)
	}
	source := &refreshSource{snapshot: snapshot}
	crawler := app.NewCrawler(source, repo, clock, nil, app.WithStandings(source, repo))
	service := app.NewTournamentRefresher(ctx, crawler, repo)
	run := func(want string) {
		t.Helper()
		job, err := service.Start(ctx, stored.ID)
		if err != nil {
			t.Fatal(err)
		}
		service.Wait()
		job, err = service.Status(ctx, job.ID)
		if err != nil || job.State != want || job.FinishedAt == nil {
			t.Fatalf("job=%+v err=%v", job, err)
		}
	}
	points = 1500
	run("succeeded")
	checked, _ := repo.GetTournament(ctx, stored.ID)
	if checked.IncludedInRanking || checked.StandingCount != 1 || checked.StandingsSyncedAt == nil || !checked.StandingsSyncedAt.Equal(clock.now) {
		t.Fatalf("checked=%+v", checked)
	}
	clock.now = clock.now.Add(time.Minute)
	run("succeeded")
	checked, _ = repo.GetTournament(ctx, stored.ID)
	if checked.FinalizedAt == nil || checked.StandingCount != 1 || !checked.StandingsSyncedAt.Equal(clock.now) {
		t.Fatalf("repeat=%+v", checked)
	}
	lastChecked := clock.now
	clock.now = clock.now.Add(time.Minute)
	for _, mode := range []string{"fetch", "incomplete", "mismatch"} {
		source.err = nil
		source.snapshot = snapshot
		switch mode {
		case "fetch":
			source.err = errors.New("offline")
		case "incomplete":
			source.snapshot.Complete = false
		case "mismatch":
			source.snapshot.TournamentID = "other"
		}
		run("failed")
		checked, _ = repo.GetTournament(ctx, stored.ID)
		if !checked.StandingsComplete || !checked.LastSyncError || checked.StandingCount != 1 || !checked.StandingsSyncedAt.Equal(lastChecked) {
			t.Fatalf("failed %s lost complete data: %+v", mode, checked)
		}
	}
	source.err = nil
	source.snapshot = snapshot
	run("succeeded")
	checked, _ = repo.GetTournament(ctx, stored.ID)
	if checked.LastSyncError {
		t.Fatal("successful retry retained error")
	}
	if _, err := repo.SetTournamentRankingInclusion(ctx, stored.ID, true, checked.InclusionVersion, "inspect values"); err != nil {
		t.Fatal(err)
	}
	ranking, err := repo.ListPlayerRanking(ctx)
	if err != nil || len(ranking) != 1 || ranking[0].PlayerName != "Canonical" || *ranking[0].TotalPointsCents != 1700 || *ranking[0].GamesPlayed != 4 {
		t.Fatalf("ranking=%+v err=%v", ranking, err)
	}
	global, err := repo.LastSyncAt(ctx)
	if err != nil || global == nil || !global.Equal(lastSuccess) {
		t.Fatalf("global=%v %v", global, err)
	}
	untouched, _ := repo.FindBySourceID(ctx, tournament.Source, "other")
	if untouched.StandingsSyncedAt != nil || source.listings.Load() != 0 {
		t.Fatal("single refresh touched another tournament or ran discovery")
	}
	if _, err := service.Start(ctx, 999999); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	foreign := tournament
	foreign.Source = domain.KickertoolHTMLSource
	foreign.SourceID = "foreign"
	if _, err := repo.UpsertMany(ctx, []domain.Tournament{foreign}); err != nil {
		t.Fatal(err)
	}
	foreign, _ = repo.FindBySourceID(ctx, foreign.Source, foreign.SourceID)
	if _, err := service.Start(ctx, foreign.ID); !errors.Is(err, ports.ErrRefreshSource) {
		t.Fatalf("source: %v", err)
	}
}

func TestRefreshDoesNotOverlapCrawlAndStopsOnShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	clock := &recheckClock{now: time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)}
	repo, db, err := gormrepo.OpenSQLite(filepath.Join(t.TempDir(), "busy.db"), clock)
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	date := clock.now.Add(-time.Hour)
	tournament := domain.Tournament{Source: domain.KickertoolAPISource, SourceID: "t", Name: "T", URL: "https://example.test/t", Date: &date, Status: "finished"}
	if _, err := repo.UpsertMany(ctx, []domain.Tournament{tournament}); err != nil {
		t.Fatal(err)
	}
	tournament, _ = repo.FindBySourceID(ctx, tournament.Source, tournament.SourceID)
	source := &refreshSource{block: make(chan struct{}), entered: make(chan struct{}, 1)}
	crawler := app.NewCrawler(source, repo, clock, nil, app.WithStandings(source, repo))
	service := app.NewTournamentRefresher(ctx, crawler, repo)
	requestCtx, stopRequest := context.WithCancel(ctx)
	job, err := service.Start(requestCtx, tournament.ID)
	if err != nil {
		t.Fatal(err)
	}
	stopRequest() // The accepted job must outlive its start request.
	select {
	case <-source.entered:
	case <-time.After(time.Second):
		t.Fatal("worker never started")
	}
	if _, err := service.Start(ctx, tournament.ID); !errors.Is(err, ports.ErrSyncBusy) {
		t.Fatalf("duplicate start: %v", err)
	}
	crawlCtx, stopCrawl := context.WithTimeout(ctx, 20*time.Millisecond)
	defer stopCrawl()
	if _, err := crawler.Crawl(crawlCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("overlap crawl: %v", err)
	}
	if source.listings.Load() != 0 || source.calls.Load() != 1 {
		t.Fatal("overlapping source requests")
	}
	cancel()
	service.Wait()
	job, err = service.Status(context.Background(), job.ID)
	if err != nil || job.State != "failed" {
		t.Fatalf("shutdown job=%+v %v", job, err)
	}
	if _, err := crawler.Crawl(context.Background()); err != nil {
		t.Fatal("gate not released", err)
	}
	// In the other direction, an already running scheduler crawl rejects manual work.
	source.discoveryBlock = make(chan struct{})
	source.discoveryEntered = make(chan struct{}, 1)
	service = app.NewTournamentRefresher(context.Background(), crawler, repo)
	crawlDone := make(chan error, 1)
	go func() { _, err := crawler.Crawl(context.Background()); crawlDone <- err }()
	select {
	case <-source.discoveryEntered:
	case <-time.After(time.Second):
		t.Fatal("crawl never started")
	}
	if _, err := service.Start(context.Background(), tournament.ID); !errors.Is(err, ports.ErrSyncBusy) {
		t.Fatalf("start during crawl: %v", err)
	}
	close(source.discoveryBlock)
	if err := <-crawlDone; err != nil {
		t.Fatal(err)
	}
}
