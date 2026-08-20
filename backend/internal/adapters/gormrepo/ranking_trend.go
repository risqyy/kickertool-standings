package gormrepo

import (
	"context"
	"fmt"
	"sort"
	"time"

	"kickertool-ranking/internal/domain"
)

// withRankingTrends decorates an already canonical, sorted ranking. Keeping
// the comparison here (rather than in an HTTP handler) means annual and
// cumulative readers use the same source/correction snapshot and the same
// tie-break order as the rank itself.
func (r *Repository) withRankingTrends(ctx context.Context, ranking []domain.PlayerAggregate, year *int) ([]domain.PlayerAggregate, error) {
	if len(ranking) == 0 {
		return ranking, nil
	}

	tournaments, err := r.rankedQualifyingTournaments(ctx, year)
	if err != nil {
		return nil, err
	}
	if len(tournaments) == 0 {
		for index := range ranking {
			ranking[index].Trend = domain.RankingTrendNew
		}
		return ranking, nil
	}
	latest := tournaments[len(tournaments)-1]
	if latest.Date == nil {
		for index := range ranking {
			ranking[index].Trend = domain.RankingTrendNew
		}
		return ranking, nil
	}
	location, err := time.LoadLocation(domain.RankingLocation)
	if err != nil {
		return nil, fmt.Errorf("load ranking timezone: %w", err)
	}

	// The cutoff is exclusive. A correction whose effective Berlin date is
	// exactly the latest tournament boundary belongs to the current ranking,
	// never to this preceding snapshot.
	cutoff := berlinCalendarDay(latest.Date, location)
	previous, err := r.listRankingBeforeTournament(ctx, tournaments, latest, cutoff, year)
	if err != nil {
		return nil, fmt.Errorf("load ranking trend baseline: %w", err)
	}
	previousRanks := make(map[string]int, len(previous))
	for index, row := range previous {
		previousRanks[rankingIdentity(row)] = index + 1
	}
	for index := range ranking {
		currentRank := index + 1
		previousRank, found := previousRanks[rankingIdentity(ranking[index])]
		switch {
		case !found:
			ranking[index].Trend = domain.RankingTrendNew
		case currentRank < previousRank:
			ranking[index].Trend = domain.RankingTrendUp
		case currentRank > previousRank:
			ranking[index].Trend = domain.RankingTrendDown
		default:
			ranking[index].Trend = domain.RankingTrendSame
		}
	}
	return ranking, nil
}

func rankingIdentity(row domain.PlayerAggregate) string {
	return row.Source + "\x00" + row.PlayerKey
}

// rankedQualifyingTournaments chooses only included, completed tournaments
// with complete standings, then orders them chronologically. The Berlin
// calendar date is primary; start time (when present), source identity and
// persisted ID make same-day sections deterministic regardless of sync order.
func (r *Repository) rankedQualifyingTournaments(ctx context.Context, year *int) ([]TournamentModel, error) {
	tournaments, err := r.qualifiedRankingTournaments(ctx, year)
	if err != nil {
		return nil, err
	}
	if len(tournaments) == 0 {
		return []TournamentModel{}, nil
	}
	location, err := time.LoadLocation(domain.RankingLocation)
	if err != nil {
		return nil, fmt.Errorf("load ranking timezone: %w", err)
	}
	sort.Slice(tournaments, func(i, j int) bool {
		return rankingTournamentBefore(tournaments[i], tournaments[j], location)
	})
	return tournaments, nil
}

func rankingTournamentBefore(left, right TournamentModel, location *time.Location) bool {
	leftDay := berlinCalendarDay(left.Date, location)
	rightDay := berlinCalendarDay(right.Date, location)
	if leftDay.Before(rightDay) {
		return true
	}
	if leftDay.After(rightDay) {
		return false
	}
	leftMoment := tournamentChronologyMoment(left)
	rightMoment := tournamentChronologyMoment(right)
	if leftMoment != nil && rightMoment != nil && !leftMoment.Equal(*rightMoment) {
		return leftMoment.Before(*rightMoment)
	}
	if leftMoment != nil && rightMoment == nil {
		return true
	}
	if leftMoment == nil && rightMoment != nil {
		return false
	}
	if left.Source != right.Source {
		return left.Source < right.Source
	}
	if left.SourceKey != right.SourceKey {
		return left.SourceKey < right.SourceKey
	}
	return left.ID < right.ID
}

func tournamentChronologyMoment(value TournamentModel) *time.Time {
	if value.StartTime != nil {
		return value.StartTime
	}
	return value.Date
}

// listRankingBeforeTournament builds the immediate predecessor snapshot by
// removing exactly the newest qualified tournament. This differs from a
// date-only query: earlier sections on the same Berlin day remain part of the
// baseline. Corrections still use the strict calendar-date cutoff so a
// correction effective on the newest tournament day belongs only to current.
func (r *Repository) listRankingBeforeTournament(ctx context.Context, tournaments []TournamentModel, latest TournamentModel, cutoff time.Time, year *int) ([]domain.PlayerAggregate, error) {
	refs := make([]uint, 0, len(tournaments)-1)
	for _, tournament := range tournaments {
		if tournament.ID != latest.ID {
			refs = append(refs, tournament.ID)
		}
	}
	var rows []StandingModel
	if len(refs) > 0 {
		if err := r.db.WithContext(ctx).
			Where("tournament_ref IN ?", refs).
			Where("player_ref NOT IN (SELECT id FROM player_models WHERE merged_into_player_id IS NOT NULL)").
			Order("id ASC").Find(&rows).Error; err != nil {
			return nil, fmt.Errorf("list trend baseline standings: %w", err)
		}
	}
	corrections, err := r.activeCorrections(ctx, r.clock.Now(), year, &cutoff)
	if err != nil {
		return nil, fmt.Errorf("list trend baseline corrections: %w", err)
	}
	return r.aggregateRankingRows(ctx, rows, corrections)
}

func berlinCalendarDay(value *time.Time, location *time.Location) time.Time {
	if value == nil {
		return time.Time{}
	}
	local := value.In(location)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
}
