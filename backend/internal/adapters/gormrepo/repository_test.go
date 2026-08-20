package gormrepo

import (
	"context"
	"errors"
	"math"
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

type mutableRepositoryClock struct{ now time.Time }

func (c *mutableRepositoryClock) Now() time.Time                       { return c.now }
func (c *mutableRepositoryClock) NewTicker(time.Duration) ports.Ticker { return nil }

func testRepoWithClock(t *testing.T, clock ports.Clock) (*Repository, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	repo, err := New(db, clock)
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

func TestManualRankingCorrectionIsAuditableAdditiveAndVersioned(t *testing.T) {
	repo, db := testRepo(t)
	ctx := context.Background()
	if _, err := repo.UpsertMany(ctx, []domain.Tournament{tournament("manual-1", "Manual")}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertStandingSnapshot(ctx, domain.StandingSnapshot{Source: domain.KickertoolAPISource, TournamentID: "manual-1", Complete: true, Standings: []domain.TournamentStanding{standing("manual-1", "manual-r1", "manual-p1", "Manual Player", 15)}}); err != nil {
		t.Fatal(err)
	}
	var player PlayerModel
	if err := db.Where("canonical_name_key = ?", domain.PlayerKey("Manual Player")).First(&player).Error; err != nil {
		t.Fatal(err)
	}
	date := time.Date(2026, time.June, 2, 0, 0, 0, 0, time.UTC)
	input := domain.ManualRankingCorrectionInput{PlayerID: player.ID, EffectiveDate: date, TournamentCountDelta: 1, GamesPlayedDelta: 5, PointsCentsDelta: 250, GoalDifferenceDelta: 2, Reason: "nach Turnierprotokoll", Administrator: "operator"}
	preview, err := repo.PreviewManualRankingCorrection(ctx, input)
	if err != nil || preview.ExpectedVersion != 0 || preview.After.TournamentCount != 2 || preview.After.GamesPlayed == nil || *preview.After.GamesPlayed != 15 || preview.After.TotalPointsCents == nil || *preview.After.TotalPointsCents != 1750 || preview.After.PointsPerGameCents == nil || *preview.After.PointsPerGameCents != 117 {
		t.Fatalf("preview=%+v err=%v", preview, err)
	}
	change, err := repo.CreateManualRankingCorrection(ctx, input, preview.ExpectedVersion)
	if err != nil || change.Version != 1 {
		t.Fatalf("create=%+v err=%v", change, err)
	}
	var standingModel StandingModel
	if err := db.Where("standing_key = ?", "id:manual-r1").First(&standingModel).Error; err != nil {
		t.Fatal(err)
	}
	if standingModel.PointsCents == nil || *standingModel.PointsCents != 1500 {
		t.Fatalf("source standing was overwritten: %+v", standingModel)
	}
	ranking, err := repo.ListPlayerRanking(ctx)
	if err != nil || len(ranking) != 1 || ranking[0].TotalPointsCents == nil || *ranking[0].TotalPointsCents != 1750 {
		t.Fatalf("ranking=%+v err=%v", ranking, err)
	}
	if _, err := repo.CreateManualRankingCorrection(ctx, input, preview.ExpectedVersion); !errors.Is(err, ports.ErrVersionConflict) {
		t.Fatalf("stale correction error=%v", err)
	}
	items, err := repo.ListManualRankingCorrections(ctx, player.ID)
	if err != nil || len(items) != 1 || items[0].Status != "active" || items[0].Administrator != "operator" {
		t.Fatalf("corrections=%+v err=%v", items, err)
	}
	revocation, err := repo.RevokeManualRankingCorrection(ctx, player.ID, items[0].ID, change.Version, "operator", "Eingabe widerrufen")
	if err != nil || revocation.After.TotalPointsCents == nil || *revocation.After.TotalPointsCents != 1500 {
		t.Fatalf("revoke=%+v err=%v", revocation, err)
	}
	var revisions int64
	if err := db.Model(&ManualRankingCorrectionRevisionModel{}).Where("correction_id = ?", items[0].ID).Count(&revisions).Error; err != nil || revisions != 2 {
		t.Fatalf("revision count=%d err=%v", revisions, err)
	}
}

func TestManualRankingCorrectionReplaceIsLinkedAndNotDoubleCounted(t *testing.T) {
	repo, db := testRepo(t)
	ctx := context.Background()
	if _, err := repo.UpsertMany(ctx, []domain.Tournament{tournament("replace-1", "Replace")}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertStandingSnapshot(ctx, domain.StandingSnapshot{Source: domain.KickertoolAPISource, TournamentID: "replace-1", Complete: true, Standings: []domain.TournamentStanding{standing("replace-1", "replace-r1", "replace-p1", "Replace Player", 15)}}); err != nil {
		t.Fatal(err)
	}
	var player PlayerModel
	if err := db.Where("canonical_name_key = ?", domain.PlayerKey("Replace Player")).First(&player).Error; err != nil {
		t.Fatal(err)
	}
	oldInput := domain.ManualRankingCorrectionInput{PlayerID: player.ID, EffectiveDate: time.Now(), PointsCentsDelta: 250, GamesPlayedDelta: 1, Reason: "initial booking", Administrator: "operator"}
	old, err := repo.CreateManualRankingCorrection(ctx, oldInput, 0)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, err := repo.PlayerStateFingerprint(ctx, player.ID)
	if err != nil {
		t.Fatal(err)
	}
	newInput := domain.ManualRankingCorrectionInput{PlayerID: player.ID, EffectiveDate: time.Now(), PointsCentsDelta: 500, GamesPlayedDelta: 2, Reason: "corrected protocol", Administrator: "operator", ReplaceCorrectionID: old.Correction.ID}
	preview, err := repo.PreviewManualRankingCorrection(ctx, newInput)
	if err != nil || preview.Superseded == nil || preview.After.TotalPointsCents == nil || *preview.After.TotalPointsCents != 2000 {
		t.Fatalf("replacement preview=%+v err=%v", preview, err)
	}
	change, err := repo.ReplaceManualRankingCorrection(ctx, newInput, old.Correction.ID, old.Version, fingerprint)
	if err != nil || change.Superseded == nil || change.Correction.SupersedesCorrectionID == nil || *change.Correction.SupersedesCorrectionID != old.Correction.ID {
		t.Fatalf("replacement change=%+v err=%v", change, err)
	}
	items, err := repo.ListManualRankingCorrections(ctx, player.ID)
	if err != nil || len(items) != 2 || items[0].Status != "replaced" || items[0].ReplacedByCorrectionID == nil || *items[0].ReplacedByCorrectionID != items[1].ID || items[1].SupersedesCorrectionID == nil || *items[1].SupersedesCorrectionID != items[0].ID {
		t.Fatalf("replacement history=%+v err=%v", items, err)
	}
	if _, err := repo.RevokeManualRankingCorrection(ctx, player.ID, items[0].ID, change.Version, "operator", "should not revoke replaced"); !errors.Is(err, ports.ErrVersionConflict) {
		t.Fatalf("revoke replaced error=%v", err)
	}
	ranking, err := repo.ListPlayerRanking(ctx)
	if err != nil || len(ranking) != 1 || ranking[0].TotalPointsCents == nil || *ranking[0].TotalPointsCents != 2000 || ranking[0].GamesPlayed == nil || *ranking[0].GamesPlayed != 12 {
		t.Fatalf("replacement ranking=%+v err=%v", ranking, err)
	}
	var revisionCount int64
	if err := db.Model(&ManualRankingCorrectionRevisionModel{}).Where("correction_id = ?", items[0].ID).Count(&revisionCount).Error; err != nil || revisionCount != 2 {
		t.Fatalf("old revision count=%d err=%v", revisionCount, err)
	}
}

func TestManualRankingCorrectionRejectsOverflow(t *testing.T) {
	repo, db := testRepo(t)
	ctx := context.Background()
	if _, err := repo.UpsertMany(ctx, []domain.Tournament{tournament("overflow-1", "Overflow")}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertStandingSnapshot(ctx, domain.StandingSnapshot{Source: domain.KickertoolAPISource, TournamentID: "overflow-1", Complete: true, Standings: []domain.TournamentStanding{standing("overflow-1", "overflow-r1", "overflow-p1", "Overflow Player", 15)}}); err != nil {
		t.Fatal(err)
	}
	var player PlayerModel
	if err := db.Where("canonical_name_key = ?", domain.PlayerKey("Overflow Player")).First(&player).Error; err != nil {
		t.Fatal(err)
	}
	input := domain.ManualRankingCorrectionInput{PlayerID: player.ID, EffectiveDate: time.Now(), PointsCentsDelta: math.MaxInt64, Reason: "overflow test", Administrator: "operator"}
	if _, err := repo.PreviewManualRankingCorrection(ctx, input); err == nil {
		t.Fatal("expected unsafe points delta to be rejected")
	}
	base := domain.PlayerAggregate{TournamentCount: 1, GamesPlayed: intPointer(1), TotalPointsCents: int64Pointer(math.MaxInt64), PointsAvailable: true, GamesAvailable: true}
	if _, err := applyCorrectionSafely(base, domain.ManualRankingCorrection{PointsCentsDelta: 1}, false); !errors.Is(err, errAggregateOverflow) {
		t.Fatalf("expected aggregate overflow, got %v", err)
	}
}

func int64Pointer(value int64) *int64 { return &value }

func TestManualCorrectionEffectiveDateChangesOverallYearAndProfileWithoutCrawl(t *testing.T) {
	clock := &mutableRepositoryClock{now: time.Date(2026, time.January, 10, 12, 0, 0, 0, time.UTC)}
	repo, db := testRepoWithClock(t, clock)
	ctx := context.Background()
	tournamentDate := time.Date(2026, time.January, 5, 12, 0, 0, 0, time.UTC)
	if _, err := repo.UpsertMany(ctx, []domain.Tournament{{Source: domain.KickertoolAPISource, SourceID: "effective", SourceKey: "effective", Name: "Effective", Date: &tournamentDate, Status: "finished", URL: "https://example.test/effective"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertStandingSnapshot(ctx, domain.StandingSnapshot{Source: domain.KickertoolAPISource, TournamentID: "effective", Complete: true, Standings: []domain.TournamentStanding{standing("effective", "effective-r1", "effective-p1", "Effective Player", 10)}}); err != nil {
		t.Fatal(err)
	}
	var player PlayerModel
	if err := db.Where("canonical_name_key = ?", domain.PlayerKey("Effective Player")).First(&player).Error; err != nil {
		t.Fatal(err)
	}
	effectiveDate := time.Date(2026, time.January, 12, 0, 0, 0, 0, time.UTC)
	input := domain.ManualRankingCorrectionInput{PlayerID: player.ID, EffectiveDate: effectiveDate, TournamentCountDelta: 1, GamesPlayedDelta: 2, PointsCentsDelta: 300, GoalDifferenceDelta: 1, Reason: "future correction", Administrator: "operator"}
	if _, err := repo.CreateManualRankingCorrection(ctx, input, 0); err != nil {
		t.Fatal(err)
	}
	current, err := repo.ListPlayerRanking(ctx)
	if err != nil || len(current) != 1 || current[0].TournamentCount != 1 || current[0].TotalPointsCents == nil || *current[0].TotalPointsCents != 1000 {
		t.Fatalf("future current=%+v err=%v", current, err)
	}
	profile, err := repo.GetPlayerProfile(ctx, player.ID)
	if err != nil || profile.Aggregate.TournamentCount != 1 {
		t.Fatalf("future profile=%+v err=%v", profile, err)
	}
	yearRanking, err := repo.ListPlayerRankingForYear(ctx, 2026)
	if err != nil || len(yearRanking) != 1 || yearRanking[0].TournamentCount != 1 {
		t.Fatalf("future year=%+v err=%v", yearRanking, err)
	}
	years, err := repo.ListAvailableRankingYears(ctx)
	if err != nil || len(years) != 1 || years[0] != 2026 {
		t.Fatalf("future years=%v err=%v", years, err)
	}
	clock.now = time.Date(2026, time.January, 13, 12, 0, 0, 0, time.UTC)
	current, err = repo.ListPlayerRanking(ctx)
	if err != nil || len(current) != 1 || current[0].TournamentCount != 2 || current[0].TotalPointsCents == nil || *current[0].TotalPointsCents != 1300 {
		t.Fatalf("effective current=%+v err=%v", current, err)
	}
	profile, err = repo.GetPlayerProfile(ctx, player.ID)
	if err != nil || profile.Aggregate.TournamentCount != 2 || profile.Aggregate.PointsPerGameCents == nil || *profile.Aggregate.PointsPerGameCents != 108 {
		t.Fatalf("effective profile=%+v err=%v", profile, err)
	}
	yearRanking, err = repo.ListPlayerRankingForYear(ctx, 2026)
	if err != nil || len(yearRanking) != 1 || yearRanking[0].TournamentCount != 2 {
		t.Fatalf("effective year=%+v err=%v", yearRanking, err)
	}
	// A correction exactly at a snapshot boundary is excluded by Before and
	// included by At, matching the audit comparison contract.
	items, err := repo.ListManualRankingCorrections(ctx, player.ID)
	if err != nil || len(items) != 1 {
		t.Fatalf("correction history=%+v err=%v", items, err)
	}
	cutoff := items[0].EffectiveDate
	before, err := repo.ListPlayerRankingBefore(ctx, cutoff)
	if err != nil || len(before) != 1 || before[0].TournamentCount != 1 {
		t.Fatalf("before cutoff=%+v err=%v", before, err)
	}
	at, err := repo.ListPlayerRankingAt(ctx, cutoff)
	if err != nil || len(at) != 1 || at[0].TournamentCount != 2 {
		t.Fatalf("at cutoff=%+v err=%v", at, err)
	}
}

func TestManualCorrectionFingerprintRejectsCrawlAndMergeBetweenPreviewConfirm(t *testing.T) {
	repo, db := testRepo(t)
	ctx := context.Background()
	if _, err := repo.UpsertMany(ctx, []domain.Tournament{tournament("fingerprint", "Fingerprint")}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertStandingSnapshot(ctx, domain.StandingSnapshot{Source: domain.KickertoolAPISource, TournamentID: "fingerprint", Complete: true, Standings: []domain.TournamentStanding{standing("fingerprint", "fingerprint-r1", "fingerprint-p1", "Fingerprint Player", 10)}}); err != nil {
		t.Fatal(err)
	}
	var source PlayerModel
	if err := db.Where("canonical_name_key = ?", domain.PlayerKey("Fingerprint Player")).First(&source).Error; err != nil {
		t.Fatal(err)
	}
	input := domain.ManualRankingCorrectionInput{PlayerID: source.ID, EffectiveDate: time.Now(), PointsCentsDelta: 100, Reason: "fingerprint test", Administrator: "operator"}
	fingerprint, err := repo.PlayerStateFingerprint(ctx, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	changed := standing("fingerprint", "fingerprint-r1", "fingerprint-p1", "Fingerprint Player", 11)
	if _, err := repo.UpsertStandingSnapshot(ctx, domain.StandingSnapshot{Source: domain.KickertoolAPISource, TournamentID: "fingerprint", Complete: true, Standings: []domain.TournamentStanding{changed}}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateManualRankingCorrectionWithFingerprint(ctx, input, 0, fingerprint); !errors.Is(err, ports.ErrVersionConflict) {
		t.Fatalf("crawl state was not rejected: %v", err)
	}

	if _, err := repo.UpsertMany(ctx, []domain.Tournament{tournament("fingerprint-target", "Target")}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertStandingSnapshot(ctx, domain.StandingSnapshot{Source: domain.KickertoolAPISource, TournamentID: "fingerprint-target", Complete: true, Standings: []domain.TournamentStanding{standing("fingerprint-target", "fingerprint-r2", "fingerprint-p2", "Target Player", 8)}}); err != nil {
		t.Fatal(err)
	}
	var target PlayerModel
	if err := db.Where("canonical_name_key = ?", domain.PlayerKey("Target Player")).First(&target).Error; err != nil {
		t.Fatal(err)
	}
	fingerprint, err = repo.PlayerStateFingerprint(ctx, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.MergePlayers(ctx, source.ID, target.ID, domain.PlayerMergeOptions{Actor: "operator", Reason: "merge between preview"}); err != nil {
		t.Fatal(err)
	}
	input.PlayerID = source.ID
	if _, err := repo.CreateManualRankingCorrectionWithFingerprint(ctx, input, 0, fingerprint); !errors.Is(err, ports.ErrVersionConflict) {
		t.Fatalf("merge state was not rejected: %v", err)
	}
}

func TestManualCorrectionMigrationPreservesExistingSQLiteRows(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:migration-preservation?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	legacyTournament := TournamentModel{Source: domain.KickertoolAPISource, SourceKey: "legacy", Name: "Legacy", URL: "https://example.test/legacy", LastSeenAt: time.Now()}
	legacyPlayer := PlayerModel{CanonicalNameKey: "legacy player", DisplayName: "Legacy Player", LastSeenAt: time.Now()}
	if err := db.AutoMigrate(&TournamentModel{}, &PlayerModel{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&legacyTournament).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&legacyPlayer).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := New(db, &mutableRepositoryClock{now: time.Now()}); err != nil {
		t.Fatal(err)
	}
	var storedTournament TournamentModel
	var storedPlayer PlayerModel
	if err := db.First(&storedTournament, legacyTournament.ID).Error; err != nil || storedTournament.Name != "Legacy" {
		t.Fatalf("tournament lost: %+v err=%v", storedTournament, err)
	}
	if err := db.First(&storedPlayer, legacyPlayer.ID).Error; err != nil || storedPlayer.DisplayName != "Legacy Player" || storedPlayer.RankingCorrectionVersion != 0 {
		t.Fatalf("player lost/default wrong: %+v err=%v", storedPlayer, err)
	}
	if !db.Migrator().HasTable(&ManualRankingCorrectionModel{}) || !db.Migrator().HasTable(&ManualRankingCorrectionRevisionModel{}) {
		t.Fatal("manual correction tables were not migrated")
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

func TestYearRankingUsesOnlyIncludedCompletedTournamentsAndScopesMetrics(t *testing.T) {
	repo, db := testRepo(t)
	ctx := context.Background()
	past2025 := time.Date(2025, time.June, 10, 12, 0, 0, 0, time.FixedZone("source", 2*60*60))
	past2026 := time.Date(2026, time.June, 10, 12, 0, 0, 0, time.FixedZone("source", 2*60*60))
	future := time.Date(2099, time.June, 10, 12, 0, 0, 0, time.FixedZone("source", 2*60*60))
	tournaments := []domain.Tournament{
		{Source: domain.KickertoolAPISource, SourceID: "year-2025", SourceKey: "year-2025", Name: "2025", Date: &past2025, URL: "https://example.test/year-2025"},
		{Source: domain.KickertoolAPISource, SourceID: "year-2026", SourceKey: "year-2026", Name: "2026", Date: &past2026, URL: "https://example.test/year-2026", Status: "finished"},
		{Source: domain.KickertoolAPISource, SourceID: "excluded-2025", SourceKey: "excluded-2025", Name: "Excluded", Date: &past2025, URL: "https://example.test/excluded"},
		{Source: domain.KickertoolAPISource, SourceID: "future-2099", SourceKey: "future-2099", Name: "Future", Date: &future, URL: "https://example.test/future"},
	}
	if _, err := repo.UpsertMany(ctx, tournaments); err != nil {
		t.Fatal(err)
	}
	insertComplete := func(tournamentID string, standings ...domain.TournamentStanding) {
		t.Helper()
		if _, err := repo.UpsertStandingSnapshot(ctx, domain.StandingSnapshot{Source: domain.KickertoolAPISource, TournamentID: tournamentID, Complete: true, Standings: standings}); err != nil {
			t.Fatal(err)
		}
	}
	p1_2025 := standing("year-2025", "2025-p1", "p1", "Player One", 10)
	p2_2025 := standing("year-2025", "2025-p2", "p2", "Player Two", 20)
	p1_2026 := standing("year-2026", "2026-p1", "p1", "Player One", 30)
	p3_excluded := standing("excluded-2025", "excluded-p3", "p3", "Player Three", 99)
	p4_future := standing("future-2099", "future-p4", "p4", "Player Four", 99)
	insertComplete("year-2025", p1_2025, p2_2025)
	insertComplete("year-2026", p1_2026)
	insertComplete("excluded-2025", p3_excluded)
	insertComplete("future-2099", p4_future)
	var excluded TournamentModel
	if err := db.Where("source_id = ?", "excluded-2025").First(&excluded).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&excluded).Update("included_in_ranking", false).Error; err != nil {
		t.Fatal(err)
	}

	years, err := repo.ListAvailableRankingYears(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(years) != 2 || years[0] != 2026 || years[1] != 2025 {
		t.Fatalf("available years=%v, want [2026 2025]", years)
	}

	ranking2025, err := repo.ListPlayerRankingForYear(ctx, 2025)
	if err != nil {
		t.Fatal(err)
	}
	if len(ranking2025) != 2 || ranking2025[0].PlayerName != "Player Two" || ranking2025[1].PlayerName != "Player One" {
		t.Fatalf("2025 ranking=%+v", ranking2025)
	}
	playerOne := ranking2025[1]
	if playerOne.TournamentCount != 1 || playerOne.TotalPointsCents == nil || *playerOne.TotalPointsCents != 1000 || playerOne.GamesPlayed == nil || *playerOne.GamesPlayed != 10 || playerOne.GoalDifference == nil || *playerOne.GoalDifference != 8 || playerOne.PointsPerGameCents == nil || *playerOne.PointsPerGameCents != 100 {
		t.Fatalf("2025 metrics leaked outside period=%+v", playerOne)
	}
	for _, row := range ranking2025 {
		if row.PlayerName == "Player Three" || row.PlayerName == "Player Four" {
			t.Fatalf("non-qualified player appeared in 2025 ranking: %+v", row)
		}
	}

	ranking2024, err := repo.ListPlayerRankingForYear(ctx, 2024)
	if err != nil {
		t.Fatal(err)
	}
	if ranking2024 == nil || len(ranking2024) != 0 {
		t.Fatalf("unavailable year ranking=%+v, want non-nil empty slice", ranking2024)
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
