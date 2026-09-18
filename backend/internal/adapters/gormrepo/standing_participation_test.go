package gormrepo

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"kickertool-ranking/internal/domain"
)

func TestCompleteRefreshReconcilesRenamedResults(t *testing.T) {
	for _, mode := range []string{"stable-id", "changed-id", "legacy-duplicate"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			repo, db := testRepoWithClock(t, &mutableRepositoryClock{now: time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)})
			old := mergeStanding("rename", "old-result", "participant", "Earlier Name", 100, 2, 1)
			corrected := mergeStanding("rename", "new-result", "participant", "Corrected Name", 200, 2, 3)
			if mode == "stable-id" {
				corrected.StandingID, corrected.StandingKey = old.StandingID, old.StandingKey
			}
			initial := []domain.TournamentStanding{old}
			if mode == "legacy-duplicate" {
				initial = append(initial, corrected)
			}
			addMonthlyTournament(t, repo, "rename", "2026-09-17T18:00:00Z", initial...)
			// Failed snapshots cannot retire or reassign any existing contribution.
			bad := mergeSnapshot(corrected)
			bad.Complete = false
			before, _ := repo.ListPlayerRanking(ctx)
			if _, err := repo.UpsertStandingSnapshot(ctx, bad); err == nil {
				t.Fatal("incomplete snapshot accepted")
			}
			after, _ := repo.ListPlayerRanking(ctx)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("incomplete snapshot changed ranking")
			}
			for attempt := 0; attempt < 2; attempt++ {
				if _, err := repo.UpsertStandingSnapshot(ctx, mergeSnapshot(corrected)); err != nil {
					t.Fatal(err)
				}
				for _, period := range []string{"overall", "year", "month"} {
					rows := rankingForParticipationTest(t, repo, period)
					if len(rows) != 1 || rows[0].PlayerName != corrected.PlayerName || rows[0].TournamentCount != 1 || *rows[0].TotalPointsCents != 200 {
						t.Fatalf("%s: %+v", period, rows)
					}
				}
			}
			var previous PlayerAggregateModel
			if err := db.Where("player_key = ?", domain.PlayerKey(old.PlayerName)).First(&previous).Error; err != nil || previous.TournamentCount != 0 {
				t.Fatalf("stale aggregate=%+v %v", previous, err)
			}
			stored, _ := repo.FindBySourceID(ctx, old.Source, old.TournamentID)
			admin, _ := repo.GetTournament(ctx, stored.ID)
			if admin.StandingCount != 1 || admin.PlayerCount != 1 {
				t.Fatalf("admin counts: %+v", admin)
			}
			var retained int64
			db.Model(&StandingModel{}).Count(&retained)
			want := int64(2)
			if mode == "stable-id" {
				want = 1
			}
			if retained != want {
				t.Fatalf("source rows discarded: %d", retained)
			}
			// Invalid complete snapshots roll back both incoming and retired rows.
			bad = mergeSnapshot(old)
			bad.Standings[0].PlayerName = ""
			if _, err := repo.UpsertStandingSnapshot(ctx, bad); err == nil {
				t.Fatal("invalid snapshot accepted")
			}
			if got := rankingForParticipationTest(t, repo, "overall"); len(got) != 1 || got[0].PlayerName != corrected.PlayerName {
				t.Fatalf("rollback: %+v", got)
			}
			// A genuinely complete empty result retires everything; a reappearing row reactivates.
			if _, err := repo.UpsertStandingSnapshot(ctx, domain.StandingSnapshot{Source: old.Source, TournamentID: old.TournamentID, Complete: true}); err != nil {
				t.Fatal(err)
			}
			if got := rankingForParticipationTest(t, repo, "overall"); len(got) != 0 {
				t.Fatalf("empty refresh: %+v", got)
			}
			if _, err := repo.UpsertStandingSnapshot(ctx, mergeSnapshot(old)); err != nil {
				t.Fatal(err)
			}
			if got := rankingForParticipationTest(t, repo, "overall"); len(got) != 1 || got[0].PlayerName != old.PlayerName {
				t.Fatalf("reactivation: %+v", got)
			}
		})
	}
}

