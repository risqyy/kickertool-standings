package gormrepo

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"kickertool-ranking/internal/adapters"
	"kickertool-ranking/internal/domain"
	"kickertool-ranking/internal/ports"
)

func testRepo(t *testing.T) (*Repository, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	repo, err := New(db, adapters.SystemClock{})
	if err != nil {
		t.Fatal(err)
	}
	return repo, db
}

func tournament(id, name string) domain.Tournament {
	return domain.Tournament{Source: domain.KickertoolAPISource, SourceID: id, SourceKey: "key:" + id, Name: name, URL: "https://example.test/" + id}
}

func TestRepositoryInsertUpdateUnchangedAndUniqueIdentity(t *testing.T) {
	repo, db := testRepo(t)
	ctx := context.Background()
	first := tournament("one", "First")
	result, err := repo.UpsertMany(ctx, []domain.Tournament{first})
	if err != nil || result.Inserted != 1 {
		t.Fatalf("insert: %+v %v", result, err)
	}
	result, err = repo.UpsertMany(ctx, []domain.Tournament{first})
	if err != nil || result.Unchanged != 1 {
		t.Fatalf("unchanged: %+v %v", result, err)
	}
	updated := tournament("one", "Changed")
	result, err = repo.UpsertMany(ctx, []domain.Tournament{updated})
	if err != nil || result.Updated != 1 {
		t.Fatalf("update: %+v %v", result, err)
	}
	var count int64
	db.Model(&TournamentModel{}).Count(&count)
	if count != 1 {
		t.Fatalf("duplicate row count=%d", count)
	}
	got, err := repo.FindBySourceID(ctx, domain.KickertoolAPISource, "one")
	if err != nil || got.Name != "Changed" {
		t.Fatalf("find: %+v %v", got, err)
	}
}

func TestRepositoryDeduplicatesTournamentBatchBySourceKey(t *testing.T) {
	repo, db := testRepo(t)
	ctx := context.Background()
	first := domain.Tournament{Source: domain.KickertoolHTMLSource, SourceID: "html-1", SourceKey: " stable-key ", Name: "First", URL: "https://example.test/first"}
	duplicate := first
	duplicate.SourceID = "html-1-duplicate"
	duplicate.Name = "Last representation"
	duplicate.URL = "https://example.test/last"

	result, err := repo.UpsertMany(ctx, []domain.Tournament{first, duplicate})
	if err != nil || result.Found != 1 || result.Inserted != 1 || result.Updated != 0 || result.Unchanged != 0 {
		t.Fatalf("deduplicated insert result=%+v err=%v", result, err)
	}
	var count int64
	if err := db.Model(&TournamentModel{}).Where("source = ? AND source_key = ?", domain.KickertoolHTMLSource, "stable-key").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected one deduplicated tournament, got %d", count)
	}
	var stored TournamentModel
	if err := db.Where("source = ? AND source_key = ?", domain.KickertoolHTMLSource, "stable-key").First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Name != duplicate.Name || stored.SourceID == nil || *stored.SourceID != duplicate.SourceID {
		t.Fatalf("last representation was not stored: %+v", stored)
	}

	result, err = repo.UpsertMany(ctx, []domain.Tournament{duplicate, duplicate})
	if err != nil || result.Found != 1 || result.Unchanged != 1 || result.Inserted != 0 || result.Updated != 0 {
		t.Fatalf("identical batch result=%+v err=%v", result, err)
	}
	changed := duplicate
	changed.Name = "Changed"
	result, err = repo.UpsertMany(ctx, []domain.Tournament{changed, changed})
	if err != nil || result.Found != 1 || result.Updated != 1 || result.Inserted != 0 || result.Unchanged != 0 {
		t.Fatalf("changed batch result=%+v err=%v", result, err)
	}
	if err := db.Model(&TournamentModel{}).Where("source = ? AND source_key = ?", domain.KickertoolHTMLSource, "stable-key").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("changed batch created duplicate, count=%d", count)
	}
}

