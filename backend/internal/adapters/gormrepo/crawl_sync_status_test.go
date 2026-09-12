package gormrepo

import (
	"context"
	"kickertool-ranking/internal/adapters"
	"path/filepath"
	"testing"
	"time"
)

func TestSuccessfulCrawlStatusSurvivesSQLiteReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "status.db")
	repo, db, err := OpenSQLite(path, adapters.SystemClock{})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := repo.LastSyncAt(ctx); err != nil || got != nil {
		t.Fatalf("new DB status=%v err=%v", got, err)
	}
	expected := time.Date(2026, 9, 12, 13, 42, 19, 123000000, time.FixedZone("CEST", 7200))
	if err := repo.RecordSuccessfulCrawl(ctx, expected); err != nil {
		t.Fatal(err)
	}
	next := expected.Add(15 * time.Minute)
	if err := repo.RecordSuccessfulCrawl(ctx, next); err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	repo, db, err = OpenSQLite(path, adapters.SystemClock{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ = db.DB()
	defer sqlDB.Close()
	got, err := repo.LastSyncAt(ctx)
	if err != nil || got == nil || !got.Equal(next) {
		t.Fatalf("persisted timestamp=%v expected=%v err=%v", got, next, err)
	}
	if err := db.Migrator().DropTable(&CrawlSyncStatusModel{}); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.LastSyncAt(ctx); err == nil || got != nil {
		t.Fatalf("missing table must be retrieval error: %v %v", got, err)
	}
}
