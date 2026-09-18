package gormrepo

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"kickertool-ranking/internal/domain"
	"kickertool-ranking/internal/ports"
)

func TestPlayerDirectoryListsEmptyAndMergedPlayersWithPaginationAndAliases(t *testing.T) {
	ctx := context.Background()
	repo, _ := testRepo(t)
	var ids []uint
	for _, name := range []string{"Alpha", "Middle", "Zulu"} {
		created, err := repo.CreateManualPlayer(ctx, domain.PlayerCreationInput{DisplayName: name, Administrator: "test"})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, created.Player.ID)
	}
	if _, err := repo.MergePlayers(ctx, ids[0], ids[2], domain.PlayerMergeOptions{Actor: "test"}); err != nil {
		t.Fatal(err)
	}
	first, err := repo.ListPlayers(ctx, domain.PlayerListFilter{Limit: 2})
	if err != nil || first.Total != 3 || first.Page != 1 || len(first.Items) != 2 || first.Items[0].DisplayName != "Alpha" || first.Items[0].Active || first.Items[0].MergedIntoPlayerID == nil {
		t.Fatalf("first=%+v %v", first, err)
	}
	last, err := repo.ListPlayers(ctx, domain.PlayerListFilter{Page: 2, Limit: 2})
	if err != nil || last.Total != 3 || len(last.Items) != 1 || last.Items[0].DisplayName != "Zulu" {
		t.Fatalf("last=%+v %v", last, err)
	}
	for _, tc := range []struct {
		query, state string
		want         int
		id           uint
	}{
		{"alpha", "active", 1, ids[2]}, {"alpha", "merged", 1, ids[0]}, {"  MIDDLE  ", "all", 1, ids[1]}, {"%", "all", 0, 0}, {"_", "all", 0, 0}, {"missing", "all", 0, 0},
	} {
		page, err := repo.ListPlayers(ctx, domain.PlayerListFilter{Query: tc.query, State: tc.state})
		if err != nil || len(page.Items) != tc.want || (tc.want > 0 && page.Items[0].ID != tc.id) {
			t.Fatalf("%+v: %+v %v", tc, page, err)
		}
	}
	stats, err := repo.GetPlayerStatistics(ctx, ids[0])
	if err != nil || stats.RequestedPlayer.ID != ids[0] || stats.RequestedPlayer.Active || stats.Player.ID != ids[2] || stats.Player.DisplayName != "Zulu" || len(stats.Tournaments) != 0 || stats.Player.Aggregate.TournamentCount != 0 {
		t.Fatalf("merged details=%+v %v", stats, err)
	}
	if _, err := repo.GetPlayerStatistics(ctx, 999999); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("unknown id=%v", err)
	}
}

