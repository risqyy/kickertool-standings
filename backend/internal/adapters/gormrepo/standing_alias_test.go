package gormrepo

import (
	"context"
	"fmt"
	"kickertool-ranking/internal/domain"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestMergedAliasCannotReplacePlayedResultWithZeroGames(t *testing.T) {
	for _, reverseMerge := range []bool{false, true} {
		for _, reverseKeys := range []bool{false, true} {
			t.Run(fmt.Sprintf("reverseMerge=%t/reverseKeys=%t", reverseMerge, reverseKeys), func(t *testing.T) {
				ctx := context.Background()
				repo, db := testRepoWithClock(t, &mutableRepositoryClock{now: time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)})
				played := mergeStanding("alias-cup", "a-played", "played-player", "Full Name", 2400, 23, 0)
				played.Rank = intPointer(6)
				zero := mergeStanding("alias-cup", "z-zero", "absent-player", "Short Name", 0, 0, 0)
				zero.Rank = intPointer(23)
				if reverseKeys {
					played.StandingID, played.StandingKey = "z-played", "z-played"
					zero.StandingID, zero.StandingKey = "a-zero", "a-zero"
				}
				addMonthlyTournament(t, repo, "alias-cup", "2026-09-10T18:00:00Z", played, zero)
				var full, short PlayerModel
				if err := db.Where("canonical_name_key = ?", domain.PlayerKey(played.PlayerName)).First(&full).Error; err != nil {
					t.Fatal(err)
				}
				if err := db.Where("canonical_name_key = ?", domain.PlayerKey(zero.PlayerName)).First(&short).Error; err != nil {
					t.Fatal(err)
				}
				source, target := short, full
				if reverseMerge {
					source, target = full, short
				}
				if _, err := repo.MergePlayers(ctx, source.ID, target.ID, domain.PlayerMergeOptions{Actor: "test"}); err != nil {
					t.Fatal(err)
				}
				check := func() {
					t.Helper()
					for _, period := range []string{"overall", "year", "month"} {
						rows := rankingForParticipationTest(t, repo, period)
						if len(rows) != 1 || rows[0].TournamentCount != 1 || rows[0].GamesPlayed == nil || *rows[0].GamesPlayed != 23 || *rows[0].TotalPointsCents != 2400 {
							t.Fatalf("%s wrong aggregate: %+v", period, rows)
						}
					}
					var row StandingModel
					if err := db.Where("player_ref = ?", target.ID).First(&row).Error; err != nil {
						t.Fatal(err)
					}
					if row.PlayerName != played.PlayerName || row.Rank == nil || *row.Rank != 6 || row.StandingKey != played.StandingKey {
						t.Fatalf("wrong source row: %+v", row)
					}
				}
				check()
				snapshot := domain.StandingSnapshot{Source: played.Source, TournamentID: played.TournamentID, Complete: true, Standings: []domain.TournamentStanding{played, zero}}
				for attempt := 0; attempt < 3; attempt++ {
					snapshot.Standings[0], snapshot.Standings[1] = snapshot.Standings[1], snapshot.Standings[0]
					result, err := repo.UpsertStandingSnapshot(ctx, snapshot)
					if err != nil {
						t.Fatal(err)
					}
					if result.StandingsUpdated != 0 || result.StandingsUnchanged != 1 {
						t.Fatalf("unchanged refresh rewrote selected row: %+v", result)
					}
					check()
				}
				snapshot.Complete = false
				if _, err := repo.UpsertStandingSnapshot(ctx, snapshot); err == nil {
					t.Fatal("incomplete snapshot accepted")
				}
				check()
				snapshot.Complete = true
				invalid := zero
				invalid.PlayerName = ""
				snapshot.Standings = []domain.TournamentStanding{played, invalid}
				if _, err := repo.UpsertStandingSnapshot(ctx, snapshot); err == nil {
					t.Fatal("invalid losing alias accepted")
				}
				check()
				// A real later correction still removes participation; never retain stale games.
				if _, err := repo.UpsertStandingSnapshot(ctx, mergeSnapshot(zero)); err != nil {
					t.Fatal(err)
				}
				if rows := rankingForParticipationTest(t, repo, "overall"); len(rows) != 0 {
					t.Fatalf("zero-only refresh retained games: %+v", rows)
				}
				snapshot.Standings = []domain.TournamentStanding{zero, played}
				if _, err := repo.UpsertStandingSnapshot(ctx, snapshot); err != nil {
					t.Fatal(err)
				}
				check()
			})
		}
	}
}

