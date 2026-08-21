package gormrepo

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"kickertool-ranking/internal/domain"
	"kickertool-ranking/internal/ports"
)

func mergeStanding(tournamentID, standingID, playerID, name string, points int64, games, goals int) domain.TournamentStanding {
	return domain.TournamentStanding{
		Source: domain.KickertoolAPISource, TournamentID: tournamentID,
		StandingID: standingID, StandingKey: standingID, PlayerID: playerID,
		PlayerName: name, PointsCents: &points, GamesPlayed: &games,
		GoalDifference: &goals, URL: "https://example.test/standings",
	}
}

func mergeSnapshot(standing domain.TournamentStanding) domain.StandingSnapshot {
	return domain.StandingSnapshot{
		Source: standing.Source, TournamentID: standing.TournamentID, Complete: true,
		Standings:   []domain.TournamentStanding{standing},
		Allocations: []domain.StandingAllocation{{StandingID: standing.StandingID, PlayerID: standing.PlayerID, PointsCents: value(standing.PointsCents)}},
	}
}

func value(input *int64) int64 {
	if input == nil {
		return 0
	}
	return *input
}

func TestMergePlayersTransfersAliasesRecomputesAndIsIdempotent(t *testing.T) {
	repo, db := testRepo(t)
	ctx := context.Background()
	if _, err := repo.UpsertMany(ctx, []domain.Tournament{tournament("merge-t1", "First"), tournament("merge-t2", "Second")}); err != nil {
		t.Fatal(err)
	}
	source := mergeStanding("merge-t1", "source-result", "source-id", "Old Name", 100, 2, 1)
	targetCollision := mergeStanding("merge-t1", "target-result", "target-id", "New Name", 150, 3, 2)
	targetSecond := mergeStanding("merge-t2", "target-result-2", "target-id", "New Name", 250, 5, 3)
	for _, snapshot := range []domain.StandingSnapshot{mergeSnapshot(source), mergeSnapshot(targetCollision), mergeSnapshot(targetSecond)} {
		if _, err := repo.UpsertStandingSnapshot(ctx, snapshot); err != nil {
			t.Fatal(err)
		}
	}
	var sourcePlayer, targetPlayer PlayerModel
	if err := db.Where("canonical_name_key = ?", domain.PlayerKey("Old Name")).First(&sourcePlayer).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("canonical_name_key = ?", domain.PlayerKey("New Name")).First(&targetPlayer).Error; err != nil {
		t.Fatal(err)
	}

	result, err := repo.MergePlayers(ctx, sourcePlayer.ID, targetPlayer.ID, domain.PlayerMergeOptions{Actor: "test", Reason: "canonical correction"})
	if err != nil {
		t.Fatalf("merge: %+v %v", result, err)
	}
	if result.AlreadyMerged || result.TransferredAliases == 0 || result.TransferredSourceIdentities == 0 || result.DeduplicatedAllocations == 0 {
		t.Fatalf("unexpected merge result: %+v", result)
	}
	var sourceAfter PlayerModel
	if err := db.First(&sourceAfter, sourcePlayer.ID).Error; err != nil || sourceAfter.MergedIntoPlayerID == nil || *sourceAfter.MergedIntoPlayerID != targetPlayer.ID {
		t.Fatalf("source tombstone=%+v err=%v", sourceAfter, err)
	}
	var targetAggregate PlayerAggregateModel
	if err := db.Where("source = ? AND player_key = ?", domain.KickertoolAPISource, domain.PlayerKey("New Name")).First(&targetAggregate).Error; err != nil {
		t.Fatal(err)
	}
	if targetAggregate.TotalPointsCents == nil || *targetAggregate.TotalPointsCents != 400 || targetAggregate.GamesPlayed == nil || *targetAggregate.GamesPlayed != 8 || targetAggregate.GoalDifference == nil || *targetAggregate.GoalDifference != 5 || targetAggregate.TournamentCount != 2 || targetAggregate.PointsPerGameCents == nil || *targetAggregate.PointsPerGameCents != 50 {
		t.Fatalf("aggregate after merge=%+v", targetAggregate)
	}
	ranking, err := repo.ListPlayerRanking(ctx)
	if err != nil || len(ranking) != 1 || ranking[0].PlayerName != "New Name" {
		t.Fatalf("active ranking=%+v err=%v", ranking, err)
	}
	var alias PlayerNameAliasModel
	if err := db.Where("name_key = ?", domain.PlayerKey("Old Name")).First(&alias).Error; err != nil || alias.PlayerID != targetPlayer.ID {
		t.Fatalf("old alias=%+v err=%v", alias, err)
	}
	if _, err := repo.UpsertMany(ctx, []domain.Tournament{tournament("merge-t3", "Future import")}); err != nil {
		t.Fatal(err)
	}
	future := mergeStanding("merge-t3", "future-result", "future-id", "Old Name", 50, 1, 0)
	if _, err := repo.UpsertStandingSnapshot(ctx, mergeSnapshot(future)); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("source = ? AND player_key = ?", domain.KickertoolAPISource, domain.PlayerKey("New Name")).First(&targetAggregate).Error; err != nil {
		t.Fatal(err)
	}
	if targetAggregate.TotalPointsCents == nil || *targetAggregate.TotalPointsCents != 450 || targetAggregate.GamesPlayed == nil || *targetAggregate.GamesPlayed != 9 || targetAggregate.TournamentCount != 3 || targetAggregate.PlayerName != "New Name" {
		t.Fatalf("future alias aggregate=%+v", targetAggregate)
	}

	repeated, err := repo.MergePlayers(ctx, sourcePlayer.ID, targetPlayer.ID, domain.PlayerMergeOptions{})
	if err != nil || !repeated.AlreadyMerged {
		t.Fatalf("repeat merge=%+v err=%v", repeated, err)
	}
	var audits int64
	db.Model(&PlayerMergeAuditModel{}).Count(&audits)
	if audits != 1 {
		t.Fatalf("repeat created audit rows=%d", audits)
	}
}

