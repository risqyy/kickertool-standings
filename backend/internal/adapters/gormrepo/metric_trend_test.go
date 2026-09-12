package gormrepo

import (
	"context"
	"reflect"
	"testing"
	"time"

	"kickertool-ranking/internal/domain"
)

func TestMetricTrendsUseIndependentRoundedValuesAndSurviveUnchangedSnapshots(t *testing.T) {
	clock := &mutableRepositoryClock{now: time.Date(2026, time.January, 20, 12, 0, 0, 0, time.UTC)}
	repo, _ := testRepoWithClock(t, clock)
	ctx := context.Background()
	first := time.Date(2026, time.January, 5, 12, 0, 0, 0, time.UTC)
	latest := first.AddDate(0, 0, 5)
	_, err := repo.UpsertMany(ctx, []domain.Tournament{
		{Source: domain.KickertoolAPISource, SourceID: "metric-first", SourceKey: "metric-first", Name: "First", Date: &first, Status: "finished"},
		{Source: domain.KickertoolAPISource, SourceID: "metric-latest", SourceKey: "metric-latest", Name: "Latest", Date: &latest, Status: "finished"},
	})
	if err != nil {
		t.Fatal(err)
	}
	cents := func(value int64) *int64 { return &value }
	cases := []struct {
		name                                               string
		beforePoints, latestPoints                         *int64
		beforeGames, latestGames, beforeGoals, latestGoals *int
		ppg, goals                                         domain.MetricTrend
	}{
		{"Opposite Up", cents(1000), cents(2000), intPointer(10), intPointer(10), intPointer(5), intPointer(-2), domain.MetricTrendUp, domain.MetricTrendDown},
		{"Opposite Down", cents(2000), cents(1000), intPointer(10), intPointer(10), intPointer(-5), intPointer(2), domain.MetricTrendDown, domain.MetricTrendUp},
		{"Unchanged Rounded", cents(1000), cents(1001), intPointer(10), intPointer(10), intPointer(0), intPointer(0), domain.MetricTrendSame, domain.MetricTrendSame},
		{"Commercial Half", cents(1000), cents(1001), intPointer(1), intPointer(1), intPointer(0), intPointer(0), domain.MetricTrendUp, domain.MetricTrendSame},
		{"Missing Before Games", cents(1000), cents(2000), nil, intPointer(10), intPointer(0), intPointer(2), domain.MetricTrendUnavailable, domain.MetricTrendUp},
		{"Missing After Games", cents(1000), cents(2000), intPointer(10), nil, intPointer(0), intPointer(-2), domain.MetricTrendUnavailable, domain.MetricTrendDown},
		{"Zero Before Games", cents(1000), cents(2000), intPointer(0), intPointer(10), intPointer(0), intPointer(0), domain.MetricTrendUnavailable, domain.MetricTrendSame},
		{"Zero All Games", cents(0), cents(0), intPointer(0), intPointer(0), intPointer(0), intPointer(0), domain.MetricTrendUnavailable, domain.MetricTrendSame},
		{"Missing Before Goals", cents(1000), cents(2000), intPointer(10), intPointer(10), nil, intPointer(2), domain.MetricTrendUp, domain.MetricTrendUnavailable},
		{"Missing After Goals", cents(2000), cents(1000), intPointer(10), intPointer(10), intPointer(2), nil, domain.MetricTrendDown, domain.MetricTrendUnavailable},
		{"Missing Points", nil, cents(1000), intPointer(10), intPointer(10), intPointer(2), intPointer(0), domain.MetricTrendUnavailable, domain.MetricTrendSame},
	}
	snapshots := []domain.StandingSnapshot{
		{Source: domain.KickertoolAPISource, TournamentID: "metric-first", Complete: true},
		{Source: domain.KickertoolAPISource, TournamentID: "metric-latest", Complete: true},
	}
	want := map[string][2]domain.MetricTrend{"New Player": {domain.MetricTrendUnavailable, domain.MetricTrendUnavailable}}
	for _, test := range cases {
		want[test.name] = [2]domain.MetricTrend{test.ppg, test.goals}
		for index := range snapshots {
			row := standing(snapshots[index].TournamentID, snapshots[index].TournamentID+test.name, test.name, test.name, 0)
			if index == 0 {
				row.PointsCents, row.GamesPlayed, row.GoalDifference = test.beforePoints, test.beforeGames, test.beforeGoals
			} else {
				row.PointsCents, row.GamesPlayed, row.GoalDifference = test.latestPoints, test.latestGames, test.latestGoals
			}
			snapshots[index].Standings = append(snapshots[index].Standings, row)
		}
	}
	snapshots[1].Standings = append(snapshots[1].Standings, standing("metric-latest", "new-row", "new", "New Player", 10))
	for attempt := 0; attempt < 2; attempt++ {
		for _, snapshot := range snapshots {
			if _, err := repo.UpsertStandingSnapshot(ctx, snapshot); err != nil {
				t.Fatal(err)
			}
		}
		for _, annual := range []bool{false, true} {
			var ranking []domain.PlayerAggregate
			if annual {
				ranking, err = repo.ListPlayerRankingForYear(ctx, 2026)
			} else {
				ranking, err = repo.ListPlayerRanking(ctx)
			}
			if err != nil {
				t.Fatal(err)
			}
			got := make(map[string][2]domain.MetricTrend)
			for _, row := range ranking {
				got[row.PlayerName] = [2]domain.MetricTrend{row.PointsPerGameTrend, row.GoalDifferenceTrend}
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("attempt=%d annual=%v metrics=%v want=%v", attempt, annual, got, want)
			}
		}
	}
}

func TestMetricTrendMissingBeforeOrAfterValue(t *testing.T) {
	value := int64(123)
	for _, pair := range [][2]*int64{{nil, &value}, {&value, nil}, {nil, nil}} {
		if got := compareMetricValues(pair[0], pair[1]); got != domain.MetricTrendUnavailable {
			t.Fatalf("missing comparison=%q", got)
		}
	}
	for _, games := range []*int{nil, intPointer(0), intPointer(-1)} {
		if validPPG(domain.PlayerAggregate{PointsPerGameCents: &value, GamesPlayed: games}) != nil {
			t.Fatal("invalid games must suppress PPG")
		}
	}
}

func TestMetricTrendsWithoutQualifyingTournamentAreUnavailable(t *testing.T) {
	repo, _ := testRepo(t)
	value := int64(100)
	rows, err := repo.withRankingTrends(context.Background(), []domain.PlayerAggregate{{
		Source: "manual_correction", PlayerKey: "player", PointsPerGameCents: &value, GamesPlayed: intPointer(10), GoalDifference: intPointer(5),
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].PointsPerGameTrend != domain.MetricTrendUnavailable || rows[0].GoalDifferenceTrend != domain.MetricTrendUnavailable {
		t.Fatalf("no tournament must mean no comparison: %+v", rows[0])
	}
}
