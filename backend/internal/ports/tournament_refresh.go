package ports

import (
	"context"
	"errors"

	"kickertool-ranking/internal/domain"
)

var ErrSyncBusy = errors.New("a synchronization is already running")
var ErrRefreshSource = errors.New("tournament source is not configured")

type TournamentLookup interface {
	GetTournament(context.Context, uint) (domain.TournamentAdminRow, error)
}

type TournamentRefresher interface {
	Start(context.Context, uint) (domain.TournamentRefreshJob, error)
	Status(context.Context, string) (domain.TournamentRefreshJob, error)
}
