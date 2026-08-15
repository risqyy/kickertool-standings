package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
	"github.com/rs/zerolog"
	"kickertool-ranking/internal/adapters"
	"kickertool-ranking/internal/adapters/gormrepo"
	"kickertool-ranking/internal/domain"
)

func main() {
	_ = godotenv.Load()
	dbPath := flag.String("db-path", valueOr(os.Getenv("DB_PATH"), "./data/tournaments.db"), "SQLite database path")
	sourceName := flag.String("source-name", "", "source player name or alias")
	targetName := flag.String("target-name", "", "target player name or alias")
	sourceID := flag.Uint("source-player-id", 0, "optional source player ID")
	targetID := flag.Uint("target-player-id", 0, "optional target player ID")
	dryRun := flag.Bool("dry-run", false, "plan the merge and roll back all writes")
	actor := flag.String("actor", "", "optional audit actor")
	reason := flag.String("reason", "", "optional audit reason")
	flag.Parse()
	logger := zerolog.New(os.Stdout).With().Timestamp().Str("service", "kickertool-player-merge").Logger()
	if strings.TrimSpace(*sourceName) == "" || strings.TrimSpace(*targetName) == "" {
		logger.Error().Msg("--source-name and --target-name are required")
		os.Exit(2)
	}
	if *dbPath == "" {
		logger.Error().Msg("--db-path must not be empty")
		os.Exit(2)
	}
	if dir := filepath.Dir(*dbPath); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			logger.Error().Err(err).Msg("create database directory")
			os.Exit(1)
		}
	}
	repo, db, err := gormrepo.OpenSQLite(*dbPath, adapters.SystemClock{})
	if err != nil {
		logger.Error().Err(err).Msg("open database")
		os.Exit(1)
	}
	sqlDB, err := db.DB()
	if err != nil {
		logger.Error().Err(err).Msg("access database")
		os.Exit(1)
	}
	defer sqlDB.Close()
	ctx := context.Background()
	resolvedSource, err := resolve(repo, ctx, *sourceName, *sourceID)
	if err != nil {
		logger.Error().Err(err).Str("role", "source").Msg("resolve player")
		os.Exit(2)
	}
	resolvedTarget, err := resolve(repo, ctx, *targetName, *targetID)
	if err != nil {
		logger.Error().Err(err).Str("role", "target").Msg("resolve player")
		os.Exit(2)
	}
	if resolvedSource.ID == resolvedTarget.ID {
		logger.Error().Msg("source and target must resolve to different active players")
		os.Exit(2)
	}
	result, err := repo.MergePlayers(ctx, resolvedSource.ID, resolvedTarget.ID, domain.PlayerMergeOptions{DryRun: *dryRun, Actor: *actor, Reason: *reason})
	if err != nil {
		logger.Error().Err(err).Uint("source_player_id", resolvedSource.ID).Uint("target_player_id", resolvedTarget.ID).Msg("player merge failed")
		os.Exit(1)
	}
	logger.Info().Uint("source_player_id", result.SourcePlayerID).Uint("target_player_id", result.TargetPlayerID).Bool("dry_run", result.DryRun).Bool("already_merged", result.AlreadyMerged).Int("transferred_aliases", result.TransferredAliases).Int("transferred_source_identities", result.TransferredSourceIdentities).Int("transferred_allocations", result.TransferredAllocations).Int("deduplicated_allocations", result.DeduplicatedAllocations).Msg("player merge completed")
}

func resolve(repo *gormrepo.Repository, ctx context.Context, name string, playerID uint) (domain.PlayerProfile, error) {
	nameKey := domain.PlayerKey(name)
	if playerID != 0 {
		profile, err := repo.GetPlayerProfile(ctx, playerID)
		if err != nil {
			return domain.PlayerProfile{}, err
		}
		for _, alias := range profile.Aliases {
			if alias.NameKey == nameKey {
				return profile, nil
			}
		}
		return domain.PlayerProfile{}, fmt.Errorf("player %d does not have alias %q", playerID, nameKey)
	}
	profiles, err := repo.SearchPlayers(ctx, name)
	if err != nil {
		return domain.PlayerProfile{}, err
	}
	matches := make([]domain.PlayerProfile, 0, len(profiles))
	for _, profile := range profiles {
		for _, alias := range profile.Aliases {
			if alias.NameKey == nameKey {
				matches = append(matches, profile)
				break
			}
		}
	}
	if len(matches) == 0 {
		return domain.PlayerProfile{}, fmt.Errorf("no player matches normalized name %q", nameKey)
	}
	if len(matches) > 1 {
		return domain.PlayerProfile{}, fmt.Errorf("ambiguous normalized name %q", nameKey)
	}
	if !matches[0].Active {
		return domain.PlayerProfile{}, errors.New("selected player is already merged; select its active canonical target")
	}
	return matches[0], nil
}

func valueOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
