package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"kickertool-ranking/internal/domain"
)

type blockedRefreshLookup struct {
	entered chan struct{}
	release chan struct{}
}

func (l blockedRefreshLookup) GetTournament(context.Context, uint) (domain.TournamentAdminRow, error) {
	close(l.entered)
	<-l.release // Deliberately emulate a lookup that has not honored cancellation yet.
	return domain.TournamentAdminRow{}, context.Canceled
}

func TestRefreshShutdownDrainsAdmittedLookupAndRejectsNewStarts(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	lookup := blockedRefreshLookup{entered: make(chan struct{}), release: make(chan struct{})}
	service := NewTournamentRefresher(ctx, NewCrawler(nil, nil, nil, nil), lookup)
	startDone := make(chan error, 1)
	go func() { _, err := service.Start(context.Background(), 7); startDone <- err }()
	select {
	case <-lookup.entered:
	case <-time.After(time.Second):
		t.Fatal("lookup did not start")
	}
	cancel()
	shutdownDone := make(chan struct{})
	go func() { service.Shutdown(); close(shutdownDone) }()
	deadline := time.After(time.Second)
	for {
		service.mu.Lock()
		stopping := service.stopping
		service.mu.Unlock()
		if stopping {
			break
		}
		select {
		case <-deadline:
			t.Fatal("shutdown did not close admission")
		case <-time.After(time.Millisecond):
		}
	}
	if _, err := service.Start(context.Background(), 7); !errors.Is(err, context.Canceled) {
		t.Fatalf("late start: %v", err)
	}
	select {
	case <-shutdownDone:
		t.Fatal("shutdown returned before admitted lookup finished")
	case <-time.After(20 * time.Millisecond):
	}
	close(lookup.release)
	if err := <-startDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("admitted start: %v", err)
	}
	select {
	case <-shutdownDone:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not finish after lookup")
	}
}