func TestPlayerStatisticsExplainSourceRowsAndIndependentCorrections(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	repo, db := testRepoWithClock(t, &mutableRepositoryClock{now: now})
	for _, row := range []domain.TournamentStanding{
		mergeStanding("first", "first-row", "p", "Test Player", 1000, 4, 2),
		mergeStanding("second", "second-row", "p", "Test Player", 600, 2, -1),
		mergeStanding("excluded", "excluded-row", "p", "Test Player", 9900, 9, 99),
		mergeStanding("zero", "zero-row", "p", "Test Player", 9900, 0, 99),
		mergeStanding("obsolete", "old-row", "p", "Test Player", 9900, 9, 99),
	} {
		addMonthlyTournament(t, repo, row.TournamentID, "2026-09-10T18:00:00Z", row)
	}
	excluded, _ := repo.FindBySourceID(ctx, domain.KickertoolAPISource, "excluded")
	if _, err := repo.SetTournamentRankingInclusion(ctx, excluded.ID, false, excluded.InclusionVersion, "not counted"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertStandingSnapshot(ctx, mergeSnapshot(mergeStanding("obsolete", "new-row", "other", "Other Identity", 100, 1, 1))); err != nil {
		t.Fatal(err)
	}
	var player PlayerModel
	if err := db.Where("canonical_name_key = ?", domain.PlayerKey("Test Player")).First(&player).Error; err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		status string
		date   time.Time
	}{
		{"active", now.Add(-24 * time.Hour)}, {"active", now.Add(24 * time.Hour)}, {"revoked", now.Add(-24 * time.Hour)}, {"replaced", now.Add(-24 * time.Hour)},
	} {
		model := ManualRankingCorrectionModel{PlayerRef: player.ID, PlayerKey: player.CanonicalNameKey, EffectiveDate: tc.date, EffectiveYear: 2026, Status: tc.status, TournamentCountDelta: 1, GamesPlayedDelta: 1, PointsCentsDelta: 250, GoalDifferenceDelta: 3, Reason: "Test correction", Administrator: "test", Revision: 1, Version: 1}
		if err := db.Create(&model).Error; err != nil {
			t.Fatal(err)
		}
	}
	// Keep missing metadata visible, without changing its eligibility.
	if err := db.Model(&StandingModel{}).Where("tournament_id = ?", "first").Update("url", "").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&TournamentModel{}).Where("source_id = ?", "first").Updates(map[string]any{"name": "", "date": nil, "url": ""}).Error; err != nil {
		t.Fatal(err)
	}
	stats, err := repo.GetPlayerStatistics(ctx, player.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !stats.ComputedAt.Equal(now) || len(stats.Tournaments) != 5 || len(stats.Corrections) != 4 {
		t.Fatalf("stats=%+v", stats)
	}
	aggregate := stats.Player.Aggregate
	if aggregate.TournamentCount != 3 || *aggregate.GamesPlayed != 7 || *aggregate.TotalPointsCents != 1850 || *aggregate.GoalDifference != 4 || *aggregate.PointsPerGameCents != 264 {
		t.Fatalf("wrong totals: %+v", aggregate)
	}
	reasons := map[string]string{}
	for _, row := range stats.Tournaments {
		reasons[row.SourceID] = row.Reason
	}
	if !reflect.DeepEqual(reasons, map[string]string{"first": "counted", "second": "counted", "excluded": "excluded", "zero": "zero_games", "obsolete": "superseded"}) {
		t.Fatalf("reasons=%v", reasons)
	}
	last := stats.Tournaments[len(stats.Tournaments)-1]
	if last.SourceID != "first" || last.Date != nil || last.URL != "" || last.Name == "" || *last.TotalPointsCents != 1000 {
		t.Fatalf("missing metadata hidden: %+v", last)
	}
	for _, row := range stats.Tournaments {
		if row.SourceID == "zero" && (row.GamesPlayed == nil || *row.GamesPlayed != 0 || row.PointsPerGameCents != nil || *row.TotalPointsCents != 9900) {
			t.Fatalf("source zero row changed: %+v", row)
		}
	}
	// Same snapshot totals as the existing profile and current public ranking.
	profile, err := repo.GetPlayerProfile(ctx, player.ID)
	if err != nil || !reflect.DeepEqual(profile.Aggregate, aggregate) {
		t.Fatalf("profile mismatch: %+v %v", profile, err)
	}
	public, err := repo.ListPlayerRanking(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range public {
		if row.PlayerKey == player.CanonicalNameKey && (row.TournamentCount != aggregate.TournamentCount || *row.TotalPointsCents != *aggregate.TotalPointsCents) {
			t.Fatalf("public mismatch: %+v", row)
		}
	}
	// Unknown metrics stay unknown both in their row and in the resulting total.
	unknown := mergeStanding("unknown", "unknown-row", "p", "Test Player", 0, 1, 0)
	unknown.GamesPlayed, unknown.PointsCents, unknown.GoalDifference = nil, nil, nil
	addMonthlyTournament(t, repo, "unknown", "2026-09-18T18:00:00Z", unknown)
	stats, err = repo.GetPlayerStatistics(ctx, player.ID)
	if err != nil || stats.Player.Aggregate.TotalPointsCents != nil || stats.Player.Aggregate.GamesPlayed != nil || stats.Player.Aggregate.GoalDifference != nil {
		t.Fatalf("unknown metrics=%+v %v", stats, err)
	}
	if stats.Tournaments[0].SourceID != "unknown" || stats.Tournaments[0].TotalPointsCents != nil || stats.Tournaments[0].Reason != "counted" {
		t.Fatalf("unknown source row=%+v", stats.Tournaments[0])
	}
}

func TestPlayerStatisticsPreserveStoredStandingOriginAfterMerge(t *testing.T) {
	ctx := context.Background()
	repo, db := testRepo(t)
	rank := 6
	row := mergeStanding("origin", "external-result", "source-player", "Source Name", 1000, 23, 8)
	row.Rank = &rank
	row.URL = "https://example.test/tournament/groups/final/standings"
	addMonthlyTournament(t, repo, "origin", "2026-09-10T18:00:00Z", row)
	var source PlayerModel
	if err := db.Where("canonical_name_key = ?", domain.PlayerKey("Source Name")).First(&source).Error; err != nil {
		t.Fatal(err)
	}
	target, err := repo.CreateManualPlayer(ctx, domain.PlayerCreationInput{DisplayName: "Canonical Name", Administrator: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.MergePlayers(ctx, source.ID, target.Player.ID, domain.PlayerMergeOptions{Actor: "test"}); err != nil {
		t.Fatal(err)
	}
	stats, err := repo.GetPlayerStatistics(ctx, source.ID)
	if err != nil || len(stats.Tournaments) != 1 {
		t.Fatalf("statistics=%+v error=%v", stats, err)
	}
	got := stats.Tournaments[0]
	if stats.Player.DisplayName != "Canonical Name" || got.SourcePlayerName != "Source Name" || got.StandingRank == nil || *got.StandingRank != 6 || got.StandingSourceID == nil || *got.StandingSourceID != "external-result" || got.StandingKey != "external-result" || got.URL != row.URL {
		t.Fatalf("lost source provenance: %+v", got)
	}
	// Older source records can lack rank/result ID/name; never synthesize them.
	// Retain the stored key and use the tournament URL only without a row URL.
	if err := db.Model(&StandingModel{}).Where("id = ?", got.ID).Updates(map[string]any{"rank": nil, "source_standing_id": nil, "player_name": "", "url": "", "games_played": 0}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&TournamentModel{}).Where("id = ?", got.TournamentID).Update("url", "https://example.test/tournament").Error; err != nil {
		t.Fatal(err)
	}
	stats, err = repo.GetPlayerStatistics(ctx, target.Player.ID)
	if err != nil {
		t.Fatal(err)
	}
	got = stats.Tournaments[0]
	if got.Reason != "zero_games" || got.StandingRank != nil || got.StandingSourceID != nil || got.SourcePlayerName != "" || got.StandingKey != "external-result" || got.URL != "https://example.test/tournament" {
		t.Fatalf("unknown/non-counted provenance changed: %+v", got)
	}
}