func TestRepositoryFallbackKeyAndRollback(t *testing.T) {
	repo, db := testRepo(t)
	ctx := context.Background()
	keyed := domain.Tournament{Source: "other", SourceKey: "stable-key", Name: "Fallback", URL: "https://example.test/fallback"}
	if result, err := repo.UpsertMany(ctx, []domain.Tournament{keyed}); err != nil || result.Inserted != 1 {
		t.Fatalf("fallback: %+v %v", result, err)
	}
	bad := domain.Tournament{Source: "other", Name: "bad", URL: "https://example.test/bad"}
	if _, err := repo.UpsertMany(ctx, []domain.Tournament{tournament("valid", "valid"), bad}); err == nil {
		t.Fatal("expected transaction rollback error")
	}
	var count int64
	db.Model(&TournamentModel{}).Where("source = ?", domain.KickertoolAPISource).Count(&count)
	if count != 0 {
		t.Fatalf("rollback did not remove valid insert, count=%d", count)
	}
	var keyedCount int64
	db.Model(&TournamentModel{}).Where("source = ?", "other").Count(&keyedCount)
	if keyedCount != 1 {
		t.Fatalf("existing row affected by rollback, count=%d", keyedCount)
	}
	_ = time.Now()
}

func standing(tournamentID, standingID, playerID, name string, points float64) domain.TournamentStanding {
	games := 10
	goalDifference := 8
	pointsCents := int64(points * 100)
	return domain.TournamentStanding{Source: domain.KickertoolAPISource, TournamentID: tournamentID, StandingID: standingID,
		StandingKey: "id:" + standingID, PlayerID: playerID, PlayerKey: "id:" + playerID, PlayerName: name,
		PointsCents: &pointsCents, GamesPlayed: &games, GoalDifference: &goalDifference, Rank: intPointer(1), Stats: map[string]float64{"pkt": points}, URL: "https://example.test/standings"}
}

func intPointer(value int) *int { return &value }

func TestStandingSnapshotIdempotencyCorrectionAndDistinctIDs(t *testing.T) {
	repo, db := testRepo(t)
	ctx := context.Background()
	if _, err := repo.UpsertMany(ctx, []domain.Tournament{tournament("t1", "Tournament")}); err != nil {
		t.Fatal(err)
	}
	snapshot := domain.StandingSnapshot{Source: domain.KickertoolAPISource, TournamentID: "t1", Complete: true,
		Standings: []domain.TournamentStanding{standing("t1", "r1", "p1", "Player One", 15), standing("t1", "r2", "p2", "Player Two", 11)}}
	result, err := repo.UpsertStandingSnapshot(ctx, snapshot)
	if err != nil || result.Found != 2 || result.PlayersInserted != 2 || result.StandingsInserted != 2 || result.AggregatesRecalculated != 2 {
		t.Fatalf("first standings result=%+v err=%v", result, err)
	}
	result, err = repo.UpsertStandingSnapshot(ctx, snapshot)
	if err != nil || result.StandingsUnchanged != 2 {
		t.Fatalf("repeat result=%+v err=%v", result, err)
	}
	var playerCount, standingCount int64
	db.Model(&PlayerModel{}).Count(&playerCount)
	db.Model(&StandingModel{}).Count(&standingCount)
	if playerCount != 2 || standingCount != 2 {
		t.Fatalf("duplicate rows players=%d standings=%d", playerCount, standingCount)
	}
	var aggregate PlayerAggregateModel
	if err := db.Where("source = ? AND player_key = ?", domain.KickertoolAPISource, domain.PlayerKey("Player One")).First(&aggregate).Error; err != nil {
		t.Fatal(err)
	}
	if aggregate.TotalPointsCents == nil || *aggregate.TotalPointsCents != 1500 {
		t.Fatalf("aggregate=%+v", aggregate)
	}
	corrected := snapshot
	corrected.Standings = []domain.TournamentStanding{standing("t1", "r1", "p1", "Player One", 20), standing("t1", "r2", "p2", "Player Two", 11)}
	result, err = repo.UpsertStandingSnapshot(ctx, corrected)
	if err != nil || result.StandingsUpdated != 1 {
		t.Fatalf("correction result=%+v err=%v", result, err)
	}
	if err := db.Where("source = ? AND player_key = ?", domain.KickertoolAPISource, domain.PlayerKey("Player One")).First(&aggregate).Error; err != nil {
		t.Fatal(err)
	}
	if aggregate.TotalPointsCents == nil || *aggregate.TotalPointsCents != 2000 {
		t.Fatalf("corrected aggregate=%+v", aggregate)
	}
	if aggregate.PointsPerGameCents == nil || *aggregate.PointsPerGameCents != 200 || aggregate.GamesPlayed == nil || *aggregate.GamesPlayed != 10 || aggregate.GoalDifference == nil || *aggregate.GoalDifference != 8 {
		t.Fatalf("aggregate match metrics=%+v", aggregate)
	}
}

