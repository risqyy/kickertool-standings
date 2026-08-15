package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/rs/zerolog"
	"kickertool-ranking/internal/domain"
	"kickertool-ranking/internal/ports"
)

type CrawlerService struct {
	source            ports.TournamentSource
	repo              ports.TournamentRepository
	standingSource    ports.TournamentStandingSource
	standingRepo      ports.StandingRepository
	standingStateRepo ports.StandingSyncStateRepository
	clock             ports.Clock
	logger            *zerolog.Logger
}

type CrawlerOption func(*CrawlerService)

func WithStandings(source ports.TournamentStandingSource, repo ports.StandingRepository, stateRepo ...ports.StandingSyncStateRepository) CrawlerOption {
	return func(c *CrawlerService) {
		c.standingSource = source
		c.standingRepo = repo
		if len(stateRepo) > 0 {
			c.standingStateRepo = stateRepo[0]
		} else if candidate, ok := repo.(ports.StandingSyncStateRepository); ok {
			c.standingStateRepo = candidate
		}
	}
}

func NewCrawler(source ports.TournamentSource, repo ports.TournamentRepository, clock ports.Clock, logger *zerolog.Logger, options ...CrawlerOption) *CrawlerService {
	crawler := &CrawlerService{source: source, repo: repo, clock: clock, logger: logger}
	for _, option := range options {
		option(crawler)
	}
	return crawler
}

func (c *CrawlerService) Crawl(ctx context.Context) (result domain.SyncResult, err error) {
	result.StartedAt = c.clock.Now()
	sourceName := "crawler"
	if named, ok := c.source.(ports.NamedTournamentSource); ok {
		sourceName = named.SourceName()
	}
	if c.logger != nil {
		c.logger.Info().Str("source", sourceName).Time("start", result.StartedAt).Msg("crawl started")
	}
	defer func() {
		result.FinishedAt = c.clock.Now()
		if c.logger != nil {
			e := c.logger.Info()
			if err != nil {
				e = c.logger.Error().Err(err)
			}
			e.Str("source", sourceName).
				Time("start", result.StartedAt).Time("end", result.FinishedAt).
				Dur("duration", result.Duration()).Int("found", result.Found).
				Int("inserted", result.Inserted).Int("updated", result.Updated).
				Int("unchanged", result.Unchanged).Int("invalid", result.Invalid).
				Int("tournaments_processed", result.TournamentsProcessed).
				Int("tournaments_succeeded", result.TournamentsSucceeded).
				Int("tournaments_failed", result.TournamentsFailed).
				Int("tournaments_skipped", result.TournamentsSkipped).
				Int("standings_found", result.StandingsFound).
				Int("players_inserted", result.PlayersInserted).
				Int("players_updated", result.PlayersUpdated).
				Int("standings_inserted", result.StandingsInserted).
				Int("standings_updated", result.StandingsUpdated).
				Int("standings_unchanged", result.StandingsUnchanged).
				Int("aggregates_recalculated", result.AggregatesRecalculated)
			if err != nil {
				e.Msg("crawl failed")
			} else {
				e.Msg("crawl finished")
			}
		}
	}()

	fetched, fetchErr := c.source.FetchTournaments(ctx)
	if fetchErr != nil {
		return result, fmt.Errorf("fetch tournaments: %w", fetchErr)
	}
	result.Found = len(fetched)
	valid := make([]domain.Tournament, 0, len(fetched))
	for _, t := range fetched {
		t = normalize(t)
		if validationErr := validate(t); validationErr != nil {
			result.Invalid++
			if c.logger != nil {
				c.logger.Warn().Err(validationErr).Str("source", t.Source).Str("source_id", t.SourceID).Msg("invalid tournament skipped")
			}
			continue
		}
		valid = append(valid, t)
	}
	persisted, persistErr := c.repo.UpsertMany(ctx, valid)
	if persistErr != nil {
		return result, fmt.Errorf("persist tournaments: %w", persistErr)
	}
	result.Inserted, result.Updated, result.Unchanged = persisted.Inserted, persisted.Updated, persisted.Unchanged
	if c.standingSource != nil && c.standingRepo != nil {
		for _, tournament := range valid {
			result.TournamentsProcessed++
			state := tournament
			if tournament.SourceID != "" {
				persisted, findErr := c.repo.FindBySourceID(ctx, tournament.Source, tournament.SourceID)
				if findErr == nil {
					state = persisted
				} else if !errors.Is(findErr, ports.ErrNotFound) {
					result.TournamentsFailed++
					if c.logger != nil {
						c.logger.Error().Err(findErr).Str("source", tournament.Source).Str("tournament_id", tournament.SourceID).Str("tournament_name", tournament.Name).Msg("load standings sync state failed")
					}
					continue
				}
			}
			decision := ShouldSyncStandings(state, c.clock.Now())
			if !decision.ShouldSync {
				result.TournamentsSkipped++
				if c.logger != nil {
					c.logger.Debug().Str("source", tournament.Source).Str("tournament_id", tournament.SourceID).Str("tournament_name", tournament.Name).Str("reason", decision.Reason).Msg("standings skipped")
				}
				continue
			}
			if c.logger != nil {
				c.logger.Info().
					Str("source", tournament.Source).
					Str("tournament_id", tournament.SourceID).
					Str("tournament_name", tournament.Name).
					Str("status", tournament.Status).
					Msg("tournament standings processing started")
			}
			started := c.clock.Now()
			snapshot, standingsErr := c.standingSource.FetchStandings(ctx, tournament)
			if standingsErr != nil {
				result.TournamentsFailed++
				c.markStandingFailure(ctx, tournament)
				if c.logger != nil {
					c.logger.Error().Err(standingsErr).Str("source", tournament.Source).Str("tournament_id", tournament.SourceID).Str("tournament_name", tournament.Name).Msg("tournament standings failed")
				}
				continue
			}
			standingResult, persistStandingErr := c.standingRepo.UpsertStandingSnapshot(ctx, snapshot)
			if persistStandingErr != nil {
				result.TournamentsFailed++
				c.markStandingFailure(ctx, tournament)
				if c.logger != nil {
					c.logger.Error().Err(persistStandingErr).Str("source", tournament.Source).Str("tournament_id", tournament.SourceID).Str("tournament_name", tournament.Name).Msg("tournament standings persistence failed")
				}
				continue
			}
			result.TournamentsSucceeded++
			result.StandingsFound += standingResult.Found
			result.PlayersInserted += standingResult.PlayersInserted
			result.PlayersUpdated += standingResult.PlayersUpdated
			result.StandingsInserted += standingResult.StandingsInserted
			result.StandingsUpdated += standingResult.StandingsUpdated
			result.StandingsUnchanged += standingResult.StandingsUnchanged
			result.AggregatesRecalculated += standingResult.AggregatesRecalculated
			if c.logger != nil {
				c.logger.Info().Str("source", tournament.Source).Str("tournament_id", tournament.SourceID).Str("tournament_name", tournament.Name).
					Dur("duration", c.clock.Now().Sub(started)).Int("standings_found", standingResult.Found).
					Int("players_inserted", standingResult.PlayersInserted).Int("players_updated", standingResult.PlayersUpdated).
					Int("standings_inserted", standingResult.StandingsInserted).Int("standings_updated", standingResult.StandingsUpdated).
					Int("standings_unchanged", standingResult.StandingsUnchanged).Int("aggregates_recalculated", standingResult.AggregatesRecalculated).
					Msg("tournament standings processed")
			}
		}
	}
	return result, nil
}

