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

func TestHTMLSingleRefreshRemovesRenamedAndZeroGameContributions(t *testing.T) {
	var name atomic.Value
	name.Store("Original Testname")
	var offline atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/community" {
			t.Error("single refresh performed global discovery")
		}
		if offline.Load() {
			http.Error(w, "unavailable", 503)
			return
		}
		// No result IDs: the real HTML adapter derives its fallback from name/rank.
		fmt.Fprintf(w, `<div data-entry-type="monster_dyp"></div><table><tr><th>Rank</th><th>Player</th><th>Points</th><th>Matches</th><th>Tor-Diff</th></tr><tr><td>1</td><td>%s</td><td>6,00</td><td>3</td><td>2</td></tr><tr><td>2</td><td>Absent Testperson</td><td>9,00</td><td>0</td><td>8</td></tr></table>`, name.Load())
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
	date := clock.now.Add(-48 * time.Hour)
	tournament := domain.Tournament{Source: domain.KickertoolHTMLSource, SourceID: "test", SourceKey: "test", Name: "Test tournament", Date: &date, Status: "finished", EntryType: "monster_dyp", URL: server.URL + "/community/tournaments/test/standings"}
	if _, err := repo.UpsertMany(ctx, []domain.Tournament{tournament}); err != nil {
		t.Fatal(err)
	}
	tournament, _ = repo.FindBySourceID(ctx, tournament.Source, tournament.SourceID)
	source, err := kickertoolhtml.NewSource(server.URL+"/community", server.Client(), nil)
	if err != nil {
		t.Fatal(err)
	}
	crawler := app.NewCrawler(source, repo, clock, nil, app.WithStandings(source, repo))
	service := app.NewTournamentRefresher(ctx, crawler, repo)
	for _, step := range []struct {
		name, state string
		offline     bool
	}{
		{"Original Testname", "succeeded", false},
		{"Corrected Testname", "succeeded", false},
		{"Corrected Testname", "succeeded", false},
		{"Corrected Testname", "failed", true},
	} {
		name.Store(step.name)
		offline.Store(step.offline)
		job, err := service.Start(ctx, tournament.ID)
		if err != nil {
			t.Fatal(err)
		}
		service.Wait()
		job, err = service.Status(ctx, job.ID)
		if err != nil || job.State != step.state {
			t.Fatalf("job=%+v err=%v", job, err)
		}
		rows, err := repo.ListPlayerRanking(ctx)
		if err != nil || len(rows) != 1 || rows[0].PlayerName != step.name || rows[0].TournamentCount != 1 || *rows[0].GamesPlayed != 3 || *rows[0].TotalPointsCents != 600 {
			t.Fatalf("ranking=%+v err=%v", rows, err)
		}
	}
	global, err := repo.LastSyncAt(ctx)
	if err != nil || global != nil {
		t.Fatalf("single refresh advanced global timestamp: %v %v", global, err)
	}
}
