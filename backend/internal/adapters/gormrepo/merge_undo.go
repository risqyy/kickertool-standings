package gormrepo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
	"kickertool-ranking/internal/domain"
	"kickertool-ranking/internal/ports"
)

const playerMergeUndoSnapshotVersion = 1

// playerMergeUndoSnapshot is the exact repository-owned state of the two
// players immediately before a merge. Derived aggregates are included so an
// undo restores the materialized read model byte-for-byte as well as its
// source rows.
type playerMergeUndoSnapshot struct {
	Source           PlayerModel
	Target           PlayerModel
	Aliases          []PlayerNameAliasModel
	SourceIdentities []SourcePlayerIdentityModel
	Standings        []StandingModel
	Allocations      []AllocationModel
	Corrections      []ManualRankingCorrectionModel
	Aggregates       []PlayerAggregateModel
	SourceBefore     domain.PlayerAggregate
	TargetBefore     domain.PlayerAggregate
}

func capturePlayerMergeState(tx *gorm.DB, sourceID, targetID uint) (playerMergeUndoSnapshot, error) {
	var snapshot playerMergeUndoSnapshot
	if err := tx.First(&snapshot.Source, sourceID).Error; err != nil {
		return snapshot, fmt.Errorf("load merge source state: %w", err)
	}
	if err := tx.First(&snapshot.Target, targetID).Error; err != nil {
		return snapshot, fmt.Errorf("load merge target state: %w", err)
	}
	ids := []uint{sourceID, targetID}
	queries := []struct {
		name string
		run  func() error
	}{
		{"aliases", func() error { return tx.Where("player_id IN ?", ids).Order("id ASC").Find(&snapshot.Aliases).Error }},
		{"source identities", func() error {
			return tx.Where("player_ref IN ?", ids).Order("id ASC").Find(&snapshot.SourceIdentities).Error
		}},
		{"standings", func() error { return tx.Where("player_ref IN ?", ids).Order("id ASC").Find(&snapshot.Standings).Error }},
		{"allocations", func() error {
			return tx.Where("player_ref IN ?", ids).Order("id ASC").Find(&snapshot.Allocations).Error
		}},
		{"ranking corrections", func() error {
			return tx.Where("player_ref IN ?", ids).Order("id ASC").Find(&snapshot.Corrections).Error
		}},
		{"aggregates", func() error { return tx.Where("player_ref IN ?", ids).Order("id ASC").Find(&snapshot.Aggregates).Error }},
	}
	for _, query := range queries {
		if err := query.run(); err != nil {
			return snapshot, fmt.Errorf("load merge %s state: %w", query.name, err)
		}
	}
	return snapshot, nil
}

func encodePlayerMergeState(snapshot playerMergeUndoSnapshot) (string, string, error) {
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return "", "", fmt.Errorf("encode player merge state: %w", err)
	}
	digest := sha256.Sum256(payload)
	return string(payload), hex.EncodeToString(digest[:]), nil
}

func currentPlayerMergeFingerprint(tx *gorm.DB, sourceID, targetID uint) (string, error) {
	snapshot, err := capturePlayerMergeState(tx, sourceID, targetID)
	if err != nil {
		return "", err
	}
	_, fingerprint, err := encodePlayerMergeState(snapshot)
	return fingerprint, err
}

func decodePlayerMergeUndoSnapshot(audit PlayerMergeAuditModel) (playerMergeUndoSnapshot, error) {
	if audit.UndoSnapshotVersion != playerMergeUndoSnapshotVersion || strings.TrimSpace(audit.UndoSnapshotJSON) == "" || strings.TrimSpace(audit.PostMergeFingerprint) == "" {
		return playerMergeUndoSnapshot{}, fmt.Errorf("%w: Für diese ältere Zusammenführung fehlen vollständige Wiederherstellungsdaten.", ports.ErrPlayerMergeUndoUnavailable)
	}
	var snapshot playerMergeUndoSnapshot
	if err := json.Unmarshal([]byte(audit.UndoSnapshotJSON), &snapshot); err != nil {
		return snapshot, fmt.Errorf("%w: Die gespeicherten Wiederherstellungsdaten sind beschädigt.", ports.ErrPlayerMergeUndoUnavailable)
	}
	if snapshot.Source.ID != audit.SourcePlayerID || snapshot.Target.ID != audit.TargetPlayerID {
		return snapshot, fmt.Errorf("%w: Die gespeicherten Wiederherstellungsdaten passen nicht zur Zusammenführung.", ports.ErrPlayerMergeUndoUnavailable)
	}
	return snapshot, nil
}

func loadPlayerMergeAudit(tx *gorm.DB, mergeID uint) (PlayerMergeAuditModel, error) {
	var audit PlayerMergeAuditModel
	if err := tx.First(&audit, mergeID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return audit, fmt.Errorf("player merge %d: %w", mergeID, ports.ErrNotFound)
		}
		return audit, fmt.Errorf("load player merge %d: %w", mergeID, err)
	}
	return audit, nil
}

