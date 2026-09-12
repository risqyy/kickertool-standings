package gormrepo

import (
	"context"
	"fmt"
	"sort"
	"time"

	"kickertool-ranking/internal/domain"
)

func (r *Repository) ListPlayerRankingForMonth(ctx context.Context, year, month int) ([]domain.PlayerAggregate, error) {
	if year < 1000 || year > 9999 || month < 1 || month > 12 {
		return nil, fmt.Errorf("invalid ranking month")
	}
	rows, err := r.listRankingRowsForYear(ctx, year, month)
	if err != nil {
		return nil, err
	}
	corrections, err := r.activeCorrections(ctx, r.clock.Now(), &year, nil, month)
	if err != nil {
		return nil, err
	}
	ranking, err := r.aggregateRankingRows(ctx, rows, corrections)
	if err != nil {
		return nil, err
	}
	return r.withRankingTrends(ctx, ranking, &year, month)
}

// Months follow the annual qualification rules. Correction-only months must
// produce a visible row; a points-only booking cannot manufacture a period.
func (r *Repository) ListAvailableRankingMonths(ctx context.Context) ([]domain.RankingMonth, error) {
	tournaments, err := r.qualifiedRankingTournaments(ctx, nil)
	if err != nil {
		return nil, err
	}
	location, err := time.LoadLocation(domain.RankingLocation)
	if err != nil {
		return nil, fmt.Errorf("load ranking timezone: %w", err)
	}
	seen := make(map[domain.RankingMonth]bool)
	for _, tournament := range tournaments {
		date := tournament.Date.In(location)
		seen[domain.RankingMonth{Year: date.Year(), Month: int(date.Month())}] = true
	}
	corrections, err := r.activeCorrections(ctx, r.clock.Now(), nil, nil)
	if err != nil {
		return nil, err
	}
	byMonth := make(map[domain.RankingMonth][]ManualRankingCorrectionModel)
	for _, correction := range corrections {
		date := correction.EffectiveDate.In(location)
		period := domain.RankingMonth{Year: date.Year(), Month: int(date.Month())}
		if !seen[period] {
			byMonth[period] = append(byMonth[period], correction)
		}
	}
	for period, active := range byMonth {
		ranking, err := r.aggregateRankingRows(ctx, nil, active)
		if err != nil {
			return nil, err
		}
		if len(ranking) > 0 {
			seen[period] = true
		}
	}
	months := make([]domain.RankingMonth, 0, len(seen))
	for month := range seen {
		months = append(months, month)
	}
	sort.Slice(months, func(i, j int) bool {
		if months[i].Year != months[j].Year {
			return months[i].Year > months[j].Year
		}
		return months[i].Month > months[j].Month
	})
	return months, nil
}