func TestStandingSnapshotIncompleteDoesNotOverwriteAndMissingRowsRemain(t *testing.T) {
	repo, db := testRepo(t)
	ctx := context.Background()
	if _, err := repo.UpsertMany(ctx, []domain.Tournament{tournament("t1", "Tournament")}); err != nil {
		t.Fatal(err)
	}
	complete := domain.StandingSnapshot{Source: domain.KickertoolAPISource, TournamentID: "t1", Complete: true, Standings: []domain.TournamentStanding{standing("t1", "r1", "p1", "Player One", 15)}}
	if _, err := repo.UpsertStandingSnapshot(ctx, complete); err != nil {
		t.Fatal(err)
	}
	incomplete := complete
	incomplete.Complete = false
	incomplete.Standings = []domain.TournamentStanding{standing("t1", "r1", "p1", "Player One", 99)}
	if _, err := repo.UpsertStandingSnapshot(ctx, incomplete); err == nil {
		t.Fatal("expected incomplete snapshot rejection")
	}
	var saved StandingModel
	if err := db.Where("standing_key = ?", "id:r1").First(&saved).Error; err != nil {
		t.Fatal(err)
	}
	if saved.PointsCents == nil || *saved.PointsCents != 1500 {
		t.Fatalf("incomplete snapshot overwrote saved result: %+v", saved)
	}
	var count int64
	db.Model(&StandingModel{}).Count(&count)
	if count != 1 {
		t.Fatalf("unexpected standing deletion/count=%d", count)
	}
}

