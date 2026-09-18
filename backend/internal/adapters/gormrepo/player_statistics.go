package gormrepo

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
	"kickertool-ranking/internal/domain"
	"kickertool-ranking/internal/ports"
)

func playerSummary(player PlayerModel) domain.PlayerSummary {
	return domain.PlayerSummary{ID: player.ID, DisplayName: player.DisplayName, Active: player.MergedIntoPlayerID == nil, MergedIntoPlayerID: player.MergedIntoPlayerID}
}

func (r *Repository) ListPlayers(ctx context.Context, filter domain.PlayerListFilter) (page domain.PlayerPage, err error) {
	page.Page, page.Limit = filter.Page, filter.Limit
	if page.Page < 1 {
		page.Page = 1
	}
	if page.Limit < 1 {
		page.Limit = 25
	}
	if page.Limit > 100 {
		page.Limit = 100
	}
	page.Items = []domain.PlayerSummary{}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Model(&PlayerModel{})
		if search := domain.PlayerKey(filter.Query); search != "" {
			search = "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(search) + "%"
			aliases := tx.Model(&PlayerNameAliasModel{}).Select("player_id").Where(`name_key LIKE ? ESCAPE '\'`, search)
			query = query.Where(`canonical_name_key LIKE ? ESCAPE '\' OR id IN (?)`, search, aliases)
		}
		switch filter.State {
		case "active":
			query = query.Where("merged_into_player_id IS NULL")
		case "merged":
			query = query.Where("merged_into_player_id IS NOT NULL")
		}
		if err := query.Count(&page.Total).Error; err != nil {
			return err
		}
		var players []PlayerModel
		if err := query.Order("canonical_name_key ASC").Order("id ASC").Offset((page.Page - 1) * page.Limit).Limit(page.Limit).Find(&players).Error; err != nil {
			return err
		}
		for _, player := range players {
			page.Items = append(page.Items, playerSummary(player))
		}
		return nil
	})
	return page, err
}

// Keep totals, effective corrections and explanatory rows on one read snapshot
// and one time boundary, even when a refresh commits concurrently.
type statisticsClock struct {
	ports.Clock
	now time.Time
}

func (c statisticsClock) Now() time.Time { return c.now }

func (r *Repository) GetPlayerStatistics(ctx context.Context, id uint) (result domain.PlayerStatistics, err error) {
	result.ComputedAt = r.clock.Now()
	result.Tournaments = []domain.PlayerTournamentContribution{}
	result.Corrections = []domain.ManualRankingCorrection{}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var requested PlayerModel
		if err := tx.First(&requested, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ports.ErrNotFound
			}
			return err
		}
		root, err := resolvePlayerRoot(tx, id)
		if err != nil {
			return err
		}
		result.RequestedPlayer = playerSummary(requested)
		snapshotRepo := &Repository{db: tx, clock: statisticsClock{Clock: r.clock, now: result.ComputedAt}}
		result.Player, err = snapshotRepo.profileForRoot(ctx, requested.ID, requested, root)
		if err != nil {
			return err
		}
		var rows []StandingModel
		if err := tx.Where("player_ref = ?", root.ID).Preload("Tournament").Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			tournament := row.Tournament
			sourceID := tournament.SourceKey
			if tournament.SourceID != nil {
				sourceID = *tournament.SourceID
			}
			name := tournament.Name
			if strings.TrimSpace(name) == "" {
				name = fmt.Sprintf("Turnier #%d", row.TournamentRef)
			}
			reason := "counted"
			switch {
			case row.Superseded:
				reason = "superseded"
			case !tournament.IncludedInRanking:
				reason = "excluded"
			case row.GamesPlayed != nil && *row.GamesPlayed == 0:
				reason = "zero_games"
			}
			url := row.URL
			if strings.TrimSpace(url) == "" {
				url = tournament.URL
			}
			item := domain.PlayerTournamentContribution{
				ID: row.ID, TournamentID: row.TournamentRef, Name: name, Date: tournament.Date,
				Source: row.Source, SourceID: sourceID, URL: url, Status: tournament.Status, Reason: reason,
				StandingRank: row.Rank, StandingSourceID: row.SourceStandingID, StandingKey: row.StandingKey, SourcePlayerName: row.PlayerName,
				GamesPlayed: row.GamesPlayed, TotalPointsCents: row.PointsCents, GoalDifference: row.GoalDifference,
			}
			if row.GamesPlayed != nil && *row.GamesPlayed > 0 && row.PointsCents != nil {
				ppg := roundCents(*row.PointsCents, int64(*row.GamesPlayed))
				item.PointsPerGameCents = &ppg
			}
			result.Tournaments = append(result.Tournaments, item)
		}
		sort.Slice(result.Tournaments, func(i, j int) bool {
			a, b := result.Tournaments[i], result.Tournaments[j]
			if a.Date != nil && b.Date != nil && !a.Date.Equal(*b.Date) {
				return a.Date.After(*b.Date)
			}
			if (a.Date != nil) != (b.Date != nil) {
				return a.Date != nil
			}
			if a.TournamentID != b.TournamentID {
				return a.TournamentID > b.TournamentID
			}
			return a.ID > b.ID
		})
		var corrections []ManualRankingCorrectionModel
		if err := tx.Where("player_ref = ?", root.ID).Order("effective_date DESC").Order("id DESC").Find(&corrections).Error; err != nil {
			return err
		}
		for _, correction := range corrections {
			result.Corrections = append(result.Corrections, fromManualCorrectionModel(correction))
		}
		return nil
	})
	return result, err
}