func TestLosingAliasIdentityConflictRollsBackCompleteSnapshot(t *testing.T) {
	for _, mode := range []string{"foreign-id", "current-id", "foreign-key", "current-key"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			repo, db := testRepo(t)
			played := mergeStanding("cup", "played", "p1", "Full Name", 2400, 23, 0)
			zero := mergeStanding("cup", "zero", "p2", "Short Name", 0, 0, 0)
			other := mergeStanding("cup", "other-result", "p3", "Other Player", 500, 5, 2)
			addMonthlyTournament(t, repo, "cup", "2026-09-10T18:00:00Z", played, zero, other)
			if strings.HasPrefix(mode, "foreign") {
				other.TournamentID, other.StandingID, other.StandingKey = "other-cup", "foreign-result", "foreign-result"
				addMonthlyTournament(t, repo, "other-cup", "2026-09-11T18:00:00Z", other)
			}
			var full, short PlayerModel
			if err := db.Where("canonical_name_key = ?", domain.PlayerKey(played.PlayerName)).First(&full).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Where("canonical_name_key = ?", domain.PlayerKey(zero.PlayerName)).First(&short).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := repo.MergePlayers(ctx, short.ID, full.ID, domain.PlayerMergeOptions{Actor: "test"}); err != nil {
				t.Fatal(err)
			}
			readState := func() map[string][]map[string]any {
				t.Helper()
				state := make(map[string][]map[string]any)
				for _, table := range []string{"standing_models", "standing_archive_models", "player_models", "source_player_identity_models", "player_name_alias_models", "player_aggregate_models", "tournament_models", "allocation_models"} {
					var rows []map[string]any
					if err := db.Table(table).Order("id").Find(&rows).Error; err != nil {
						t.Fatal(err)
					}
					state[table] = rows
				}
				return state
			}
			before := readState()
			changed := played
			changed.PointsCents = int64Pointer(9900)
			zero.StandingID = other.StandingID
			zero.StandingKey = other.StandingKey
			if strings.HasSuffix(mode, "key") {
				zero.StandingID = "unclaimed-source-id"
			}
			_, err := repo.UpsertStandingSnapshot(ctx, domain.StandingSnapshot{Source: played.Source, TournamentID: played.TournamentID, Complete: true, Standings: []domain.TournamentStanding{changed, zero}})
			if err == nil || !strings.Contains(err.Error(), "ambiguous standing identity") {
				t.Fatalf("invalid losing alias accepted: %v", err)
			}
			if after := readState(); !reflect.DeepEqual(before, after) {
				t.Fatal("invalid snapshot changed stored results or identity state")
			}
		})
	}
}

