package app

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"kickertool-ranking/internal/domain"
	"kickertool-ranking/internal/ports"
)

type fixedClock struct {
	mu     sync.Mutex
	now    time.Time
	ticker *fakeTicker
}

func (c *fixedClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(time.Millisecond)
	return c.now
}
func (c *fixedClock) NewTicker(time.Duration) ports.Ticker { return c.ticker }

type fakeTicker struct {
	ch      chan time.Time
	stopped atomic.Bool
}

func (t *fakeTicker) Chan() <-chan time.Time { return t.ch }
func (t *fakeTicker) Stop()                  { t.stopped.Store(true) }

type fakeSource struct {
	tournaments []domain.Tournament
	err         error
}

func (s fakeSource) FetchTournaments(context.Context) ([]domain.Tournament, error) {
	return s.tournaments, s.err
}

type fakeRepo struct {
	result   domain.SyncResult
	err      error
	received []domain.Tournament
}

type fakeStandingSource struct {
	called int
}

func (s *fakeStandingSource) FetchStandings(context.Context, domain.Tournament) (domain.StandingSnapshot, error) {
	s.called++
	return domain.StandingSnapshot{Source: domain.KickertoolAPISource, TournamentID: "old", Complete: true}, nil
}

type fakeStandingRepo struct{ calls int }

func (r *fakeStandingRepo) UpsertStandingSnapshot(context.Context, domain.StandingSnapshot) (domain.StandingSyncResult, error) {
	r.calls++
	return domain.StandingSyncResult{}, nil
}

func (r *fakeRepo) UpsertMany(_ context.Context, ts []domain.Tournament) (domain.SyncResult, error) {
	r.received = append(r.received, ts...)
	return r.result, r.err
}
func (r *fakeRepo) FindBySourceID(context.Context, string, string) (domain.Tournament, error) {
	return domain.Tournament{}, ports.ErrNotFound
}

func TestCrawlerValidatesAndLogsStructuredResult(t *testing.T) {
	clock := &fixedClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	repo := &fakeRepo{result: domain.SyncResult{Inserted: 1}}
	valid := domain.Tournament{Source: domain.KickertoolAPISource, SourceID: "ok", SourceKey: "key", Name: "Valid", URL: "https://example.test/ok"}
	invalid := domain.Tournament{Source: domain.KickertoolAPISource, SourceID: "bad"}
	var log bytes.Buffer
	logger := zerolog.New(&log)
	crawler := NewCrawler(fakeSource{tournaments: []domain.Tournament{valid, invalid}}, repo, clock, &logger)
	result, err := crawler.Crawl(context.Background())
	if err != nil || result.Found != 2 || result.Invalid != 1 || result.Inserted != 1 || len(repo.received) != 1 {
		t.Fatalf("result=%+v err=%v received=%d", result, err, len(repo.received))
	}
	text := log.String()
	for _, field := range []string{"\"source\":\"crawler\"", "\"found\":2", "\"inserted\":1", "\"invalid\":1"} {
		if !strings.Contains(text, field) {
			t.Fatalf("log missing %s: %s", field, text)
		}
	}
}

func TestCrawlerProcessesCompletedOnly(t *testing.T) {
	clock := &fixedClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.FixedZone("CET", 3600))}
	listingRepo := &fakeRepo{}
	standingSource := &fakeStandingSource{}
	standingRepo := &fakeStandingRepo{}
	oldDate := time.Date(2025, 12, 31, 0, 0, 0, 0, time.FixedZone("CET", 3600))
	futureDate := time.Date(2026, 1, 1, 0, 0, 0, 0, time.FixedZone("CET", 3600))
	old := domain.Tournament{Source: domain.KickertoolAPISource, SourceID: "old", SourceKey: "old", Name: "Old", URL: "https://example.test/old", Date: &oldDate}
	future := domain.Tournament{Source: domain.KickertoolAPISource, SourceID: "future", SourceKey: "future", Name: "Future", URL: "https://example.test/future", Date: &futureDate}
	logger := zerolog.Nop()
	crawler := NewCrawler(fakeSource{tournaments: []domain.Tournament{old, future}}, listingRepo, clock, &logger, WithStandings(standingSource, standingRepo))
	result, err := crawler.Crawl(context.Background())
	if err != nil || result.TournamentsProcessed != 2 || result.TournamentsSucceeded != 1 || result.TournamentsSkipped != 1 || standingSource.called != 1 || standingRepo.calls != 1 {
		t.Fatalf("result=%+v source_calls=%d repo_calls=%d err=%v", result, standingSource.called, standingRepo.calls, err)
	}
}

