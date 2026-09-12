package gormrepo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// A singleton rather than MAX(standings_synced_at): an individual tournament
// timestamp cannot attest to discovery and every due standings check succeeding.
// Existing installations intentionally start without a successful crawl record.
type CrawlSyncStatusModel struct {
	ID         uint      `gorm:"primaryKey"`
	FinishedAt time.Time `gorm:"not null"`
}

func (r *Repository) RecordSuccessfulCrawl(ctx context.Context, finishedAt time.Time) error {
	if finishedAt.IsZero() {
		return fmt.Errorf("successful crawl completion must not be zero")
	}
	record := CrawlSyncStatusModel{ID: 1, FinishedAt: finishedAt.UTC()}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"finished_at"}),
	}).Create(&record).Error
}

func (r *Repository) LastSyncAt(ctx context.Context) (*time.Time, error) {
	var record CrawlSyncStatusModel
	if err := r.db.WithContext(ctx).First(&record, 1).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("load successful crawl status: %w", err)
	}
	return &record.FinishedAt, nil
}
