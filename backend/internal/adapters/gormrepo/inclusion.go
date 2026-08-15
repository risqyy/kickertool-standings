package gormrepo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"kickertool-ranking/internal/domain"
	"kickertool-ranking/internal/ports"
)

func (r *Repository) ListTournaments(ctx context.Context, filter domain.TournamentListFilter) (domain.TournamentPage, error) {
	page := filter.Page
	if page < 1 {
		page = 1
	}
	limit := filter.Limit
	if limit < 1 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}
	db := r.db.WithContext(ctx).Model(&TournamentModel{})
	if query := strings.TrimSpace(filter.Query); query != "" {
		pattern := "%" + query + "%"
		db = db.Where("name LIKE ? OR source_id LIKE ? OR source_key LIKE ?", pattern, pattern, pattern)
	}
	if filter.Included != nil {
		db = db.Where("included_in_ranking = ?", *filter.Included)
	}
	if state := strings.TrimSpace(filter.State); state != "" {
		db = db.Where("status = ?", state)
	}
	if source := strings.TrimSpace(filter.Source); source != "" {
		db = db.Where("source = ?", source)
	}
	if filter.DateFrom != nil {
		db = db.Where("date >= ?", *filter.DateFrom)
	}
	if filter.DateTo != nil {
		db = db.Where("date <= ?", *filter.DateTo)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return domain.TournamentPage{}, fmt.Errorf("count tournaments: %w", err)
	}
	order := map[string]string{
		"name": "name", "date": "date", "source": "source", "status": "status",
		"included": "included_in_ranking", "updated": "updated_at", "participants": "participants",
	}
	column := order[strings.ToLower(strings.TrimSpace(filter.Sort))]
	if column == "" {
		column = "date"
	}
	direction := "ASC"
	if filter.Desc {
		direction = "DESC"
	}
	var models []TournamentModel
	if err := db.Order(column + " " + direction).Order("id ASC").Offset((page - 1) * limit).Limit(limit).Find(&models).Error; err != nil {
		return domain.TournamentPage{}, fmt.Errorf("list tournaments: %w", err)
	}
	items := make([]domain.TournamentAdminRow, 0, len(models))
	for _, model := range models {
		row, err := r.adminRow(ctx, model)
		if err != nil {
			return domain.TournamentPage{}, err
		}
		items = append(items, row)
	}
	var lastSync *time.Time
	var latest time.Time
	if err := r.db.WithContext(ctx).Model(&TournamentModel{}).Select("MAX(standings_synced_at)").Scan(&latest).Error; err == nil && !latest.IsZero() {
		lastSync = &latest
	}
	return domain.TournamentPage{Items: items, Page: page, Limit: limit, Total: total, LastSyncAt: lastSync}, nil
}

func (r *Repository) adminRow(ctx context.Context, model TournamentModel) (domain.TournamentAdminRow, error) {
	var standings int64
	var players int64
	db := r.db.WithContext(ctx).Model(&StandingModel{}).Where("tournament_ref = ?", model.ID)
	if err := db.Count(&standings).Error; err != nil {
		return domain.TournamentAdminRow{}, fmt.Errorf("count tournament standings %d: %w", model.ID, err)
	}
	if err := db.Distinct("player_ref").Count(&players).Error; err != nil {
		return domain.TournamentAdminRow{}, fmt.Errorf("count tournament players %d: %w", model.ID, err)
	}
	return domain.TournamentAdminRow{Tournament: fromModel(model), StandingCount: int(standings), PlayerCount: int(players), StandingsComplete: model.StandingsSyncComplete, LastSyncError: model.LastStandingsSyncFailed, InclusionVersion: maxVersion(model.InclusionVersion)}, nil
}

func (r *Repository) GetDashboard(ctx context.Context) (domain.Dashboard, error) {
	var result domain.Dashboard
	if err := r.db.WithContext(ctx).Model(&TournamentModel{}).Count(&result.TournamentCount).Error; err != nil {
		return result, err
	}
	if err := r.db.WithContext(ctx).Model(&TournamentModel{}).Where("included_in_ranking = ?", true).Count(&result.IncludedTournamentCount).Error; err != nil {
		return result, err
	}
	result.ExcludedTournamentCount = result.TournamentCount - result.IncludedTournamentCount
	if err := r.db.WithContext(ctx).Model(&PlayerModel{}).Where("merged_into_player_id IS NULL").Count(&result.PlayerCount).Error; err != nil {
		return result, err
	}
	var latest time.Time
	if err := r.db.WithContext(ctx).Model(&TournamentModel{}).Select("MAX(standings_synced_at)").Scan(&latest).Error; err == nil && !latest.IsZero() {
		result.LastSyncAt = &latest
	}
	return result, nil
}