func validatePlayerMergeUndoState(tx *gorm.DB, audit PlayerMergeAuditModel) (playerMergeUndoSnapshot, string, error) {
	if audit.UndoneAt != nil {
		return playerMergeUndoSnapshot{}, "", fmt.Errorf("%w: Diese Zusammenführung wurde bereits rückgängig gemacht.", ports.ErrPlayerMergeUndoUnavailable)
	}
	snapshot, err := decodePlayerMergeUndoSnapshot(audit)
	if err != nil {
		return snapshot, "", err
	}
	fingerprint, err := currentPlayerMergeFingerprint(tx, audit.SourcePlayerID, audit.TargetPlayerID)
	if err != nil {
		return snapshot, "", fmt.Errorf("%w: Der aktuelle Spielerzustand kann nicht sicher geprüft werden.", ports.ErrPlayerMergeUndoUnavailable)
	}
	if fingerprint != audit.PostMergeFingerprint {
		return snapshot, fingerprint, fmt.Errorf("%w: Die Spielerdaten haben sich seit der Zusammenführung geändert; ein vollständiges Rückgängigmachen ist nicht mehr sicher möglich.", ports.ErrVersionConflict)
	}
	return snapshot, fingerprint, nil
}

func (r *Repository) ListPlayerMerges(ctx context.Context) ([]domain.PlayerMergeAudit, error) {
	db := r.db.WithContext(ctx)
	var audits []PlayerMergeAuditModel
	if err := db.Order("merged_at DESC").Order("id DESC").Find(&audits).Error; err != nil {
		return nil, fmt.Errorf("list player merges: %w", err)
	}
	result := make([]domain.PlayerMergeAudit, 0, len(audits))
	for _, audit := range audits {
		item := playerMergeAuditFromModel(audit)
		if item.UndoAvailable {
			if _, _, err := validatePlayerMergeUndoState(db, audit); err != nil {
				item.UndoAvailable = false
				item.UndoUnavailableReason = playerMergeUndoReason(err)
			}
		}
		result = append(result, item)
	}
	return result, nil
}

func playerMergeUndoReason(err error) string {
	message := err.Error()
	for _, sentinel := range []error{ports.ErrPlayerMergeUndoUnavailable, ports.ErrVersionConflict} {
		prefix := sentinel.Error() + ": "
		if strings.HasPrefix(message, prefix) {
			return strings.TrimPrefix(message, prefix)
		}
	}
	return "Der aktuelle Spielerzustand kann nicht sicher geprüft werden."
}

func (r *Repository) PreviewPlayerMergeUndo(ctx context.Context, mergeID uint) (domain.PlayerMergeUndoPreview, error) {
	audit, err := loadPlayerMergeAudit(r.db.WithContext(ctx), mergeID)
	if err != nil {
		return domain.PlayerMergeUndoPreview{}, err
	}
	snapshot, fingerprint, err := validatePlayerMergeUndoState(r.db.WithContext(ctx), audit)
	if err != nil {
		return domain.PlayerMergeUndoPreview{}, err
	}
	return domain.PlayerMergeUndoPreview{Merge: playerMergeAuditFromModel(audit), SourceBefore: snapshot.SourceBefore, TargetBefore: snapshot.TargetBefore, StateFingerprint: fingerprint}, nil
}

func (r *Repository) UndoPlayerMerge(ctx context.Context, mergeID uint, options domain.PlayerMergeUndoOptions) (result domain.PlayerMergeUndoResult, err error) {
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		audit, err := loadPlayerMergeAudit(tx, mergeID)
		if err != nil {
			return err
		}
		snapshot, fingerprint, err := validatePlayerMergeUndoState(tx, audit)
		if err != nil {
			return err
		}
		if strings.TrimSpace(options.ExpectedFingerprint) == "" || options.ExpectedFingerprint != fingerprint {
			return fmt.Errorf("%w: Die Undo-Vorschau ist veraltet; bitte erneut prüfen.", ports.ErrVersionConflict)
		}
		if err := restorePlayerMergeState(tx, snapshot); err != nil {
			return err
		}
		now := r.clock.Now()
		if err := tx.Model(&audit).Updates(map[string]any{"undone_at": now, "undone_by": strings.TrimSpace(options.Actor), "undo_reason": strings.TrimSpace(options.Reason)}).Error; err != nil {
			return fmt.Errorf("mark player merge undone: %w", err)
		}
		audit.UndoneAt = &now
		audit.UndoneBy = strings.TrimSpace(options.Actor)
		audit.UndoReason = strings.TrimSpace(options.Reason)
		sourceAfter, err := aggregateForTx(tx, snapshot.Source, now)
		if err != nil {
			return fmt.Errorf("load restored source aggregate: %w", err)
		}
		targetAfter, err := aggregateForTx(tx, snapshot.Target, now)
		if err != nil {
			return fmt.Errorf("load restored target aggregate: %w", err)
		}
		result = domain.PlayerMergeUndoResult{Merge: playerMergeAuditFromModel(audit), SourceAfter: sourceAfter, TargetAfter: targetAfter, UndoneAt: now}
		return nil
	})
	return result, err
}

