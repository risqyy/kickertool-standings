package gormrepo

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"kickertool-ranking/internal/domain"
)

func TestCreateManualPlayerIsAuditedAndDoesNotCreateRankingData(t *testing.T) {
	clock := &mutableRepositoryClock{now: time.Date(2026, time.August, 23, 10, 11, 12, 0, time.UTC)}
	repo, db := testRepoWithClock(t, clock)
	result, err := repo.CreateManualPlayer(context.Background(), domain.PlayerCreationInput{DisplayName: "  Zoë\tMüller  ", Administrator: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Created || result.Player.ID == 0 || result.Player.CanonicalNameKey != "zoë müller" {
		t.Fatalf("creation result=%+v", result)
	}
	if result.Player.DisplayName != "Zoë Müller" || result.Player.CreatedAt != clock.now || result.Player.CreatedBy != "operator" || result.Player.Origin != manualPlayerOrigin {
		t.Fatalf("audit profile=%+v", result.Player)
	}
	var players, aliases, aggregates, standings, tournaments int64
	db.Model(&PlayerModel{}).Count(&players)
	db.Model(&PlayerNameAliasModel{}).Count(&aliases)
	db.Model(&PlayerAggregateModel{}).Count(&aggregates)
	db.Model(&StandingModel{}).Count(&standings)
	db.Model(&TournamentModel{}).Count(&tournaments)
	if players != 1 || aliases != 1 || aggregates != 0 || standings != 0 || tournaments != 0 {
		t.Fatalf("manual identity rows players=%d aliases=%d aggregates=%d standings=%d tournaments=%d", players, aliases, aggregates, standings, tournaments)
	}
	ranking, err := repo.ListPlayerRanking(context.Background())
	if err != nil || len(ranking) != 0 {
		t.Fatalf("ranking changed by identity-only creation: ranking=%+v err=%v", ranking, err)
	}
	years, err := repo.ListAvailableRankingYears(context.Background())
	if err != nil || len(years) != 0 {
		t.Fatalf("available years changed by identity-only creation: years=%v err=%v", years, err)
	}
	items, err := repo.SearchPlayers(context.Background(), "ZOË")
	if err != nil || len(items) != 1 || items[0].ID != result.Player.ID {
		t.Fatalf("search result=%+v err=%v", items, err)
	}

	correctionInput := domain.ManualRankingCorrectionInput{
		PlayerID: result.Player.ID, EffectiveDate: clock.now.Add(-time.Hour), EffectiveYear: 2026,
		TournamentCountDelta: 1, GamesPlayedDelta: 1, PointsCentsDelta: 100,
		Reason: "manual player correction", Administrator: "operator",
	}
	if _, err := repo.CreateManualRankingCorrection(context.Background(), correctionInput, 0); err != nil {
		t.Fatal(err)
	}
	ranking, err = repo.ListPlayerRanking(context.Background())
	if err != nil || len(ranking) != 1 || ranking[0].PlayerKey != result.Player.CanonicalNameKey {
		t.Fatalf("effective correction did not add manual player to ranking: ranking=%+v err=%v", ranking, err)
	}
	years, err = repo.ListAvailableRankingYears(context.Background())
	if err != nil || len(years) != 1 || years[0] != 2026 {
		t.Fatalf("effective correction did not add its year: years=%v err=%v", years, err)
	}
}

func TestCreateManualPlayerReturnsActiveRootForAliasAndMergeConflict(t *testing.T) {
	repo, db := testRepo(t)
	ctx := context.Background()
	first, err := repo.CreateManualPlayer(ctx, domain.PlayerCreationInput{DisplayName: "Original Player", Administrator: "one"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.CreateManualPlayer(ctx, domain.PlayerCreationInput{DisplayName: "Target Player", Administrator: "two"})
	if err != nil {
		t.Fatal(err)
	}
	// Give the first player an alias exactly as a source import or an earlier
	// manual correction could have done, then merge it into the target.
	if err := db.Create(&PlayerNameAliasModel{NameKey: domain.PlayerKey("Original Alias"), DisplayName: "Original Alias", PlayerID: first.Player.ID}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repo.MergePlayers(ctx, first.Player.ID, second.Player.ID, domain.PlayerMergeOptions{Actor: "operator"}); err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{" original player ", "ORIGINAL ALIAS"} {
		conflict, err := repo.CreateManualPlayer(ctx, domain.PlayerCreationInput{DisplayName: input, Administrator: "three"})
		if err != nil {
			t.Fatal(err)
		}
		if conflict.Created || conflict.Player.ID != second.Player.ID || !conflict.Player.Active || conflict.Player.MergedIntoPlayerID != nil {
			t.Fatalf("conflict for %q=%+v", input, conflict)
		}
	}
	var count int64
	db.Model(&PlayerModel{}).Count(&count)
	if count != 2 {
		t.Fatalf("duplicate player rows=%d", count)
	}
}

func TestCrawlerReusesManualPlayerIdentity(t *testing.T) {
	repo, db := testRepo(t)
	ctx := context.Background()
	created, err := repo.CreateManualPlayer(ctx, domain.PlayerCreationInput{DisplayName: "Crawler Match", Administrator: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertMany(ctx, []domain.Tournament{tournament("manual-crawl", "Manual Crawl")}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertStandingSnapshot(ctx, domain.StandingSnapshot{Source: domain.KickertoolAPISource, TournamentID: "manual-crawl", Complete: true, Standings: []domain.TournamentStanding{standing("manual-crawl", "manual-crawl-row", "external-crawl-id", "  CRAWLER\tMATCH ", 12)}}); err != nil {
		t.Fatal(err)
	}
	var row StandingModel
	if err := db.Where("standing_key = ?", "id:manual-crawl-row").First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.PlayerRef != created.Player.ID {
		t.Fatalf("crawler row player_ref=%d manual player=%d", row.PlayerRef, created.Player.ID)
	}
	profile, err := repo.GetPlayerProfile(ctx, created.Player.ID)
	if err != nil || profile.Aggregate.TournamentCount != 1 {
		t.Fatalf("profile=%+v err=%v", profile, err)
	}
	var players, aggregates int64
	db.Model(&PlayerModel{}).Count(&players)
	db.Model(&PlayerAggregateModel{}).Count(&aggregates)
	if players != 1 || aggregates != 1 {
		t.Fatalf("crawler created duplicate player/aggregate players=%d aggregates=%d", players, aggregates)
	}
}

func TestCreateManualPlayerConcurrentIdenticalRequestsCreateOne(t *testing.T) {
	repo, db := testRepo(t)
	ctx := context.Background()
	const calls = 12
	results := make([]domain.PlayerCreationResult, calls)
	errs := make([]error, calls)
	var wg sync.WaitGroup
	for i := 0; i < calls; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			results[index], errs[index] = repo.CreateManualPlayer(ctx, domain.PlayerCreationInput{DisplayName: "  Parallel\tPlayer ", Administrator: "operator"})
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("call %d error=%v", i, err)
		}
	}
	created := 0
	owner := uint(0)
	for _, result := range results {
		if result.Created {
			created++
		}
		if owner == 0 {
			owner = result.Player.ID
		}
		if result.Player.ID != owner || !result.Player.Active {
			t.Fatalf("inconsistent concurrent result=%+v owner=%d", result, owner)
		}
	}
	if created != 1 {
		t.Fatalf("created=%d results=%+v", created, results)
	}
	var count int64
	db.Model(&PlayerModel{}).Count(&count)
	if count != 1 {
		t.Fatalf("player rows=%d", count)
	}
}

func TestCreateManualPlayerValidation(t *testing.T) {
	repo, _ := testRepo(t)
	for _, input := range []string{"", "x", " \t\n ", "!!!", "bad\x00name", strings.Repeat("x", domain.MaxPlayerDisplayNameLength+1), "A" + strings.Repeat(" ", domain.MaxPlayerDisplayNameLength) + "B"} {
		_, err := repo.CreateManualPlayer(context.Background(), domain.PlayerCreationInput{DisplayName: input, Administrator: "operator"})
		if err == nil {
			t.Fatalf("expected validation error for %q", input)
		}
	}
	if _, err := repo.CreateManualPlayer(context.Background(), domain.PlayerCreationInput{DisplayName: "bad\x00name", Administrator: "operator"}); !errors.Is(err, domain.ErrInvalidPlayerName) {
		t.Fatalf("invalid control error=%v", err)
	}
	if _, err := repo.CreateManualPlayer(context.Background(), domain.PlayerCreationInput{DisplayName: strings.Repeat("x", domain.MaxPlayerDisplayNameLength+1), Administrator: "operator"}); !errors.Is(err, domain.ErrPlayerNameTooLong) {
		t.Fatalf("long name error=%v", err)
	}
	if _, err := repo.CreateManualPlayer(context.Background(), domain.PlayerCreationInput{DisplayName: "x", Administrator: "operator"}); !errors.Is(err, domain.ErrPlayerNameTooShort) {
		t.Fatalf("short name error=%v", err)
	}
}

func TestManualPlayerAuditMigrationPreservesLegacyPlayers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-players.db")
	legacy, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := legacy.Exec(`CREATE TABLE player_models (
		id integer PRIMARY KEY AUTOINCREMENT,
		canonical_name_key text NOT NULL,
		display_name text NOT NULL,
		merged_into_player_id integer,
		merged_at datetime,
		last_seen_at datetime NOT NULL,
		created_at datetime,
		updated_at datetime,
		ranking_correction_version integer NOT NULL DEFAULT 0
	)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := legacy.Exec(`INSERT INTO player_models (canonical_name_key, display_name, last_seen_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`, "legacy player", "Legacy Player", time.Now(), time.Now(), time.Now()).Error; err != nil {
		t.Fatal(err)
	}
	sqlDB, err := legacy.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}

	clock := &mutableRepositoryClock{now: time.Date(2026, time.August, 23, 12, 0, 0, 0, time.UTC)}
	repo, migrated, err := OpenSQLite(path, clock)
	if err != nil {
		t.Fatal(err)
	}
	migratedSQL, err := migrated.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = migratedSQL.Close() })
	if !migrated.Migrator().HasColumn(&PlayerModel{}, "CreatedBy") || !migrated.Migrator().HasColumn(&PlayerModel{}, "Origin") {
		t.Fatal("manual player audit columns were not migrated")
	}
	profile, err := repo.GetPlayerProfile(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if profile.DisplayName != "Legacy Player" || profile.CreatedBy != "" || profile.Origin != "" {
		t.Fatalf("legacy player changed during migration: %+v", profile)
	}
}