func (r *Repository) LastSyncAt(ctx context.Context) (*time.Time, error) {
	var latest time.Time
	if err := r.db.WithContext(ctx).Model(&TournamentModel{}).Select("MAX(standings_synced_at)").Scan(&latest).Error; err != nil {
		return nil, err
	}
	if latest.IsZero() {
		return nil, nil
	}
	return &latest, nil
}

func (r *Repository) SetTournamentRankingInclusion(ctx context.Context, tournamentID uint, included bool, expectedVersion int64, reason string) (result domain.TournamentInclusionChange, err error) {
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var tournament TournamentModel
		if findErr := tx.First(&tournament, tournamentID).Error; findErr != nil {
			if errors.Is(findErr, gorm.ErrRecordNotFound) {
				return fmt.Errorf("tournament %d: %w", tournamentID, ports.ErrNotFound)
			}
			return findErr
		}
		currentVersion := maxVersion(tournament.InclusionVersion)
		if expectedVersion != currentVersion {
			return fmt.Errorf("tournament %d: %w (expected %d, current %d)", tournamentID, ports.ErrVersionConflict, expectedVersion, currentVersion)
		}
		previous := tournament.IncludedInRanking
		newVersion := currentVersion
		if previous != included {
			newVersion++
			now := r.clock.Now()
			if err := tx.Model(&tournament).Updates(map[string]any{
				"included_in_ranking": included, "inclusion_updated_at": now,
				"inclusion_version": newVersion, "inclusion_reason": strings.TrimSpace(reason),
			}).Error; err != nil {
				return fmt.Errorf("update tournament inclusion: %w", err)
			}
			var audit TournamentInclusionAuditModel
			audit = TournamentInclusionAuditModel{TournamentID: tournament.ID, Included: included, PreviousIncluded: previous, ExpectedVersion: expectedVersion, NewVersion: newVersion, Reason: strings.TrimSpace(reason), ChangedAt: now}
			if err := tx.Create(&audit).Error; err != nil {
				return fmt.Errorf("write tournament inclusion audit: %w", err)
			}
			var rows []StandingModel
			if err := tx.Where("tournament_ref = ?", tournament.ID).Find(&rows).Error; err != nil {
				return fmt.Errorf("load affected standings: %w", err)
			}
			seen := make(map[string]struct{})
			for _, row := range rows {
				key := row.Source + "\x00" + row.PlayerKey
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				if err := r.recalculateAggregate(tx, row.Source, row.PlayerKey, now); err != nil {
					return err
				}
			}
			result.Changed = true
			result.AuditID = audit.ID
		}
		updated := tournament
		updated.IncludedInRanking = included
		updated.InclusionVersion = newVersion
		if previous == included {
			updated.InclusionReason = tournament.InclusionReason
		}
		row, rowErr := r.adminRowTx(tx, updated)
		if rowErr != nil {
			return rowErr
		}
		result.Tournament = row
		return nil
	})
	return result, err
}

func (r *Repository) adminRowTx(tx *gorm.DB, model TournamentModel) (domain.TournamentAdminRow, error) {
	var standings int64
	var players int64
	if err := tx.Model(&StandingModel{}).Where("tournament_ref = ?", model.ID).Count(&standings).Error; err != nil {
		return domain.TournamentAdminRow{}, err
	}
	if err := tx.Model(&StandingModel{}).Where("tournament_ref = ?", model.ID).Distinct("player_ref").Count(&players).Error; err != nil {
		return domain.TournamentAdminRow{}, err
	}
	return domain.TournamentAdminRow{Tournament: fromModel(model), StandingCount: int(standings), PlayerCount: int(players), StandingsComplete: model.StandingsSyncComplete, LastSyncError: model.LastStandingsSyncFailed, InclusionVersion: maxVersion(model.InclusionVersion)}, nil
}

func maxVersion(value int64) int64 {
	if value < 1 {
		return 1
	}
	return value
}