func rankingForParticipationTest(t *testing.T, repo *Repository, period string) []domain.PlayerAggregate {
	t.Helper()
	var rows []domain.PlayerAggregate
	var err error
	switch period {
	case "overall":
		rows, err = repo.ListPlayerRanking(context.Background())
	case "year":
		rows, err = repo.ListPlayerRankingForYear(context.Background(), 2026)
	case "month":
		rows, err = repo.ListPlayerRankingForMonth(context.Background(), 2026, 9)
	}
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestZeroGamesDoNotContributeButUnknownAndOtherParticipationRemain(t *testing.T) {
	ctx := context.Background()
	repo, db := testRepoWithClock(t, &mutableRepositoryClock{now: time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)})
	played := mergeStanding("played", "played-result", "p", "Participant", 600, 3, 2)
	addMonthlyTournament(t, repo, "played", "2026-09-01T18:00:00Z", played)
	absent := mergeStanding("latest", "absent", "p", "Participant", 9900, 0, 99)
	neverPlayed := mergeStanding("latest", "never", "n", "Absent Only", 300, 0, 10)
	unknown := mergeStanding("latest", "unknown", "u", "Unknown Games", 200, 1, 1)
	unknown.GamesPlayed = nil
	addMonthlyTournament(t, repo, "latest", "2026-09-17T18:00:00Z", absent, neverPlayed, unknown)
	for _, period := range []string{"overall", "year", "month"} {
		rows := rankingForParticipationTest(t, repo, period)
		if len(rows) != 2 {
			t.Fatalf("%s: %+v", period, rows)
		}
		for _, row := range rows {
			switch row.PlayerName {
			case "Participant":
				if row.TournamentCount != 1 || *row.GamesPlayed != 3 || *row.TotalPointsCents != 600 || *row.GoalDifference != 2 || *row.PointsPerGameCents != 200 || row.PointsPerGameTrend != domain.MetricTrendSame {
					t.Fatalf("zero counted: %+v", row)
				}
			case "Unknown Games":
				if row.TournamentCount != 1 || row.GamesPlayed != nil || row.PointsPerGameCents != nil {
					t.Fatalf("unknown treated as zero: %+v", row)
				}
			default:
				t.Fatalf("unexpected row: %+v", row)
			}
		}
	}
	var player PlayerModel
	db.Where("canonical_name_key = ?", domain.PlayerKey("Absent Only")).First(&player)
	profile, err := repo.GetPlayerProfile(ctx, player.ID)
	if err != nil || profile.Aggregate.TournamentCount != 0 {
		t.Fatalf("profile=%+v err=%v", profile, err)
	}
	// Independent explicit manual contributions must still work for this identity.
	input := domain.ManualRankingCorrectionInput{PlayerID: player.ID, EffectiveDate: time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC), EffectiveYear: 2026, TournamentCountDelta: 1, GamesPlayedDelta: 2, PointsCentsDelta: 500, Reason: "manual", Administrator: "test"}
	preview, err := repo.PreviewManualRankingCorrection(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateManualRankingCorrection(ctx, input, preview.ExpectedVersion); err != nil {
		t.Fatal(err)
	}
	for _, period := range []string{"overall", "year", "month"} {
		rows := rankingForParticipationTest(t, repo, period)
		if len(rows) != 3 {
			t.Fatalf("manual contribution missing: %+v", rows)
		}
		for _, row := range rows {
			if row.PlayerName == "Absent Only" && (row.TournamentCount != 1 || *row.GamesPlayed != 2 || *row.TotalPointsCents != 500) {
				t.Fatalf("manual contribution contaminated: %+v", row)
			}
		}
	}
	// A zero-only newest tournament must not change the trend comparison boundary.
	zero := mergeStanding("zero-only", "zero-row", "p", "Participant", 0, 0, 0)
	addMonthlyTournament(t, repo, "zero-only", "2026-09-19T18:00:00Z", zero)
	qualified, err := repo.rankedQualifyingTournaments(ctx, nil)
	if err != nil || len(qualified) != 2 {
		t.Fatalf("zero-only tournament qualified: %+v %v", qualified, err)
	}
	// Changing explicit zero to played and back updates the materialized aggregate too.
	absent.GamesPlayed = intPointer(2)
	for _, games := range []int{2, 0} {
		absent.GamesPlayed = intPointer(games)
		if _, err := repo.UpsertStandingSnapshot(ctx, domain.StandingSnapshot{Source: absent.Source, TournamentID: "latest", Complete: true, Standings: []domain.TournamentStanding{absent, neverPlayed, unknown}}); err != nil {
			t.Fatal(err)
		}
		var aggregate PlayerAggregateModel
		db.Where("player_key = ?", domain.PlayerKey("Participant")).First(&aggregate)
		want := 1
		if games > 0 {
			want = 2
		}
		if aggregate.TournamentCount != want {
			t.Fatalf("aggregate count=%d want=%d", aggregate.TournamentCount, want)
		}
	}
}

func TestMergingCurrentResultIntoObsoleteIdentityKeepsCurrentValues(t *testing.T) {
	repo, db := testRepo(t)
	ctx := context.Background()
	old := mergeStanding("t", "old", "old-p", "Old Identity", 100, 1, 1)
	current := mergeStanding("t", "new", "new-p", "Current Identity", 700, 4, 8)
	if _, err := repo.UpsertMany(ctx, []domain.Tournament{tournament("t", "T")}); err != nil {
		t.Fatal(err)
	}
	for _, row := range []domain.TournamentStanding{old, current} {
		if _, err := repo.UpsertStandingSnapshot(ctx, mergeSnapshot(row)); err != nil {
			t.Fatal(err)
		}
	}
	var source, target PlayerModel
	db.Where("canonical_name_key = ?", domain.PlayerKey(current.PlayerName)).First(&source)
	db.Where("canonical_name_key = ?", domain.PlayerKey(old.PlayerName)).First(&target)
	result, err := repo.MergePlayers(ctx, source.ID, target.ID, domain.PlayerMergeOptions{Actor: "test"})
	if err != nil || result.TargetAfter.TournamentCount != 1 || *result.TargetAfter.TotalPointsCents != 700 {
		t.Fatalf("merge lost current row: %+v %v", result, err)
	}
	if _, err := repo.UpsertStandingSnapshot(ctx, mergeSnapshot(current)); err != nil {
		t.Fatal(err)
	}
	rows, _ := repo.ListPlayerRanking(ctx)
	if len(rows) != 1 || rows[0].PlayerName != old.PlayerName || *rows[0].TotalPointsCents != 700 {
		t.Fatalf("refresh lost manual merge: %+v", rows)
	}
}

