package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"kickertool-ranking/internal/domain"
	"kickertool-ranking/internal/ports"
)

// TournamentRefresher uses the same gate and standing repository as the regular
// crawler. Jobs outlive the HTTP request, but stop on application shutdown or
// after ten minutes. The most recent 100 jobs are retained until process exit.
type TournamentRefresher struct {
	ctx      context.Context
	crawler  *CrawlerService
	lookup   ports.TournamentLookup
	mu       sync.Mutex
	jobs     map[string]domain.TournamentRefreshJob
	order    []string
	workers  sync.WaitGroup
	stopping bool
}

func NewTournamentRefresher(ctx context.Context, crawler *CrawlerService, lookup ports.TournamentLookup) *TournamentRefresher {
	return &TournamentRefresher{ctx: ctx, crawler: crawler, lookup: lookup, jobs: make(map[string]domain.TournamentRefreshJob)}
}

func (s *TournamentRefresher) Start(ctx context.Context, id uint) (domain.TournamentRefreshJob, error) {
	// Count admitted requests as well as workers: HTTP shutdown can time out
	// while a request is still looking up its tournament.
	s.mu.Lock()
	if s.stopping {
		s.mu.Unlock()
		return domain.TournamentRefreshJob{}, context.Canceled
	}
	s.workers.Add(1)
	s.mu.Unlock()
	defer s.workers.Done()
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.ctx, cancel)
	defer func() { stop(); cancel() }()
	if err := s.ctx.Err(); err != nil {
		return domain.TournamentRefreshJob{}, err
	}
	select {
	case s.crawler.syncGate <- struct{}{}:
	default:
		return domain.TournamentRefreshJob{}, ports.ErrSyncBusy
	}
	started := false
	defer func() {
		if !started {
			<-s.crawler.syncGate
		}
	}()
	row, err := s.lookup.GetTournament(ctx, id)
	if err != nil {
		return domain.TournamentRefreshJob{}, err
	}
	source, named := s.crawler.source.(ports.NamedTournamentSource)
	if !named || source.SourceName() != row.Source || s.crawler.standingSource == nil || s.crawler.standingRepo == nil {
		return domain.TournamentRefreshJob{}, ports.ErrRefreshSource
	}
	if err := ctx.Err(); err != nil {
		return domain.TournamentRefreshJob{}, err
	}
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return domain.TournamentRefreshJob{}, err
	}
	job := domain.TournamentRefreshJob{ID: hex.EncodeToString(token[:]), TournamentID: id, State: "running", StartedAt: s.crawler.clock.Now()}
	s.mu.Lock()
	if s.stopping || s.ctx.Err() != nil {
		s.mu.Unlock()
		return domain.TournamentRefreshJob{}, context.Canceled
	}
	if len(s.order) == 100 {
		delete(s.jobs, s.order[0])
		s.order = s.order[1:]
	}
	s.jobs[job.ID] = job
	s.order = append(s.order, job.ID)
	s.mu.Unlock()
	started = true
	s.workers.Add(1)
	go s.run(job, row.Tournament)
	return job, nil
}

func (s *TournamentRefresher) Status(ctx context.Context, id string) (domain.TournamentRefreshJob, error) {
	if err := ctx.Err(); err != nil {
		return domain.TournamentRefreshJob{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return domain.TournamentRefreshJob{}, ports.ErrNotFound
	}
	return job, nil
}

func (s *TournamentRefresher) run(job domain.TournamentRefreshJob, tournament domain.Tournament) {
	defer s.workers.Done()
	defer func() { <-s.crawler.syncGate }()
	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Minute)
	defer cancel()
	snapshot, err := s.crawler.standingSource.FetchStandings(ctx, tournament)
	if err == nil && (!snapshot.Complete || snapshot.Source != tournament.Source || snapshot.TournamentID != tournament.SourceID) {
		err = fmt.Errorf("incomplete or mismatched tournament snapshot")
	}
	if err == nil {
		err = ctx.Err()
	}
	if err == nil {
		_, err = s.crawler.standingRepo.UpsertStandingSnapshot(ctx, snapshot)
	}
	job.State = "succeeded"
	if err != nil {
		job.State = "failed"
		// Also record failed attempts when the fetch context expired. No source
		// values are written here; a retained complete snapshot stays eligible.
		failureCtx, stop := context.WithTimeout(context.WithoutCancel(s.ctx), 5*time.Second)
		s.crawler.markStandingFailure(failureCtx, tournament)
		stop()
	}
	if s.crawler.logger != nil {
		s.crawler.logger.Info().Err(err).Uint("tournament_id", tournament.ID).Str("job_id", job.ID).Str("state", job.State).Msg("manual tournament refresh finished")
	}
	finished := s.crawler.clock.Now()
	job.FinishedAt = &finished
	s.mu.Lock()
	s.jobs[job.ID] = job
	s.mu.Unlock()
}

// Wait waits for accepted requests and workers. The caller must prevent new
// starts; application shutdown uses Shutdown to close admission first.
func (s *TournamentRefresher) Wait() { s.workers.Wait() }

// Shutdown closes job admission and drains accepted requests and workers even
// if HTTP shutdown timed out. Cancel the application context before calling.
func (s *TournamentRefresher) Shutdown() {
	s.mu.Lock()
	s.stopping = true
	s.mu.Unlock()
	s.Wait()
}
