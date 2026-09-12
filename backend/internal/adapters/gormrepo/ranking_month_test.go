package gormrepo

import (
	"context"
	"reflect"
	"testing"
	"time"

	"kickertool-ranking/internal/domain"
)

func addMonthlyTournament(t *testing.T, repo *Repository, id, date string, rows ...domain.TournamentStanding) {
	t.Helper()
	when, err := time.Parse(time.RFC3339, date)
	if err != nil {
		t.Fatal(err)
	}
	tournament := domain.Tournament{Source: domain.KickertoolAPISource, SourceID: id, SourceKey: id, Name: id, Date: &when, Status: "finished", URL: "https://example.test/" + id}
	if _, err := repo.UpsertMany(context.Background(), []domain.Tournament{tournament}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertStandingSnapshot(context.Background(), domain.StandingSnapshot{Source: domain.KickertoolAPISource, TournamentID: id, Complete: true, Standings: rows}); err != nil {
		t.Fatal(err)
	}
}

func TestMonthRankingBerlinBoundariesAndWeightedMetrics(t *testing.T) {
	repo, _ := testRepoWithClock(t, &mutableRepositoryClock{now: time.Date(2027, 1, 10, 0, 0, 0, 0, time.UTC)})
	ctx := context.Background()
	for _, item := range []struct {
		id, date string
		points   float64
		games    int
	}{
		{"dec", "2025-12-31T22:59:59Z", 99, 1},
		{"jan", "2025-12-31T23:00:00Z", 10, 2},
		{"jan-end", "2026-01-31T22:59:59Z", 20, 8},
		{"feb", "2026-01-31T23:00:00Z", 50, 1},
		{"mar", "2026-03-31T21:59:59Z", 60, 1},
		{"apr", "2026-03-31T22:00:00Z", 65, 1},
		{"aug", "2026-08-31T21:59:59Z", 70, 1},
		{"sep", "2026-08-31T22:00:00Z", 80, 1},
	} {
		row := standing(item.id, item.id+"-row", "player", "Player", item.points)
		row.GamesPlayed = intPointer(item.games)
		addMonthlyTournament(t, repo, item.id, item.date, row)
	}
	for _, item := range []struct {
		year, month, count, games int
		points, ppg               int64
	}{
		{2025, 12, 1, 1, 9900, 9900}, {2026, 1, 2, 10, 3000, 300}, {2026, 2, 1, 1, 5000, 5000},
		{2026, 3, 1, 1, 6000, 6000}, {2026, 4, 1, 1, 6500, 6500},
		{2026, 8, 1, 1, 7000, 7000}, {2026, 9, 1, 1, 8000, 8000},
	} {
		rows, err := repo.ListPlayerRankingForMonth(ctx, item.year, item.month)
		if err != nil || len(rows) != 1 {
			t.Fatalf("%d-%d rows=%+v err=%v", item.year, item.month, rows, err)
		}
		row := rows[0]
		if row.TournamentCount != item.count || *row.GamesPlayed != item.games || *row.TotalPointsCents != item.points || *row.PointsPerGameCents != item.ppg || *row.GoalDifference != item.count*8 {
			t.Fatalf("%d-%d wrong metrics %+v", item.year, item.month, row)
		}
	}
	months, err := repo.ListAvailableRankingMonths(ctx)
	want := []domain.RankingMonth{{Year: 2026, Month: 9}, {Year: 2026, Month: 8}, {Year: 2026, Month: 4}, {Year: 2026, Month: 3}, {Year: 2026, Month: 2}, {Year: 2026, Month: 1}, {Year: 2025, Month: 12}}
	if err != nil || !reflect.DeepEqual(months, want) {
		t.Fatalf("months=%+v err=%v", months, err)
	}
	empty, err := repo.ListPlayerRankingForMonth(ctx, 2026, 5)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty month=%+v err=%v", empty, err)
	}
}

func TestMonthTrendBaselineKeepsEarlierSameDayAndExcludesOtherMonths(t *testing.T) {
	repo, db := testRepoWithClock(t, &mutableRepositoryClock{now: time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)})
	ctx := context.Background()
	addMonthlyTournament(t, repo, "prior", "2026-01-31T12:00:00Z", standing("prior", "prior-a", "a", "A", 500), standing("prior", "prior-b", "b", "B", 1))
	addMonthlyTournament(t, repo, "first", "2026-02-02T10:00:00Z", standing("first", "first-a", "a", "A", 10), standing("first", "first-b", "b", "B", 20))
	addMonthlyTournament(t, repo, "latest", "2026-02-02T12:00:00Z", standing("latest", "latest-a", "a", "A", 30), standing("latest", "latest-b", "b", "B", 1))
	var player PlayerModel
	if err := db.Where("canonical_name_key = ?", domain.PlayerKey("A")).First(&player).Error; err != nil {
		t.Fatal(err)
	}
	for _, date := range []time.Time{time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC), time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC)} {
		if _, err := repo.CreateManualRankingCorrection(ctx, domain.ManualRankingCorrectionInput{PlayerID: player.ID, EffectiveDate: date, PointsCentsDelta: 10000, Reason: "baseline boundary", Administrator: "test"}, -1); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repo.CreateManualRankingCorrection(ctx, domain.ManualRankingCorrectionInput{PlayerID: player.ID, EffectiveDate: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), PointsCentsDelta: 100, Reason: "earlier in month", Administrator: "test"}, -1); err != nil {
		t.Fatal(err)
	}
	rows, err := repo.ListPlayerRankingForMonth(ctx, 2026, 2)
	if err != nil || len(rows) != 2 || rows[0].PlayerName != "A" || rows[0].Trend != domain.RankingTrendUp || rows[1].Trend != domain.RankingTrendDown {
		t.Fatalf("month trend baseline leaked: rows=%+v err=%v", rows, err)
	}
	year := 2026
	tournaments, err := repo.rankedQualifyingTournaments(ctx, &year, 2)
	if err != nil {
		t.Fatal(err)
	}
	location, _ := time.LoadLocation(domain.RankingLocation)
	latest := tournaments[len(tournaments)-1]
	baseline, err := repo.listRankingBeforeTournament(ctx, tournaments, latest, berlinCalendarDay(latest.Date, location), &year, 2)
	if err != nil || len(baseline) != 2 || baseline[0].PlayerName != "B" || *baseline[0].TotalPointsCents != 2000 || *baseline[1].TotalPointsCents != 1100 {
		t.Fatalf("incorrect scoped baseline=%+v err=%v", baseline, err)
	}
}