func TestMergePlayersDryRunRollsBackAndRejectsSelf(t *testing.T) {
	repo, db := testRepo(t)
	ctx := context.Background()
	if _, err := repo.UpsertMany(ctx, []domain.Tournament{tournament("dry-t1", "Dry")}); err != nil {
		t.Fatal(err)
	}
	old := mergeStanding("dry-t1", "dry-r1", "dry-old", "Old", 100, 1, 0)
	newPlayer := mergeStanding("dry-t1", "dry-r2", "dry-new", "New", 200, 2, 0)
	if _, err := repo.UpsertStandingSnapshot(ctx, mergeSnapshot(old)); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertStandingSnapshot(ctx, mergeSnapshot(newPlayer)); err != nil {
		t.Fatal(err)
	}
	var source, target PlayerModel
	db.Where("canonical_name_key = ?", "old").First(&source)
	db.Where("canonical_name_key = ?", "new").First(&target)
	beforeAliases, beforeAudits := int64(0), int64(0)
	db.Model(&PlayerNameAliasModel{}).Count(&beforeAliases)
	db.Model(&PlayerMergeAuditModel{}).Count(&beforeAudits)
	result, err := repo.MergePlayers(ctx, source.ID, target.ID, domain.PlayerMergeOptions{DryRun: true})
	if err != nil || !result.DryRun || result.TransferredAliases == 0 {
		t.Fatalf("dry run=%+v err=%v", result, err)
	}
	var sourceAfter PlayerModel
	if err := db.First(&sourceAfter, source.ID).Error; err != nil || sourceAfter.MergedIntoPlayerID != nil {
		t.Fatalf("dry run changed source=%+v err=%v", sourceAfter, err)
	}
	var afterAliases, afterAudits int64
	db.Model(&PlayerNameAliasModel{}).Count(&afterAliases)
	db.Model(&PlayerMergeAuditModel{}).Count(&afterAudits)
	if beforeAliases != afterAliases || beforeAudits != afterAudits {
		t.Fatalf("dry run wrote aliases/audits before=%d/%d after=%d/%d", beforeAliases, beforeAudits, afterAliases, afterAudits)
	}
	if _, err := repo.MergePlayers(ctx, source.ID, source.ID, domain.PlayerMergeOptions{}); err == nil {
		t.Fatal("expected self merge validation error")
	}
}

