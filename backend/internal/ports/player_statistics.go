package ports

import (
	"context"
	"kickertool-ranking/internal/domain"
)

type PlayerStatisticsRepository interface {
	ListPlayers(context.Context, domain.PlayerListFilter) (domain.PlayerPage, error)
	GetPlayerStatistics(context.Context, uint) (domain.PlayerStatistics, error)
}