func (c *CrawlerService) markStandingFailure(ctx context.Context, tournament domain.Tournament) {
	if c.standingStateRepo == nil || tournament.SourceID == "" {
		return
	}
	if err := c.standingStateRepo.MarkStandingSyncFailed(ctx, tournament.Source, tournament.SourceID); err != nil && c.logger != nil {
		c.logger.Error().Err(err).Str("source", tournament.Source).Str("tournament_id", tournament.SourceID).Str("tournament_name", tournament.Name).Msg("persist standings sync failure state failed")
	}
}

func validate(t domain.Tournament) error {
	if strings.TrimSpace(t.Source) == "" {
		return fmt.Errorf("source is required")
	}
	if strings.TrimSpace(t.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if strings.TrimSpace(t.URL) == "" {
		return fmt.Errorf("url is required")
	}
	if strings.TrimSpace(t.SourceID) == "" && strings.TrimSpace(t.SourceKey) == "" {
		return fmt.Errorf("source id or source key is required")
	}
	if t.Participants != nil && *t.Participants < 0 {
		return fmt.Errorf("participants cannot be negative")
	}
	return nil
}

func normalize(t domain.Tournament) domain.Tournament {
	t.Source = strings.TrimSpace(t.Source)
	t.SourceID = strings.TrimSpace(t.SourceID)
	t.SourceKey = strings.TrimSpace(t.SourceKey)
	t.Name = strings.Join(strings.Fields(t.Name), " ")
	t.Venue = strings.Join(strings.Fields(t.Venue), " ")
	t.City = strings.Join(strings.Fields(t.City), " ")
	t.Country = strings.Join(strings.Fields(t.Country), " ")
	t.Organizer = strings.Join(strings.Fields(t.Organizer), " ")
	t.Status = strings.Join(strings.Fields(t.Status), " ")
	t.URL = strings.TrimSpace(t.URL)
	if t.SourceKey == "" && t.SourceID == "" && t.URL != "" {
		sum := sha256.Sum256([]byte(strings.TrimRight(t.URL, "/")))
		t.SourceKey = "sha256:" + hex.EncodeToString(sum[:])
	}
	return t
}