func TestUndoPlayerMergeRestoresExactStateAndLeavesOtherPlayersUntouched(t *testing.T) {
	clock := &mutableRepositoryClock{now: time.Date(2026, time.August, 21, 9, 0, 0, 0, time.UTC)}
	repo, db := testRepoWithClock(t, clock)
	ctx := context.Background()
	if _, err := repo.UpsertMany(ctx, []domain.Tournament{tournament("undo-t1", "First"), tournament("undo-t2", "Second"), tournament("undo-t3", "Other")}); err != nil {
		t.Fatal(err)
	}
	sourceStanding := mergeStanding("undo-t1", "undo-source", "undo-source-id", "Undo Source", 100, 2, 1)
	targetCollision := mergeStanding("undo-t1", "undo-target-collision", "undo-target-id", "Undo Target", 150, 3, 2)
	targetSecond := mergeStanding("undo-t2", "undo-target-second", "undo-target-id", "Undo Target", 250, 5, 3)
	otherStanding := mergeStanding("undo-t3", "undo-other", "undo-other-id", "Other Player", 300, 6, 4)
	for _, snapshot := range []domain.StandingSnapshot{mergeSnapshot(sourceStanding), mergeSnapshot(targetCollision), mergeSnapshot(targetSecond), mergeSnapshot(otherStanding)} {
		if _, err := repo.UpsertStandingSnapshot(ctx, snapshot); err != nil {
			t.Fatal(err)
		}
	}
	var source, target, other PlayerModel
	for name, destination := range map[string]*PlayerModel{"Undo Source": &source, "Undo Target": &target, "Other Player": &other} {
		if err := db.Where("canonical_name_key = ?", domain.PlayerKey(name)).First(destination).Error; err != nil {
			t.Fatal(err)
		}
	}
	correction := ManualRankingCorrectionModel{
		PlayerRef: source.ID, PlayerKey: source.CanonicalNameKey, EffectiveDate: clock.now.Add(-time.Hour), EffectiveYear: 2026,
		TournamentCountDelta: 1, GamesPlayedDelta: 1, PointsCentsDelta: 25, GoalDifferenceDelta: 1,
		Reason: "source correction", Administrator: "tester", CreatedAt: clock.now.Add(-time.Hour), Status: manualCorrectionActive, Revision: 1, Version: 1,
	}
	if err := db.Create(&correction).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.recalculatePlayerAggregates(db, source, clock.now); err != nil {
		t.Fatal(err)
	}
	before, err := capturePlayerMergeState(db, source.ID, target.ID)
	if err != nil {
		t.Fatal(err)
	}
	beforeJSON, _, err := encodePlayerMergeState(before)
	if err != nil {
		t.Fatal(err)
	}
	var otherRowsBefore int64
	if err := db.Model(&StandingModel{}).Where("player_ref = ?", other.ID).Count(&otherRowsBefore).Error; err != nil {
		t.Fatal(err)
	}

	mergeResult, err := repo.MergePlayers(ctx, source.ID, target.ID, domain.PlayerMergeOptions{Actor: "tester", Reason: "undo test"})
	if err != nil || mergeResult.DeduplicatedAllocations == 0 {
		t.Fatalf("merge=%+v err=%v", mergeResult, err)
	}
	merges, err := repo.ListPlayerMerges(ctx)
	if err != nil || len(merges) != 1 || !merges[0].UndoAvailable {
		t.Fatalf("merge history=%+v err=%v", merges, err)
	}
	preview, err := repo.PreviewPlayerMergeUndo(ctx, merges[0].ID)
	if err != nil || preview.StateFingerprint == "" || preview.SourceBefore.PlayerName != source.DisplayName || preview.TargetBefore.PlayerName != target.DisplayName {
		t.Fatalf("undo preview=%+v err=%v", preview, err)
	}
	undoResult, err := repo.UndoPlayerMerge(ctx, merges[0].ID, domain.PlayerMergeUndoOptions{Actor: "tester", Reason: "wrong players", ExpectedFingerprint: preview.StateFingerprint})
	if err != nil {
		t.Fatalf("undo: %+v %v", undoResult, err)
	}
	after, err := capturePlayerMergeState(db, source.ID, target.ID)
	if err != nil {
		t.Fatal(err)
	}
	afterJSON, _, err := encodePlayerMergeState(after)
	if err != nil {
		t.Fatal(err)
	}
	if beforeJSON != afterJSON {
		t.Fatalf("restored state differs from pre-merge state\nbefore=%s\nafter=%s", beforeJSON, afterJSON)
	}
	var otherRowsAfter int64
	if err := db.Model(&StandingModel{}).Where("player_ref = ?", other.ID).Count(&otherRowsAfter).Error; err != nil || otherRowsAfter != otherRowsBefore {
		t.Fatalf("other player changed before=%d after=%d err=%v", otherRowsBefore, otherRowsAfter, err)
	}
	var correctionAfter ManualRankingCorrectionModel
	if err := db.First(&correctionAfter, correction.ID).Error; err != nil || correctionAfter.PlayerRef != source.ID || correctionAfter.PlayerKey != source.CanonicalNameKey {
		t.Fatalf("correction not restored=%+v err=%v", correctionAfter, err)
	}
	merges, err = repo.ListPlayerMerges(ctx)
	if err != nil || len(merges) != 1 || merges[0].UndoAvailable || merges[0].UndoneAt == nil || merges[0].UndoneBy != "tester" {
		t.Fatalf("undone history=%+v err=%v", merges, err)
	}
	if _, err := repo.PreviewPlayerMergeUndo(ctx, merges[0].ID); !errors.Is(err, ports.ErrPlayerMergeUndoUnavailable) {
		t.Fatalf("repeated undo preview error=%v", err)
	}
}

