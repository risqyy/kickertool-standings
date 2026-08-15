package gormrepo

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
	"kickertool-ranking/internal/domain"
)

var errMergeDryRun = errors.New("merge dry run rollback")

// MergePlayers transfers source into target. The target is always the active
// canonical player; the source row is retained as a tombstone for auditability.
// A dry run executes the same transaction and rolls it back before returning.
func (r *Repository) MergePlayers(ctx context.Context, sourcePlayerID, targetPlayerID uint, options domain.PlayerMergeOptions) (result domain.MergeResult, err error) {
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var source, target PlayerModel
		if err := tx.First(&source, sourcePlayerID).Error; err != nil {
			return fmt.Errorf("load source player %d: %w", sourcePlayerID, err)
		}
		if err := tx.First(&target, targetPlayerID).Error; err != nil {
			return fmt.Errorf("load target player %d: %w", targetPlayerID, err)
		}
		if sourcePlayerID == targetPlayerID {
			return errors.New("source and target player must differ")
		}
		sourceRoot, err := resolvePlayerRoot(tx, source.ID)
		if err != nil {
			return err
		}
		targetRoot, err := resolvePlayerRoot(tx, target.ID)
		if err != nil {
			return err
		}
		result = domain.MergeResult{
			SourcePlayerID: sourceRoot.ID, TargetPlayerID: targetRoot.ID,
			SourceDisplayName: sourceRoot.DisplayName, TargetDisplayName: targetRoot.DisplayName,
			DryRun: options.DryRun, MergedAt: r.clock.Now(),
		}
		if aggregate, err := aggregateForTx(tx, sourceRoot); err != nil {
			return err
		} else {
			result.SourceBefore = &aggregate
		}
		if aggregate, err := aggregateForTx(tx, targetRoot); err != nil {
			return err
		} else {
			result.TargetBefore = &aggregate
		}
		if sourceRoot.ID == targetRoot.ID {
			result.AlreadyMerged = true
			return nil
		}

		if err := transferAliases(tx, sourceRoot.ID, targetRoot.ID, &result); err != nil {
			return err
		}
		if err := transferSourceIdentities(tx, sourceRoot.ID, targetRoot.ID, &result); err != nil {
			return err
		}
		sources, err := transferStandingRows(tx, sourceRoot, targetRoot, &result)
		if err != nil {
			return err
		}
		if err := transferAllocationRows(tx, sourceRoot.ID, targetRoot.ID, &result); err != nil {
			return err
		}

		if err := tx.Model(&sourceRoot).Updates(map[string]any{
			"merged_into_player_id": targetRoot.ID,
			"merged_at":             result.MergedAt,
		}).Error; err != nil {
			return fmt.Errorf("mark source player merged: %w", err)
		}

		for sourceName := range sources {
			if err := tx.Where("source = ? AND player_key = ?", sourceName, sourceRoot.CanonicalNameKey).Delete(&PlayerAggregateModel{}).Error; err != nil {
				return fmt.Errorf("remove source aggregate: %w", err)
			}
			if err := r.recalculateAggregate(tx, sourceName, targetRoot.CanonicalNameKey, result.MergedAt); err != nil {
				return err
			}
			result.RecalculatedAggregates++
		}
		if aggregate, err := aggregateForTx(tx, targetRoot); err != nil {
			return err
		} else {
			result.TargetAfter = &aggregate
		}
		audit := PlayerMergeAuditModel{
			SourcePlayerID: sourceRoot.ID, TargetPlayerID: targetRoot.ID,
			SourceDisplayName: sourceRoot.DisplayName, TargetDisplayName: targetRoot.DisplayName,
			MergedAt: result.MergedAt, TransferredAliases: result.TransferredAliases,
			TransferredSourceIdentities: result.TransferredSourceIdentities,
			TransferredAllocations:      result.TransferredAllocations,
			DeduplicatedAllocations:     result.DeduplicatedAllocations,
			Actor:                       strings.TrimSpace(options.Actor), Reason: strings.TrimSpace(options.Reason),
		}
		if err := tx.Create(&audit).Error; err != nil {
			return fmt.Errorf("write player merge audit: %w", err)
		}
		if options.DryRun {
			return errMergeDryRun
		}
		return nil
	})
	if errors.Is(err, errMergeDryRun) {
		return result, nil
	}
	return result, err
}

func aggregateForTx(tx *gorm.DB, player PlayerModel) (domain.PlayerAggregate, error) {
	var rows []StandingModel
	if err := tx.Where("player_ref = ?", player.ID).Find(&rows).Error; err != nil {
		return domain.PlayerAggregate{}, err
	}
	return aggregateFromRows(rows, player), nil
}

