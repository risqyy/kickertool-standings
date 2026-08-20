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

// PeriodRankingReader provides the public ranking's period-aware view. The
// cumulative PlayerRankingReader remains the compatibility API used by the
// legacy standings route.
type PeriodRankingReader interface {
	PlayerRankingReader
	ListPlayerRankingForYear(ctx context.Context, year int) ([]domain.PlayerAggregate, error)
	ListAvailableRankingYears(ctx context.Context) ([]int, error)
}

// SnapshotRankingReader is used by audit/comparison views. The boundary is
// exclusive: a correction effective exactly at the newest tournament belongs
// to the current snapshot, not the immediately preceding one.
type SnapshotRankingReader interface {
	ListPlayerRankingBefore(ctx context.Context, cutoff time.Time) ([]domain.PlayerAggregate, error)
}

type PlayerMergeService interface {
	MergePlayers(ctx context.Context, sourcePlayerID, targetPlayerID uint, options domain.PlayerMergeOptions) (domain.MergeResult, error)
}

type PlayerDirectory interface {
	SearchPlayers(ctx context.Context, query string) ([]domain.PlayerProfile, error)
	GetPlayerProfile(ctx context.Context, playerID uint) (domain.PlayerProfile, error)
}

// ManualRankingCorrectionRepository stores immutable correction history and
// exposes transactional preview/confirm/revoke operations.
type ManualRankingCorrectionRepository interface {
	PreviewManualRankingCorrection(ctx context.Context, input domain.ManualRankingCorrectionInput) (domain.ManualRankingCorrectionPreview, error)
	CreateManualRankingCorrection(ctx context.Context, input domain.ManualRankingCorrectionInput, expectedVersion int64) (domain.ManualRankingCorrectionChange, error)
	ListManualRankingCorrections(ctx context.Context, playerID uint) ([]domain.ManualRankingCorrection, error)
	RevokeManualRankingCorrection(ctx context.Context, playerID, correctionID uint, expectedVersion int64, administrator, reason string) (domain.ManualRankingCorrectionRevocation, error)
}

// ManualRankingCorrectionReplaceRepository makes the immutable replace
// operation explicit for administrative clients and audit tooling.
type ManualRankingCorrectionReplaceRepository interface {
	ReplaceManualRankingCorrection(ctx context.Context, input domain.ManualRankingCorrectionInput, replaceCorrectionID uint, expectedVersion int64, expectedFingerprint string) (domain.ManualRankingCorrectionChange, error)
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
