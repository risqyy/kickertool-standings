package integration

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"kickertool-ranking/internal/adapters/gormrepo"
	"kickertool-ranking/internal/adapters/kickertoolhtml"
	"kickertool-ranking/internal/app"
	"kickertool-ranking/internal/domain"
)

func TestHTMLRefreshRepairsMergedZeroGameAliasAndRetainsStandingOrigin(t *testing.T) {
	var offline atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if offline.Load() {
			http.Error(w, "offline", 503)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<div data-entry-type="monster_dyp"></div><div class="table"><div class="table-row table-header"><div>#</div><div>Player</div><div>Num.</div><div>G±</div><div>Pkt</div><div>ØP</div></div><div class="table-row body"><div>6</div><div>Full Testname</div><div>23</div><div>0</div><div>24</div><div>1.04</div></div><div class="table-row body"><div>23</div><div>Short Testname</div><div>0</div><div>0</div><div>0</div><div>0.00</div></div></div>`)
	}))
	defer server.Close()
	ctx := context.Background()
	clock := &recheckClock{now: time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)}
	repo, db, err := gormrepo.OpenSQLite(filepath.Join(t.TempDir(), "ranking.db"), clock)
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	date := time.Date(2026, 9, 10, 18, 0, 0, 0, time.UTC)
	tournament := domain.Tournament{Source: domain.KickertoolHTMLSource, SourceID: "alias", SourceKey: "alias", Name: "Alias Cup", Date: &date, Status: "finished", EntryType: "monster_dyp", URL: server.URL + "/community/tournaments/alias/standings"}
	if _, err := repo.UpsertMany(ctx, []domain.Tournament{tournament}); err != nil {
		t.Fatal(err)
	}
	tournament, err = repo.FindBySourceID(ctx, tournament.Source, tournament.SourceID)
	if err != nil {
		t.Fatal(err)
	}
	source, err := kickertoolhtml.NewSource(server.URL+"/community", server.Client(), nil)
	if err != nil {
		t.Fatal(err)
	}
	crawler := app.NewCrawler(source, repo, clock, nil, app.WithStandings(source, repo))
	service := app.NewTournamentRefresher(ctx, crawler, repo)
	refresh := func(want string) {
		t.Helper()
		job, err := service.Start(ctx, tournament.ID)
		if err != nil {
			t.Fatal(err)
		}
		service.Wait()
		job, err = service.Status(ctx, job.ID)
		if err != nil || job.State != want {
			t.Fatalf("job=%+v err=%v", job, err)
		}
	}
	refresh("succeeded")
	var full, short gormrepo.PlayerModel
	if err := db.Where("canonical_name_key = ?", domain.PlayerKey("Full Testname")).First(&full).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("canonical_name_key = ?", domain.PlayerKey("Short Testname")).First(&short).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repo.MergePlayers(ctx, short.ID, full.ID, domain.PlayerMergeOptions{Actor: "test"}); err != nil {
		t.Fatal(err)
	}
	// Simulate the bad stored aggregate from the previous importer version.
	if err := db.Model(&gormrepo.StandingModel{}).Where("player_ref = ?", full.ID).Updates(map[string]any{"games_played": 0, "points_cents": 0, "rank": 23}).Error; err != nil {
		t.Fatal(err)
	}
	for _, failed := range []bool{false, false, true} {
		offline.Store(failed)
		want := "succeeded"
		if failed {
			want = "failed"
		}
		refresh(want)
		stats, err := repo.GetPlayerStatistics(ctx, full.ID)
		if err != nil {
			t.Fatal(err)
		}
		if stats.Player.Aggregate.TournamentCount != 1 || stats.Player.Aggregate.GamesPlayed == nil || *stats.Player.Aggregate.GamesPlayed != 23 || *stats.Player.Aggregate.TotalPointsCents != 2400 || len(stats.Tournaments) != 1 {
			t.Fatalf("stats=%+v", stats)
		}
		var stored gormrepo.StandingModel
		if err := db.Where("player_ref = ?", full.ID).First(&stored).Error; err != nil {
			t.Fatal(err)
		}
		if stored.PlayerName != "Full Testname" || stored.Rank == nil || *stored.Rank != 6 || stored.URL != tournament.URL {
			t.Fatalf("origin=%+v", stored)
		}
	}
}