func TestSupersededColumnMigrationPreservesLegacyRowsAndFingerprintShape(t *testing.T) {
	repo, db := testRepo(t)
	ctx := context.Background()
	if _, err := repo.UpsertMany(ctx, []domain.Tournament{tournament("legacy", "Legacy")}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertStandingSnapshot(ctx, mergeSnapshot(mergeStanding("legacy", "r", "p", "Legacy Player", 100, 1, 1))); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropColumn(&StandingModel{}, "superseded"); err != nil {
		t.Fatal(err)
	}
	migrated, err := New(db, repo.clock)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := migrated.ListPlayerRanking(ctx)
	if err != nil || len(rows) != 1 {
		t.Fatalf("migration hid legacy rows: %+v %v", rows, err)
	}
	encoded, err := json.Marshal(StandingModel{})
	if err != nil || strings.Contains(string(encoded), "Superseded") {
		t.Fatalf("legacy JSON changed: %s %v", encoded, err)
	}
}

func TestHistoricalIdentityCollisionArchivesAndRollsBackAtomically(t *testing.T) {
	for _, mode := range []string{"current-id-new-name", "historical-id-current-name"} {
		t.Run(mode, func(t *testing.T) {
			repo, db := testRepo(t)
			ctx := context.Background()
			if _, err := repo.UpsertMany(ctx, []domain.Tournament{tournament("t", "T")}); err != nil {
				t.Fatal(err)
			}
			old := mergeStanding("t", "id1", "p", "Earlier Name", 100, 2, 1)
			current := mergeStanding("t", "id2", "p", "Corrected Name", 200, 2, 2)
			for _, row := range []domain.TournamentStanding{old, current} {
				if _, err := repo.UpsertStandingSnapshot(ctx, mergeSnapshot(row)); err != nil {
					t.Fatal(err)
				}
			}
			var obsolete StandingModel
			if err := db.Where("superseded = ?", true).First(&obsolete).Error; err != nil {
				t.Fatal(err)
			}
			next := current
			if mode == "current-id-new-name" {
				next.PlayerName = old.PlayerName
			} else {
				next.StandingID, next.StandingKey = old.StandingID, old.StandingKey
			}
			bad := mergeSnapshot(next)
			bad.Standings = append(bad.Standings, mergeStanding("t", "zz-invalid", "x", "", 0, 1, 0))
			if _, err := repo.UpsertStandingSnapshot(ctx, bad); err == nil {
				t.Fatal("invalid snapshot accepted")
			}
			var count int64
			db.Model(&StandingArchiveModel{}).Count(&count)
			if count != 0 {
				t.Fatal("failed transaction retained archive")
			}
			rows, _ := repo.ListPlayerRanking(ctx)
			if len(rows) != 1 || rows[0].PlayerName != current.PlayerName {
				t.Fatalf("failed transaction altered current result: %+v", rows)
			}
			for attempt := 0; attempt < 2; attempt++ {
				if _, err := repo.UpsertStandingSnapshot(ctx, mergeSnapshot(next)); err != nil {
					t.Fatal(err)
				}
			}
			rows, _ = repo.ListPlayerRanking(ctx)
			if len(rows) != 1 || rows[0].PlayerName != next.PlayerName || *rows[0].TotalPointsCents != 200 {
				t.Fatalf("identity correction: %+v", rows)
			}
			var archives []StandingArchiveModel
			if err := db.Find(&archives).Error; err != nil || len(archives) != 1 {
				t.Fatalf("archives=%+v %v", archives, err)
			}
			var restored StandingModel
			if err := json.Unmarshal([]byte(archives[0].SnapshotJSON), &restored); err != nil {
				t.Fatal(err)
			}
			expectedJSON, err := json.Marshal(obsolete)
			if err != nil {
				t.Fatal(err)
			}
			if archives[0].SnapshotJSON != string(expectedJSON) || archives[0].OriginalRowID != obsolete.ID || archives[0].TournamentRef != obsolete.TournamentRef {
				t.Fatalf("archive lost source data: %+v", restored)
			}
			// Neither identity was merged or deleted; source IDs are only provenance.
			db.Model(&PlayerModel{}).Where("merged_into_player_id IS NULL").Count(&count)
			if count != 2 {
				t.Fatalf("implicit identity merge: %d", count)
			}
		})
	}
}