func TestUndoPlayerMergeRejectsLegacyAndChangedStateWithoutPartialWrites(t *testing.T) {
	repo, db := testRepo(t)
	ctx := context.Background()
	legacy := PlayerMergeAuditModel{SourcePlayerID: 1, TargetPlayerID: 2, SourceDisplayName: "Legacy Source", TargetDisplayName: "Legacy Target", MergedAt: time.Now()}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repo.PreviewPlayerMergeUndo(ctx, legacy.ID); !errors.Is(err, ports.ErrPlayerMergeUndoUnavailable) {
		t.Fatalf("legacy preview error=%v", err)
	}

	if _, err := repo.UpsertMany(ctx, []domain.Tournament{tournament("changed-t1", "Changed")}); err != nil {
		t.Fatal(err)
	}
	for _, standing := range []domain.TournamentStanding{
		mergeStanding("changed-t1", "changed-source-row", "changed-source-id", "Changed Source", 100, 1, 0),
		mergeStanding("changed-t1", "changed-target-row", "changed-target-id", "Changed Target", 200, 2, 0),
	} {
		if _, err := repo.UpsertStandingSnapshot(ctx, mergeSnapshot(standing)); err != nil {
			t.Fatal(err)
		}
	}
	var source, target PlayerModel
	db.Where("canonical_name_key = ?", domain.PlayerKey("Changed Source")).First(&source)
	db.Where("canonical_name_key = ?", domain.PlayerKey("Changed Target")).First(&target)
	if _, err := repo.MergePlayers(ctx, source.ID, target.ID, domain.PlayerMergeOptions{}); err != nil {
		t.Fatal(err)
	}
	merges, err := repo.ListPlayerMerges(ctx)
	if err != nil || len(merges) != 2 {
		t.Fatalf("merges=%+v err=%v", merges, err)
	}
	changedMerge := merges[0]
	if err := db.Model(&target).Update("display_name", "Changed After Merge").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repo.PreviewPlayerMergeUndo(ctx, changedMerge.ID); !errors.Is(err, ports.ErrVersionConflict) {
		t.Fatalf("changed preview error=%v", err)
	}
	merges, err = repo.ListPlayerMerges(ctx)
	if err != nil || merges[0].UndoAvailable || !strings.Contains(merges[0].UndoUnavailableReason, "geändert") {
		t.Fatalf("changed merge history should explain unavailable undo: merges=%+v err=%v", merges, err)
	}
	var sourceAfter PlayerModel
	if err := db.First(&sourceAfter, source.ID).Error; err != nil || sourceAfter.MergedIntoPlayerID == nil || *sourceAfter.MergedIntoPlayerID != target.ID {
		t.Fatalf("failed undo partially restored source=%+v err=%v", sourceAfter, err)
	}
}