func TestMergedAliasUnknownGamesAreNotTreatedAsZero(t *testing.T) {
	ctx := context.Background()
	repo, db := testRepo(t)
	unknown := mergeStanding("unknown-alias", "a-unknown", "p1", "Unknown Games", 400, 1, 2)
	unknown.GamesPlayed = nil
	zero := mergeStanding("unknown-alias", "z-zero", "p2", "Absent Alias", 0, 0, 0)
	addMonthlyTournament(t, repo, "unknown-alias", "2026-09-10T18:00:00Z", unknown, zero)
	var source, target PlayerModel
	db.Where("canonical_name_key = ?", domain.PlayerKey(unknown.PlayerName)).First(&source)
	db.Where("canonical_name_key = ?", domain.PlayerKey(zero.PlayerName)).First(&target)
	if _, err := repo.MergePlayers(ctx, source.ID, target.ID, domain.PlayerMergeOptions{Actor: "test"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertStandingSnapshot(ctx, domain.StandingSnapshot{Source: zero.Source, TournamentID: zero.TournamentID, Complete: true, Standings: []domain.TournamentStanding{unknown, zero}}); err != nil {
		t.Fatal(err)
	}
	p, err := repo.GetPlayerProfile(ctx, target.ID)
	if err != nil || p.Aggregate.TournamentCount != 1 || p.Aggregate.GamesPlayed != nil || p.Aggregate.TotalPointsCents == nil || *p.Aggregate.TotalPointsCents != 400 {
		t.Fatalf("unknown collapsed to zero: %+v %v", p, err)
	}
}

func TestMergedPlayedAliasesKeepCanonicalResultAndCanUndo(t *testing.T) {
	ctx := context.Background()
	repo, db := testRepo(t)
	alias := mergeStanding("played-alias", "z-alias", "p1", "Old Name", 9000, 30, 20)
	canonical := mergeStanding("played-alias", "a-canonical", "p2", "Current Name", 400, 4, 2)
	addMonthlyTournament(t, repo, "played-alias", "2026-09-10T18:00:00Z", alias, canonical)
	var source, target PlayerModel
	if err := db.Where("canonical_name_key = ?", domain.PlayerKey(alias.PlayerName)).First(&source).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("canonical_name_key = ?", domain.PlayerKey(canonical.PlayerName)).First(&target).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repo.MergePlayers(ctx, source.ID, target.ID, domain.PlayerMergeOptions{Actor: "test"}); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if _, err := repo.UpsertStandingSnapshot(ctx, domain.StandingSnapshot{Source: alias.Source, TournamentID: alias.TournamentID, Complete: true, Standings: []domain.TournamentStanding{canonical, alias}}); err != nil {
			t.Fatal(err)
		}
		profile, err := repo.GetPlayerProfile(ctx, target.ID)
		if err != nil || *profile.Aggregate.GamesPlayed != 4 || *profile.Aggregate.TotalPointsCents != 400 {
			t.Fatalf("duplicate played aliases summed or selected by magnitude: %+v %v", profile, err)
		}
	}
	// Refresh touches timestamps, so use a fresh merge to exercise zero-game
	// replacement and its undo snapshot without unrelated concurrent changes.
	if _, err := repo.UpsertStandingSnapshot(ctx, mergeSnapshot(canonical)); err != nil {
		t.Fatal(err)
	}
	zero := mergeStanding("played-alias", "empty-target", "p3", "Empty Target", 0, 0, 0)
	if _, err := repo.UpsertStandingSnapshot(ctx, domain.StandingSnapshot{Source: zero.Source, TournamentID: zero.TournamentID, Complete: true, Standings: []domain.TournamentStanding{canonical, zero}}); err != nil {
		t.Fatal(err)
	}
	var empty PlayerModel
	if err := db.Where("canonical_name_key = ?", domain.PlayerKey(zero.PlayerName)).First(&empty).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repo.MergePlayers(ctx, target.ID, empty.ID, domain.PlayerMergeOptions{Actor: "test"}); err != nil {
		t.Fatal(err)
	}
	merges, err := repo.ListPlayerMerges(ctx)
	if err != nil || len(merges) != 2 {
		t.Fatalf("merges=%+v %v", merges, err)
	}
	preview, err := repo.PreviewPlayerMergeUndo(ctx, merges[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UndoPlayerMerge(ctx, merges[0].ID, domain.PlayerMergeUndoOptions{Actor: "test", ExpectedFingerprint: preview.StateFingerprint}); err != nil {
		t.Fatal(err)
	}
	profile, err := repo.GetPlayerProfile(ctx, target.ID)
	if err != nil || *profile.Aggregate.GamesPlayed != 4 {
		t.Fatalf("undo lost played result: %+v %v", profile, err)
	}
	profile, err = repo.GetPlayerProfile(ctx, empty.ID)
	if err != nil || profile.Aggregate.TournamentCount != 0 {
		t.Fatalf("undo gave empty target a contribution: %+v %v", profile, err)
	}
}
