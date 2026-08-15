package app

import (
	"context"
	"time"

	"github.com/rs/zerolog"
	"kickertool-ranking/internal/ports"
)

type SchedulerService struct {
	crawler  ports.Crawler
	clock    ports.Clock
	interval time.Duration
	logger   *zerolog.Logger
}

func NewScheduler(crawler ports.Crawler, clock ports.Clock, interval time.Duration, logger *zerolog.Logger) *SchedulerService {
	if interval <= 0 {
		interval = 15 * time.Minute
	}
	return &SchedulerService{crawler: crawler, clock: clock, interval: interval, logger: logger}
}

func (s *SchedulerService) Run(ctx context.Context) error {
	ticker := s.clock.NewTicker(s.interval)
	defer ticker.Stop()
	var pending *time.Time
	drainStale := func(cutoff time.Time) {
		for {
			select {
			case tick := <-ticker.Chan():
				if tick.After(cutoff) {
					pending = &tick
					return
				}
			default:
				return
			}
		}
	}
	run := func() {
		result, err := s.crawler.Crawl(ctx)
		if err != nil {
			if s.logger != nil {
				s.logger.Error().Err(err).Str("component", "scheduler").Msg("scheduled crawl failed; continuing")
			}
			return
		}
		if s.logger != nil {
			s.logger.Debug().Str("component", "scheduler").Int("found", result.Found).Msg("scheduled crawl completed")
		}
	}

	run()
	finished := s.clock.Now()
	// Ticks that elapsed during the immediate run are stale and must not
	// trigger an overlapping or catch-up run.
	drainStale(finished)
	for {
		if pending != nil {
			pending = nil
			run()
			finished = s.clock.Now()
			drainStale(finished)
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case tick := <-ticker.Chan():
			if !tick.After(finished) {
				continue
			}
			run()
			finished = s.clock.Now()
			// A run is synchronous, so ticks received while it was active are stale.
			drainStale(finished)
		}
	}
}