func TestListPlayerRankingIsDeterministicallySorted(t *testing.T) {
	repo, _ := testRepo(t)
	ctx := context.Background()
	if _, err := repo.UpsertMany(ctx, []domain.Tournament{tournament("t1", "Tournament")}); err != nil {
		t.Fatal(err)
	}
	low := standing("t1", "r1", "p1", "Player One", 5)
	high := standing("t1", "r2", "p2", "Player Two", 20)
	if _, err := repo.UpsertStandingSnapshot(ctx, domain.StandingSnapshot{
		Source: domain.KickertoolAPISource, TournamentID: "t1", Complete: true,
		Standings: []domain.TournamentStanding{low, high},
	}); err != nil {
		t.Fatal(err)
	}

	ranking, err := repo.ListPlayerRanking(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(ranking) != 2 || ranking[0].PlayerName != "Player Two" || ranking[1].PlayerName != "Player One" {
		t.Fatalf("unexpected ranking order: %+v", ranking)
	}
}

func TestPlayersUseCanonicalNameAcrossSourcesAndKeepExternalAliases(t *testing.T) {
	repo, db := testRepo(t)
	ctx := context.Background()
	if _, err := repo.UpsertMany(ctx, []domain.Tournament{
		tournament("api-tournament", "API Tournament"),
		{Source: domain.KickertoolHTMLSource, SourceID: "html-tournament", SourceKey: "html-tournament", Name: "HTML Tournament", URL: "https://example.test/html"},
	}); err != nil {
		t.Fatal(err)
	}
	apiPoints := int64(100)
	htmlPoints := int64(200)
	api := domain.TournamentStanding{Source: domain.KickertoolAPISource, TournamentID: "api-tournament", StandingID: "api-result", StandingKey: "api-result", PlayerID: "api-player", PlayerName: "  PLAYER ONE  ", PointsCents: &apiPoints}
	html := domain.TournamentStanding{Source: domain.KickertoolHTMLSource, TournamentID: "html-tournament", StandingID: "html-result", StandingKey: "html-result", PlayerID: "html-player", PlayerName: "player one", PointsCents: &htmlPoints}
	if result, err := repo.UpsertStandingSnapshot(ctx, domain.StandingSnapshot{Source: api.Source, TournamentID: api.TournamentID, Complete: true, Standings: []domain.TournamentStanding{api}}); err != nil || result.PlayersInserted != 1 {
		t.Fatalf("API insert=%+v err=%v", result, err)
	}
	if result, err := repo.UpsertStandingSnapshot(ctx, domain.StandingSnapshot{Source: html.Source, TournamentID: html.TournamentID, Complete: true, Standings: []domain.TournamentStanding{html}}); err != nil || result.PlayersInserted != 0 {
		t.Fatalf("HTML alias insert=%+v err=%v", result, err)
	}
	var players, aliases int64
	db.Model(&PlayerModel{}).Count(&players)
	db.Model(&SourcePlayerIdentityModel{}).Count(&aliases)
	if players != 1 || aliases != 2 {
		t.Fatalf("canonical identity counts players=%d aliases=%d", players, aliases)
	}
}

func TestDifferentNamesWithSameExternalIDRemainSeparatePlayers(t *testing.T) {
	repo, db := testRepo(t)
	ctx := context.Background()
	if _, err := repo.UpsertMany(ctx, []domain.Tournament{
		tournament("t1", "First"),
		tournament("t2", "Second"),
	}); err != nil {
		t.Fatal(err)
	}
	first := domain.TournamentStanding{Source: domain.KickertoolAPISource, TournamentID: "t1", StandingID: "r1", StandingKey: "r1", PlayerID: "reused", PlayerName: "Player One"}
	second := domain.TournamentStanding{Source: domain.KickertoolAPISource, TournamentID: "t2", StandingID: "r2", StandingKey: "r2", PlayerID: "reused", PlayerName: "Player Two"}
	if _, err := repo.UpsertStandingSnapshot(ctx, domain.StandingSnapshot{Source: domain.KickertoolAPISource, TournamentID: "t1", Complete: true, Standings: []domain.TournamentStanding{first}}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertStandingSnapshot(ctx, domain.StandingSnapshot{Source: domain.KickertoolAPISource, TournamentID: "t2", Complete: true, Standings: []domain.TournamentStanding{second}}); err != nil {
		t.Fatal(err)
	}
	var players int64
	db.Model(&PlayerModel{}).Count(&players)
	if players != 2 {
		t.Fatalf("same external ID merged different names: players=%d", players)
	}
}

func TestPlayerDirectorySearchUsesCanonicalAndAliasRelevance(t *testing.T) {
	repo, db := testRepo(t)
	ctx := context.Background()
	now := time.Now()
	players := []PlayerModel{
		{CanonicalNameKey: "player one", DisplayName: "Player One", LastSeenAt: now},
		{CanonicalNameKey: "player two", DisplayName: "Player Two", LastSeenAt: now},
	}
	if err := db.Create(&players).Error; err != nil {
		t.Fatal(err)
	}
	aliases := []PlayerNameAliasModel{
		{NameKey: "player one", DisplayName: "Player One", PlayerID: players[0].ID},
		{NameKey: "player two", DisplayName: "Player Two", PlayerID: players[1].ID},
		{NameKey: "alternate one", DisplayName: "Alternate One", PlayerID: players[0].ID},
	}
	if err := db.Create(&aliases).Error; err != nil {
		t.Fatal(err)
	}
	results, err := repo.SearchPlayers(ctx, "  PLA  ")
	if err != nil || len(results) != 2 || results[0].DisplayName != "Player One" || results[1].DisplayName != "Player Two" {
		t.Fatalf("canonical search results=%+v err=%v", results, err)
	}
	results, err = repo.SearchPlayers(ctx, "Alternate")
	if err != nil || len(results) != 1 || results[0].ID != players[0].ID || results[0].MatchedAlias != "Alternate One" {
		t.Fatalf("alias search results=%+v err=%v", results, err)
	}
	results, err = repo.SearchPlayers(ctx, "")
	if err != nil || len(results) != 0 {
		t.Fatalf("empty search results=%+v err=%v", results, err)
	}
}

func TestStandingHashFinalizationAndChangedSnapshotRevokesFinalization(t *testing.T) {
	repo, _ := testRepo(t)
	ctx := context.Background()
	if _, err := repo.UpsertMany(ctx, []domain.Tournament{tournament("t1", "Tournament")}); err != nil {
		t.Fatal(err)
	}
	snapshot := domain.StandingSnapshot{Source: domain.KickertoolAPISource, TournamentID: "t1", Complete: true,
		Standings: []domain.TournamentStanding{standing("t1", "r1", "p1", "Player One", 15)}}
	if _, err := repo.UpsertStandingSnapshot(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	first, err := repo.FindBySourceID(ctx, domain.KickertoolAPISource, "t1")
	if err != nil || first.FinalizedAt != nil || first.ConsecutiveIdenticalCompleteSnapshots != 1 || first.StandingsHash == "" {
		t.Fatalf("first complete sync state=%+v err=%v", first, err)
	}
	if _, err := repo.UpsertStandingSnapshot(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	second, err := repo.FindBySourceID(ctx, domain.KickertoolAPISource, "t1")
	if err != nil || second.FinalizedAt == nil || second.ConsecutiveIdenticalCompleteSnapshots != 2 {
		t.Fatalf("second complete sync state=%+v err=%v", second, err)
	}
	corrected := snapshot
	corrected.Standings = []domain.TournamentStanding{standing("t1", "r1", "p1", "Player One", 16)}
	if _, err := repo.UpsertStandingSnapshot(ctx, corrected); err != nil {
		t.Fatal(err)
	}
	third, err := repo.FindBySourceID(ctx, domain.KickertoolAPISource, "t1")
	if err != nil || third.FinalizedAt != nil || third.ConsecutiveIdenticalCompleteSnapshots != 1 || third.StandingsHash == second.StandingsHash {
		t.Fatalf("changed complete sync state=%+v err=%v", third, err)
	}
}

func TestTournamentRankingInclusionRecomputesAndCrawlerUpsertPreservesAdminChoice(t *testing.T) {
	repo, db := testRepo(t)
	ctx := context.Background()
	if _, err := repo.UpsertMany(ctx, []domain.Tournament{tournament("included", "Included")}); err != nil {
		t.Fatal(err)
	}
	points := int64(250)
	games := 2
	standing := standing("included", "standing-1", "player-1", "Player One", 25)
	standing.PointsCents = &points
	standing.GamesPlayed = &games
	if _, err := repo.UpsertStandingSnapshot(ctx, domain.StandingSnapshot{Source: domain.KickertoolAPISource, TournamentID: "included", Complete: true, Standings: []domain.TournamentStanding{standing}}); err != nil {
		t.Fatal(err)
	}
	var model TournamentModel
	if err := db.Where("source_id = ?", "included").First(&model).Error; err != nil {
		t.Fatal(err)
	}
	if !model.IncludedInRanking || model.InclusionVersion != 1 {
		t.Fatalf("new tournament inclusion defaults=%+v", model)
	}
	ranking, err := repo.ListPlayerRanking(ctx)
	if err != nil || len(ranking) != 1 || ranking[0].TotalPointsCents == nil || *ranking[0].TotalPointsCents != points {
		t.Fatalf("initial ranking=%+v err=%v", ranking, err)
	}
	changed, err := repo.SetTournamentRankingInclusion(ctx, model.ID, false, 1, "test exclude")
	if err != nil || !changed.Changed || changed.Tournament.IncludedInRanking {
		t.Fatalf("exclude result=%+v err=%v", changed, err)
	}
	ranking, err = repo.ListPlayerRanking(ctx)
	if err != nil || len(ranking) != 0 {
		t.Fatalf("excluded ranking=%+v err=%v", ranking, err)
	}
	if _, err := repo.SetTournamentRankingInclusion(ctx, model.ID, true, 1, "stale"); !errors.Is(err, ports.ErrVersionConflict) {
		t.Fatalf("expected version conflict, got %v", err)
	}
	if _, err := repo.SetTournamentRankingInclusion(ctx, model.ID, true, 2, "test include"); err != nil {
		t.Fatal(err)
	}
	ranking, err = repo.ListPlayerRanking(ctx)
	if err != nil || len(ranking) != 1 || ranking[0].TotalPointsCents == nil || *ranking[0].TotalPointsCents != points {
		t.Fatalf("re-included ranking=%+v err=%v", ranking, err)
	}
	if _, err := repo.UpsertMany(ctx, []domain.Tournament{tournament("included", "Updated source fields")}); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&model, model.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !model.IncludedInRanking {
		t.Fatal("crawler source upsert unexpectedly changed inclusion")
	}
}
