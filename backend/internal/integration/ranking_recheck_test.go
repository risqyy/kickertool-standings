package integration

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"kickertool-ranking/internal/adapters"
	"kickertool-ranking/internal/adapters/gormrepo"
	"kickertool-ranking/internal/adapters/httpapi"
	"kickertool-ranking/internal/app"
	"kickertool-ranking/internal/domain"
)

type recheckClock struct {
	adapters.SystemClock
	now time.Time
}

func (c *recheckClock) Now() time.Time { return c.now }

type recheckSource struct {
	tournaments []domain.Tournament
	snapshots   map[string]domain.StandingSnapshot
	failure     bool
	incomplete  bool
}

func (s *recheckSource) FetchTournaments(context.Context) ([]domain.Tournament, error) {
	return s.tournaments, nil
}

func (s *recheckSource) FetchStandings(_ context.Context, tournament domain.Tournament) (domain.StandingSnapshot, error) {
	if tournament.SourceID == "latest" && s.failure {
		return domain.StandingSnapshot{}, errors.New("temporary source failure")
	}
	snapshot := s.snapshots[tournament.SourceID]
	if tournament.SourceID == "latest" && s.incomplete {
		snapshot.Complete = false
	}
	return snapshot, nil
}

// Exercise the four features through the real crawler, SQLite repository and
// public HTTP handler: a failed daily recheck must not change the month scope,
// its metric baseline, or the last fully successful crawl timestamp.
func TestMonthlyMetricTrendsSurviveFinalizedRecheckFailures(t *testing.T) {
	ctx := context.Background()
	clock := &recheckClock{now: time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)}
	repo, db, err := gormrepo.OpenSQLite(filepath.Join(t.TempDir(), "ranking.db"), clock)
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	source := &recheckSource{snapshots: map[string]domain.StandingSnapshot{}}
	add := func(id, date string, points int64, goals int) {
		moment, parseErr := time.Parse(time.RFC3339, date)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		source.tournaments = append(source.tournaments, domain.Tournament{
			Source: domain.KickertoolAPISource, SourceID: id, Name: id,
			Date: &moment, Status: "finished", EntryType: "monster_dyp", URL: "https://example.test/" + id,
		})
		games := 2
		source.snapshots[id] = domain.StandingSnapshot{
			Source: domain.KickertoolAPISource, TournamentID: id, Complete: true,
			Standings: []domain.TournamentStanding{{Source: domain.KickertoolAPISource, TournamentID: id,
				StandingKey: id + "-player", PlayerID: "player", PlayerName: "Player One",
				PointsCents: &points, GamesPlayed: &games, GoalDifference: &goals}},
		}
	}
	add("august", "2026-08-31T21:30:00Z", 10000, 50)
	add("september", "2026-08-31T22:30:00Z", 1000, 4)
	add("latest", "2026-09-05T17:00:00Z", 3000, -6)
	crawler := app.NewCrawler(source, repo, clock, nil, app.WithStandings(source, repo))
	crawl := func(success bool) domain.SyncResult {
		t.Helper()
		result, crawlErr := crawler.Crawl(ctx)
		if (crawlErr == nil) != success {
			t.Fatalf("crawl=%+v error=%v, want success=%v", result, crawlErr, success)
		}
		return result
	}
	crawl(true)
	for _, tournament := range source.tournaments {
		stored, findErr := repo.FindBySourceID(ctx, tournament.Source, tournament.SourceID)
		if findErr != nil {
			t.Fatal(findErr)
		}
		if _, includeErr := repo.SetTournamentRankingInclusion(ctx, stored.ID, true, stored.InclusionVersion, "integration fixture"); includeErr != nil {
			t.Fatal(includeErr)
		}
	}
	players, err := repo.SearchPlayers(ctx, "Player One")
	if err != nil || len(players) != 1 {
		t.Fatalf("players=%+v err=%v", players, err)
	}
	_, err = repo.CreateManualRankingCorrection(ctx, domain.ManualRankingCorrectionInput{
		PlayerID: players[0].ID, EffectiveDate: time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC),
		EffectiveYear: 2026, PointsCentsDelta: 200, Reason: "September correction", Administrator: "test",
	}, players[0].RankingCorrectionVersion)
	if err != nil {
		t.Fatal(err)
	}
	clock.now = clock.now.Add(15 * time.Minute)
	crawl(true)
	for _, tournament := range source.tournaments {
		stored, findErr := repo.FindBySourceID(ctx, tournament.Source, tournament.SourceID)
		if findErr != nil || stored.FinalizedAt == nil {
			t.Fatalf("not finalized: %+v %v", stored, findErr)
		}
	}
	read := func(query string) map[string]any {
		t.Helper()
		response := httptest.NewRecorder()
		httpapi.NewPublicRankingAPIHandler(repo).ServeHTTP(response, httptest.NewRequest("GET", "/api/public/rankings"+query, nil))
		if response.Code != 200 {
			t.Fatalf("GET %s: %d %s", query, response.Code, response.Body.String())
		}
		var payload map[string]any
		if decodeErr := json.Unmarshal(response.Body.Bytes(), &payload); decodeErr != nil {
			t.Fatal(decodeErr)
		}
		return payload
	}
	row := func(payload map[string]any) map[string]any {
		t.Helper()
		items := payload["items"].([]any)
		if len(items) != 1 {
			t.Fatalf("items=%+v", items)
		}
		return items[0].(map[string]any)
	}
	initial := read("?year=2026&month=9")
	initialRow := row(initial)
	for key, want := range map[string]any{"includedTournamentCount": float64(2), "gamesPlayed": float64(4), "totalPoints": "42.00", "pointsPerGame": "10.50", "goalDifference": float64(-2), "pointsPerGameTrend": "up", "goalDifferenceTrend": "down"} {
		if initialRow[key] != want {
			t.Fatalf("initial %s=%v, want %v", key, initialRow[key], want)
		}
	}
	if row(read("?year=2026&month=8"))["totalPoints"] != "100.00" {
		t.Fatal("Berlin month boundary leaked")
	}
	clock.now = clock.now.Add(15 * time.Minute)
	if result := crawl(true); result.TournamentsSkipped != 3 {
		t.Fatalf("early recheck: %+v", result)
	}
	unchanged := read("?year=2026&month=9")
	if unchanged["lastSyncAt"] == initial["lastSyncAt"] || !reflect.DeepEqual(row(unchanged), initialRow) {
		t.Fatal("unchanged crawl must advance only successful sync time")
	}
	clock.now = clock.now.Add(24 * time.Hour)
	source.failure = true
	crawl(false)
	failed := read("?year=2026&month=9")
	if failed["lastSyncAt"] != unchanged["lastSyncAt"] || !reflect.DeepEqual(row(failed), initialRow) {
		t.Fatal("failed recheck changed retained month or successful sync time")
	}
	source.failure = false
	source.incomplete = true
	clock.now = clock.now.Add(15 * time.Minute)
	crawl(false)
	if incomplete := read("?year=2026&month=9"); incomplete["lastSyncAt"] != unchanged["lastSyncAt"] || !reflect.DeepEqual(row(incomplete), initialRow) {
		t.Fatal("incomplete recheck changed retained ranking")
	}
	source.incomplete = false
	corrected := source.snapshots["latest"]
	points, goals := int64(0), 10
	corrected.Standings[0].PointsCents, corrected.Standings[0].GoalDifference = &points, &goals
	source.snapshots["latest"] = corrected
	clock.now = clock.now.Add(15 * time.Minute)
	crawl(true)
	final := read("?year=2026&month=9")
	finalRow := row(final)
	for key, want := range map[string]any{"includedTournamentCount": float64(2), "gamesPlayed": float64(4), "totalPoints": "12.00", "pointsPerGame": "3.00", "goalDifference": float64(14), "pointsPerGameTrend": "down", "goalDifferenceTrend": "up"} {
		if finalRow[key] != want {
			t.Fatalf("corrected %s=%v, want %v", key, finalRow[key], want)
		}
	}
	if final["lastSyncAt"] == unchanged["lastSyncAt"] {
		t.Fatal("successful retry did not advance sync time")
	}
	for _, query := range []string{"", "?year=2026", "?year=2026&month=8"} {
		if read(query)["lastSyncAt"] != final["lastSyncAt"] {
			t.Fatalf("sync status depends on period %s", query)
		}
	}
	clock.now = clock.now.Add(15 * time.Minute)
	crawl(true)
	if !reflect.DeepEqual(row(read("?year=2026&month=9")), finalRow) {
		t.Fatal("unchanged retry duplicated results or changed trends")
	}
}
