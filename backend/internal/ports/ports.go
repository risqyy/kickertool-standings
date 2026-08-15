package ports

import (
	"context"
	"errors"
	"net/http"
	"time"

	"kickertool-ranking/internal/domain"
)

var ErrNotFound = errors.New("not found")

type TournamentSource interface {
	FetchTournaments(ctx context.Context) ([]domain.Tournament, error)
}

type NamedTournamentSource interface {
	SourceName() string
}

type TournamentStandingSource interface {
	FetchStandings(ctx context.Context, tournament domain.Tournament) (domain.StandingSnapshot, error)
}

type TournamentRepository interface {
	UpsertMany(ctx context.Context, tournaments []domain.Tournament) (domain.SyncResult, error)
	FindBySourceID(ctx context.Context, source, sourceID string) (domain.Tournament, error)
}

var ErrVersionConflict = errors.New("version conflict")

type TournamentAdminRepository interface {
	ListTournaments(ctx context.Context, filter domain.TournamentListFilter) (domain.TournamentPage, error)
	GetDashboard(ctx context.Context) (domain.Dashboard, error)
	SetTournamentRankingInclusion(ctx context.Context, tournamentID uint, included bool, expectedVersion int64, reason string) (domain.TournamentInclusionChange, error)
}

type StandingRepository interface {
	UpsertStandingSnapshot(ctx context.Context, snapshot domain.StandingSnapshot) (domain.StandingSyncResult, error)
}

type StandingSyncStateRepository interface {
	MarkStandingSyncFailed(ctx context.Context, source, tournamentID string) error
}

// PlayerRankingReader exposes the materialized accumulated ranking to read-only adapters.
type PlayerRankingReader interface {
	ListPlayerRanking(ctx context.Context) ([]domain.PlayerAggregate, error)
}

type PlayerMergeService interface {
	MergePlayers(ctx context.Context, sourcePlayerID, targetPlayerID uint, options domain.PlayerMergeOptions) (domain.MergeResult, error)
}

type PlayerDirectory interface {
	SearchPlayers(ctx context.Context, query string) ([]domain.PlayerProfile, error)
	GetPlayerProfile(ctx context.Context, playerID uint) (domain.PlayerProfile, error)
}

type Crawler interface {
	Crawl(ctx context.Context) (domain.SyncResult, error)
}

type Scheduler interface {
	Run(ctx context.Context) error
}

type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

type Clock interface {
	Now() time.Time
	NewTicker(interval time.Duration) Ticker
}

type Ticker interface {
	Chan() <-chan time.Time
	Stop()
}