func transferAliases(tx *gorm.DB, sourceID, targetID uint, result *domain.MergeResult) error {
	var aliases []PlayerNameAliasModel
	if err := tx.Where("player_id = ?", sourceID).Order("id ASC").Find(&aliases).Error; err != nil {
		return fmt.Errorf("load source aliases: %w", err)
	}
	for _, alias := range aliases {
		var existing PlayerNameAliasModel
		err := tx.Where("name_key = ?", alias.NameKey).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if err := tx.Model(&alias).Update("player_id", targetID).Error; err != nil {
				return fmt.Errorf("transfer alias %s: %w", alias.NameKey, err)
			}
			result.TransferredAliases++
			continue
		}
		if err != nil {
			return fmt.Errorf("find alias %s: %w", alias.NameKey, err)
		}
		if existing.PlayerID == sourceID {
			if err := tx.Model(&alias).Update("player_id", targetID).Error; err != nil {
				return fmt.Errorf("transfer alias %s: %w", alias.NameKey, err)
			}
			result.TransferredAliases++
			continue
		}
		if existing.PlayerID != targetID {
			return fmt.Errorf("alias conflict for normalized name %q", alias.NameKey)
		}
		if err := tx.Delete(&alias).Error; err != nil {
			return fmt.Errorf("deduplicate alias %s: %w", alias.NameKey, err)
		}
	}
	return nil
}

func transferSourceIdentities(tx *gorm.DB, sourceID, targetID uint, result *domain.MergeResult) error {
	var identities []SourcePlayerIdentityModel
	if err := tx.Where("player_ref = ?", sourceID).Order("id ASC").Find(&identities).Error; err != nil {
		return fmt.Errorf("load source identities: %w", err)
	}
	for _, identity := range identities {
		var existing SourcePlayerIdentityModel
		err := tx.Where("source = ? AND external_id = ? AND name_key = ?", identity.Source, identity.ExternalID, identity.NameKey).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if err := tx.Model(&identity).Update("player_ref", targetID).Error; err != nil {
				return fmt.Errorf("transfer source identity: %w", err)
			}
			result.TransferredSourceIdentities++
			continue
		}
		if err != nil {
			return fmt.Errorf("find source identity: %w", err)
		}
		if existing.PlayerRef == sourceID {
			if err := tx.Model(&identity).Update("player_ref", targetID).Error; err != nil {
				return fmt.Errorf("transfer source identity: %w", err)
			}
			result.TransferredSourceIdentities++
			continue
		}
		if existing.PlayerRef != targetID {
			return fmt.Errorf("source identity conflict for %s/%s", identity.Source, identity.ExternalID)
		}
		if err := tx.Delete(&identity).Error; err != nil {
			return fmt.Errorf("deduplicate source identity: %w", err)
		}
	}
	return nil
}

func transferStandingRows(tx *gorm.DB, source, target PlayerModel, result *domain.MergeResult) (map[string]struct{}, error) {
	var rows []StandingModel
	if err := tx.Where("player_ref = ?", source.ID).Order("id ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load source standings: %w", err)
	}
	sources := make(map[string]struct{})
	for _, row := range rows {
		sources[row.Source] = struct{}{}
		var collision StandingModel
		findErr := tx.Where("player_ref = ? AND source = ? AND tournament_id = ?", target.ID, row.Source, row.TournamentID).First(&collision).Error
		if errors.Is(findErr, gorm.ErrRecordNotFound) && row.SourceStandingID != nil && *row.SourceStandingID != "" {
			findErr = tx.Where("player_ref = ? AND source = ? AND source_standing_id = ?", target.ID, row.Source, *row.SourceStandingID).First(&collision).Error
		}
		if findErr == nil {
			if err := tx.Delete(&row).Error; err != nil {
				return nil, fmt.Errorf("deduplicate standing %d: %w", row.ID, err)
			}
			result.DeduplicatedAllocations++
			if row.SourceStandingID != nil && *row.SourceStandingID != "" {
				if err := tx.Where("source = ? AND standing_id = ?", row.Source, *row.SourceStandingID).Delete(&AllocationModel{}).Error; err != nil {
					return nil, fmt.Errorf("deduplicate standing allocations: %w", err)
				}
			}
			continue
		}
		if !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("find standing collision: %w", findErr)
		}
		if err := tx.Model(&row).Updates(map[string]any{"player_ref": target.ID, "player_key": target.CanonicalNameKey}).Error; err != nil {
			return nil, fmt.Errorf("transfer standing %d: %w", row.ID, err)
		}
		result.TransferredAllocations++
	}
	return sources, nil
}

func transferAllocationRows(tx *gorm.DB, sourceID, targetID uint, result *domain.MergeResult) error {
	var rows []AllocationModel
	if err := tx.Where("player_ref = ?", sourceID).Order("id ASC").Find(&rows).Error; err != nil {
		return fmt.Errorf("load source allocations: %w", err)
	}
	for _, row := range rows {
		var collision AllocationModel
		err := tx.Where("source = ? AND standing_id = ? AND player_ref = ?", row.Source, row.StandingID, targetID).First(&collision).Error
		if err == nil {
			if err := tx.Delete(&row).Error; err != nil {
				return fmt.Errorf("deduplicate allocation %d: %w", row.ID, err)
			}
			result.DeduplicatedAllocations++
			continue
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("find allocation collision: %w", err)
		}
		if err := tx.Model(&row).Update("player_ref", targetID).Error; err != nil {
			return fmt.Errorf("transfer allocation %d: %w", row.ID, err)
		}
		result.TransferredAllocations++
	}
	return nil
}

// MergeAudit returns immutable merge records in deterministic order.
func (r *Repository) MergeAudit(ctx context.Context) ([]PlayerMergeAuditModel, error) {
	var audits []PlayerMergeAuditModel
	err := r.db.WithContext(ctx).Order("merged_at DESC").Order("id DESC").Find(&audits).Error
	return audits, err
}
