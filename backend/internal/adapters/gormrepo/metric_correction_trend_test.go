package gormrepo

import (
	"context"
	"kickertool-ranking/internal/domain"
	"testing"
	"time"
)

func TestMetricTrendsRetainCorrectionOnlyBaselineBeforeFirstMonthlyTournament(t *testing.T) {
	ctx := context.Background()
	repo, db := testRepoWithClock(t, &mutableRepositoryClock{now: time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)})
	row := standing("latest", "latest-a", "a", "A", 20)
	row.GamesPlayed = intPointer(10)
	row.GoalDifference = intPointer(-2)
	addMonthlyTournament(t, repo, "latest", "2026-09-10T12:00:00Z", row)
	var player PlayerModel
	if err := db.Where("canonical_name_key = ?", domain.PlayerKey("A")).First(&player).Error; err != nil {
		t.Fatal(err)
	}
	_, err := repo.CreateManualRankingCorrection(ctx, domain.ManualRankingCorrectionInput{PlayerID: player.ID, EffectiveDate: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), TournamentCountDelta: 1, GamesPlayedDelta: 10, PointsCentsDelta: 1000, GoalDifferenceDelta: 5, Reason: "baseline", Administrator: "test"}, -1)
	if err != nil {
		t.Fatal(err)
	}
	ranking, err := repo.ListPlayerRankingForMonth(ctx, 2026, 9)
	if err != nil || len(ranking) != 1 {
		t.Fatalf("ranking=%+v err=%v", ranking, err)
	}
	if *ranking[0].PointsPerGameCents != 150 || *ranking[0].GoalDifference != 3 {
		t.Fatalf("current metrics=%+v", ranking[0])
	}
	if ranking[0].PointsPerGameTrend != domain.MetricTrendUp || ranking[0].GoalDifferenceTrend != domain.MetricTrendDown {
		t.Fatalf("valid correction-only baseline was lost: %s/%s", ranking[0].PointsPerGameTrend, ranking[0].GoalDifferenceTrend)
	}
}

func TestMetricTrendsDoNotSubstituteAnotherRealSourceBaseline(t *testing.T) {
	ctx := context.Background()
	repo, _ := testRepoWithClock(t, &mutableRepositoryClock{now: time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)})
	addMonthlyTournament(t, repo, "first", "2026-09-01T12:00:00Z", standing("first", "first-a", "a", "A", 10))
	addMonthlyTournament(t, repo, "latest", "2026-09-10T12:00:00Z", standing("latest", "latest-a", "a", "A", 20))
	ranking, err := repo.ListPlayerRankingForMonth(ctx, 2026, 9)
	if err != nil || len(ranking) != 1 {
		t.Fatalf("ranking=%+v err=%v", ranking, err)
	}
	ranking[0].Source = domain.KickertoolHTMLSource
	year := 2026
	ranking, err = repo.withRankingTrends(ctx, ranking, &year, 9)
	if err != nil {
		t.Fatal(err)
	}
	if ranking[0].PointsPerGameTrend != domain.MetricTrendUnavailable || ranking[0].GoalDifferenceTrend != domain.MetricTrendUnavailable {
		t.Fatalf("metrics must not compare different real sources: %+v", ranking[0])
	}
}