type fakeCrawler struct {
	mu        sync.Mutex
	calls     int
	active    int
	maxActive int
	failNext  bool
	entered   chan struct{}
	release   chan struct{}
}

func (c *fakeCrawler) Crawl(ctx context.Context) (domain.SyncResult, error) {
	c.mu.Lock()
	c.calls++
	c.active++
	if c.active > c.maxActive {
		c.maxActive = c.active
	}
	call := c.calls
	c.mu.Unlock()
	if c.entered != nil {
		select {
		case c.entered <- struct{}{}:
		default:
		}
	}
	if call == 1 && c.release != nil {
		select {
		case <-c.release:
		case <-ctx.Done():
		}
	}
	c.mu.Lock()
	c.active--
	fail := c.failNext
	c.failNext = false
	c.mu.Unlock()
	if fail {
		return domain.SyncResult{}, errors.New("crawl failed")
	}
	return domain.SyncResult{Found: 1}, nil
}

func TestSchedulerImmediateTicksFailureContinuationAndStop(t *testing.T) {
	ticker := &fakeTicker{ch: make(chan time.Time, 4)}
	clock := &fixedClock{now: time.Now(), ticker: ticker}
	crawler := &fakeCrawler{failNext: true, entered: make(chan struct{}, 2)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	logger := zerolog.Nop()
	done := make(chan error, 1)
	go func() { done <- NewScheduler(crawler, clock, time.Millisecond, &logger).Run(ctx) }()
	select {
	case <-crawler.entered:
	case <-time.After(time.Second):
		t.Fatal("immediate crawl did not start")
	}
	waitForCrawlerIdle(t, crawler)
	time.Sleep(10 * time.Millisecond)
	ticker.ch <- time.Now()
	select {
	case <-crawler.entered:
	case <-time.After(time.Second):
		t.Fatal("scheduled crawl did not start")
	}
	crawler.mu.Lock()
	calls := crawler.calls
	crawler.mu.Unlock()
	if calls < 2 {
		t.Fatalf("expected immediate plus tick, calls=%d", calls)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("scheduler error=%v", err)
	}
	if !ticker.stopped.Load() {
		t.Fatal("ticker was not stopped")
	}
}

func TestSchedulerDoesNotOverlapAndDrainsStaleTicks(t *testing.T) {
	ticker := &fakeTicker{ch: make(chan time.Time, 4)}
	clock := &fixedClock{now: time.Now(), ticker: ticker}
	crawler := &fakeCrawler{entered: make(chan struct{}, 2), release: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	logger := zerolog.Nop()
	done := make(chan error, 1)
	go func() { done <- NewScheduler(crawler, clock, time.Millisecond, &logger).Run(ctx) }()
	<-crawler.entered
	ticker.ch <- clock.Now()
	ticker.ch <- clock.Now()
	close(crawler.release)
	time.Sleep(20 * time.Millisecond)
	cancel()
	<-done
	crawler.mu.Lock()
	calls, maxActive := crawler.calls, crawler.maxActive
	crawler.mu.Unlock()
	if calls != 1 || maxActive != 1 {
		t.Fatalf("calls=%d max_active=%d", calls, maxActive)
	}
}

func waitForCrawlerIdle(t *testing.T, crawler *fakeCrawler) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		crawler.mu.Lock()
		active := crawler.active
		crawler.mu.Unlock()
		if active == 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("crawler did not become idle")
}