func TestMonthCorrectionsEffectiveRevokedMergedAndCorrectionOnlyPeriods(t *testing.T) {
	clock := &mutableRepositoryClock{now: time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)}
	repo, db := testRepoWithClock(t, clock)
	ctx := context.Background()
	addMonthlyTournament(t, repo, "base", "2025-12-20T12:00:00Z", standing("base", "base-row", "source", "Source", 10))
	var source PlayerModel
	if err := db.Where("canonical_name_key = ?", domain.PlayerKey("Source")).First(&source).Error; err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		date  string
		count int
	}{{"2025-12-31T23:00:00Z", 1}, {"2026-02-01T00:00:00Z", 0}, {"2026-03-15T00:00:00Z", 1}} {
		date, _ := time.Parse(time.RFC3339, item.date)
		if _, err := repo.CreateManualRankingCorrection(ctx, domain.ManualRankingCorrectionInput{PlayerID: source.ID, EffectiveDate: date, TournamentCountDelta: item.count, GamesPlayedDelta: 2, PointsCentsDelta: 300, GoalDifferenceDelta: 1, Reason: "month correction", Administrator: "test"}, -1); err != nil {
			t.Fatal(err)
		}
	}
	assertMonths := func(want []domain.RankingMonth) {
		t.Helper()
		got, err := repo.ListAvailableRankingMonths(ctx)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("months=%+v want=%+v err=%v", got, want, err)
		}
	}
	assertMonths([]domain.RankingMonth{{Year: 2026, Month: 1}, {Year: 2025, Month: 12}})
	jan, err := repo.ListPlayerRankingForMonth(ctx, 2026, 1)
	if err != nil || len(jan) != 1 || jan[0].TournamentCount != 1 || *jan[0].TotalPointsCents != 300 || *jan[0].PointsPerGameCents != 150 {
		t.Fatalf("jan corrections=%+v err=%v", jan, err)
	}
	for _, month := range []int{2, 3} {
		rows, err := repo.ListPlayerRankingForMonth(ctx, 2026, month)
		if err != nil || len(rows) != 0 {
			t.Fatalf("month=%d rows=%+v err=%v", month, rows, err)
		}
	}
	target := PlayerModel{CanonicalNameKey: domain.PlayerKey("Target"), DisplayName: "Target", LastSeenAt: clock.now}
	if err := db.Create(&target).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repo.MergePlayers(ctx, source.ID, target.ID, domain.PlayerMergeOptions{Actor: "test", Reason: "month merge"}); err != nil {
		t.Fatal(err)
	}
	jan, err = repo.ListPlayerRankingForMonth(ctx, 2026, 1)
	if err != nil || len(jan) != 1 || jan[0].PlayerName != "Target" || *jan[0].TotalPointsCents != 300 {
		t.Fatalf("merged jan=%+v err=%v", jan, err)
	}
	items, err := repo.ListManualRankingCorrections(ctx, target.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.EffectiveDate.In(time.UTC).Month() == time.December { // Berlin January starts on the preceding UTC day.
			if _, err := repo.RevokeManualRankingCorrection(ctx, target.ID, item.ID, -1, "test", "remove January"); err != nil {
				t.Fatal(err)
			}
		}
	}
	assertMonths([]domain.RankingMonth{{Year: 2025, Month: 12}})
	clock.now = time.Date(2026, 3, 16, 0, 0, 0, 0, time.UTC)
	assertMonths([]domain.RankingMonth{{Year: 2026, Month: 3}, {Year: 2025, Month: 12}})
}

func TestMonthRankingPreservesQualificationAndMissingMetrics(t *testing.T) {
	repo, db := testRepoWithClock(t, &mutableRepositoryClock{now: time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)})
	for _, id := range []string{"included", "excluded", "incomplete", "live"} {
		row := standing(id, id+"-row", id, id, 10)
		if id == "included" {
			row.GamesPlayed = nil
			row.GoalDifference = nil
		}
		addMonthlyTournament(t, repo, id, "2026-02-01T12:00:00Z", row)
	}
	for _, item := range []struct {
		id, column string
		value      any
	}{{"excluded", "included_in_ranking", false}, {"incomplete", "standings_sync_complete", false}, {"live", "is_live", true}} {
		if err := db.Model(&TournamentModel{}).Where("source_id = ?", item.id).Update(item.column, item.value).Error; err != nil {
			t.Fatal(err)
		}
	}
	rows, err := repo.ListPlayerRankingForMonth(context.Background(), 2026, 2)
	if err != nil || len(rows) != 1 || rows[0].PlayerName != "included" || rows[0].GamesPlayed != nil || rows[0].PointsPerGameCents != nil || rows[0].GoalDifference != nil {
		t.Fatalf("qualification/availability: %+v err=%v", rows, err)
	}
}
