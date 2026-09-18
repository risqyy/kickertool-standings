package gormrepo

import (
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// StandingArchiveModel preserves an obsolete raw row when a subsequent complete
// snapshot needs its unique identity for another existing row. It is immutable
// provenance, not an additional result or a player merge.
type StandingArchiveModel struct {
	ID            uint      `gorm:"primaryKey"`
	OriginalRowID uint      `gorm:"not null;index"`
	TournamentRef uint      `gorm:"not null;index"`
	SnapshotJSON  string    `gorm:"not null"`
	ArchivedAt    time.Time `gorm:"not null"`
}

func archiveObsoleteStanding(tx *gorm.DB, row StandingModel, now time.Time) error {
	if !row.Superseded {
		return fmt.Errorf("cannot archive a current standing")
	}
	serialized, err := json.Marshal(row)
	if err != nil {
		return err
	}
	archive := StandingArchiveModel{OriginalRowID: row.ID, TournamentRef: row.TournamentRef, SnapshotJSON: string(serialized), ArchivedAt: now}
	if err := tx.Create(&archive).Error; err != nil {
		return fmt.Errorf("archive obsolete standing: %w", err)
	}
	if err := tx.Delete(&row).Error; err != nil {
		return fmt.Errorf("release obsolete standing identity: %w", err)
	}
	return nil
}