func restorePlayerMergeState(tx *gorm.DB, snapshot playerMergeUndoSnapshot) error {
	ids := []uint{snapshot.Source.ID, snapshot.Target.ID}
	deletes := []struct {
		name  string
		model any
		where string
	}{
		{"allocations", &AllocationModel{}, "player_ref IN ?"},
		{"standings", &StandingModel{}, "player_ref IN ?"},
		{"source identities", &SourcePlayerIdentityModel{}, "player_ref IN ?"},
		{"aliases", &PlayerNameAliasModel{}, "player_id IN ?"},
		{"ranking corrections", &ManualRankingCorrectionModel{}, "player_ref IN ?"},
		{"aggregates", &PlayerAggregateModel{}, "player_ref IN ?"},
	}
	for _, deletion := range deletes {
		if err := tx.Where(deletion.where, ids).Delete(deletion.model).Error; err != nil {
			return fmt.Errorf("clear merged %s: %w", deletion.name, err)
		}
	}
	if err := restorePlayerModel(tx, snapshot.Source); err != nil {
		return err
	}
	if err := restorePlayerModel(tx, snapshot.Target); err != nil {
		return err
	}
	if len(snapshot.Aliases) > 0 {
		if err := tx.Omit("Player").Create(&snapshot.Aliases).Error; err != nil {
			return fmt.Errorf("restore aliases: %w", err)
		}
	}
	if len(snapshot.SourceIdentities) > 0 {
		if err := tx.Omit("Player").Create(&snapshot.SourceIdentities).Error; err != nil {
			return fmt.Errorf("restore source identities: %w", err)
		}
	}
	if len(snapshot.Standings) > 0 {
		if err := tx.Omit("Player", "Tournament").Create(&snapshot.Standings).Error; err != nil {
			return fmt.Errorf("restore standings: %w", err)
		}
	}
	if len(snapshot.Allocations) > 0 {
		if err := tx.Create(&snapshot.Allocations).Error; err != nil {
			return fmt.Errorf("restore allocations: %w", err)
		}
	}
	if len(snapshot.Corrections) > 0 {
		if err := tx.Omit("Player").Create(&snapshot.Corrections).Error; err != nil {
			return fmt.Errorf("restore ranking corrections: %w", err)
		}
	}
	if len(snapshot.Aggregates) > 0 {
		if err := tx.Omit("Player").Create(&snapshot.Aggregates).Error; err != nil {
			return fmt.Errorf("restore aggregates: %w", err)
		}
	}
	return nil
}

func restorePlayerModel(tx *gorm.DB, player PlayerModel) error {
	updates := map[string]any{
		"canonical_name_key":         player.CanonicalNameKey,
		"display_name":               player.DisplayName,
		"merged_into_player_id":      player.MergedIntoPlayerID,
		"merged_at":                  player.MergedAt,
		"last_seen_at":               player.LastSeenAt,
		"created_at":                 player.CreatedAt,
		"updated_at":                 player.UpdatedAt,
		"ranking_correction_version": player.RankingCorrectionVersion,
	}
	result := tx.Model(&PlayerModel{}).Where("id = ?", player.ID).UpdateColumns(updates)
	if result.Error != nil {
		return fmt.Errorf("restore player %d: %w", player.ID, result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("restore player %d: %w", player.ID, ports.ErrNotFound)
	}
	return nil
}

func playerMergeAuditFromModel(audit PlayerMergeAuditModel) domain.PlayerMergeAudit {
	result := domain.PlayerMergeAudit{
		ID: audit.ID, SourcePlayerID: audit.SourcePlayerID, TargetPlayerID: audit.TargetPlayerID,
		SourceDisplayName: audit.SourceDisplayName, TargetDisplayName: audit.TargetDisplayName,
		MergedAt: audit.MergedAt, TransferredAliases: audit.TransferredAliases,
		TransferredSourceIdentities: audit.TransferredSourceIdentities,
		TransferredAllocations:      audit.TransferredAllocations, DeduplicatedAllocations: audit.DeduplicatedAllocations,
		Actor: audit.Actor, Reason: audit.Reason, UndoneAt: audit.UndoneAt, UndoneBy: audit.UndoneBy, UndoReason: audit.UndoReason,
	}
	switch {
	case audit.UndoneAt != nil:
		result.UndoUnavailableReason = "Diese Zusammenführung wurde bereits rückgängig gemacht."
	case audit.UndoSnapshotVersion != playerMergeUndoSnapshotVersion || audit.UndoSnapshotJSON == "" || audit.PostMergeFingerprint == "":
		result.UndoUnavailableReason = "Für diese ältere Zusammenführung fehlen vollständige Wiederherstellungsdaten."
	default:
		result.UndoAvailable = true
	}
	return result
}
