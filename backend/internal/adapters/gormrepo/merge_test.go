package gormrepo

import (
	"context"
	"testing"

	"kickertool-ranking/internal/domain"
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
