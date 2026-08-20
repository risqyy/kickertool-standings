package gormrepo

import (
	"context"
	"fmt"
	"time"

	"kickertool-ranking/internal/domain"
)

// rankingState is the shared aggregation state for current, annual and
// snapshot readers. Source standings remain the base; corrections are added
// exactly once per player/source state and never written into a standing row.
type rankingState struct {
	source                                          string
	player                                          PlayerModel
	tournaments                                     map[string]struct{}
	rows, correctionCount                           int
	tournamentDelta                                 int
	totalPointsCents                                int64
	pointsAvailable, gamesAvailable, goalsAvailable bool
	gamesPlayed, goalDifference                     int
}

func (r *Repository) listCurrentRanking(ctx context.Context, now time.Time) ([]domain.PlayerAggregate, error) {
	var rows []StandingModel
	if err := r.db.WithContext(ctx).
		Joins("JOIN tournament_models ON tournament_models.id = standing_models.tournament_ref").
		Where("tournament_models.included_in_ranking = ?", true).
		Where("standing_models.player_ref NOT IN (SELECT id FROM player_models WHERE merged_into_player_id IS NOT NULL)").
		Order("standing_models.id ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list current ranking standings: %w", err)
	}
	corrections, err := r.activeCorrections(ctx, now, nil, nil)
	if err != nil {
		return nil, err
	}
	return r.aggregateRankingRows(ctx, rows, corrections)
}

func (r *Repository) activeCorrections(ctx context.Context, now time.Time, year *int, before *time.Time) ([]ManualRankingCorrectionModel, error) {
	query := r.db.WithContext(ctx).Where("status = ? AND effective_date <= ?", manualCorrectionActive, now)
	if year != nil {
		query = query.Where("effective_year = ?", *year)
	}
	if before != nil {
		query = query.Where("effective_date < ?", *before)
	}
	var corrections []ManualRankingCorrectionModel
	if err := query.Order("id ASC").Find(&corrections).Error; err != nil {
		return nil, fmt.Errorf("list active ranking corrections: %w", err)
	}
	return corrections, nil
}

func (r *Repository) aggregateRankingRows(ctx context.Context, rows []StandingModel, corrections []ManualRankingCorrectionModel) ([]domain.PlayerAggregate, error) {
	playerIDs := make(map[uint]struct{}, len(rows)+len(corrections))
	for _, row := range rows {
		playerIDs[row.PlayerRef] = struct{}{}
	}
	for _, correction := range corrections {
		playerIDs[correction.PlayerRef] = struct{}{}
	}
	players := make([]PlayerModel, 0, len(playerIDs))
	if len(playerIDs) > 0 {
		ids := make([]uint, 0, len(playerIDs))
		for id := range playerIDs {
			ids = append(ids, id)
		}
		if err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&players).Error; err != nil {
			return nil, fmt.Errorf("load ranking players: %w", err)
		}
	}
	playerByID := make(map[uint]PlayerModel, len(players))
	for _, player := range players {
		playerByID[player.ID] = player
	}
	states := make(map[string]*rankingState)
	sourcesByPlayer := make(map[uint]map[string]struct{})
	var err error
	for _, row := range rows {
		player, ok := playerByID[row.PlayerRef]
		if !ok || player.MergedIntoPlayerID != nil {
			continue
		}
		key := row.Source + "\x00" + player.CanonicalNameKey
		state := states[key]
		if state == nil {
			state = &rankingState{source: row.Source, player: player, tournaments: make(map[string]struct{}), pointsAvailable: true, gamesAvailable: true, goalsAvailable: true}
			states[key] = state
		}
		state.rows, err = addIntChecked(state.rows, 1)
		if err != nil {
			return nil, err
		}
		state.tournaments[row.Source+"\x00"+row.TournamentID] = struct{}{}
		if row.PointsCents == nil {
			state.pointsAvailable = false
		} else {
			state.totalPointsCents, err = addInt64Checked(state.totalPointsCents, *row.PointsCents)
			if err != nil {
				return nil, err
			}
		}
		if row.GamesPlayed == nil {
			state.gamesAvailable = false
		} else {
			state.gamesPlayed, err = addIntChecked(state.gamesPlayed, *row.GamesPlayed)
			if err != nil {
				return nil, err
			}
		}
		if row.GoalDifference == nil {
			state.goalsAvailable = false
		} else {
			state.goalDifference, err = addIntChecked(state.goalDifference, *row.GoalDifference)
			if err != nil {
				return nil, err
			}
		}
		if sourcesByPlayer[player.ID] == nil {
			sourcesByPlayer[player.ID] = make(map[string]struct{})
		}
		sourcesByPlayer[player.ID][row.Source] = struct{}{}
	}
	for _, correction := range corrections {
		player, ok := playerByID[correction.PlayerRef]
		if !ok || player.MergedIntoPlayerID != nil {
			continue
		}
		sources := sourcesByPlayer[player.ID]
		if len(sources) == 0 {
			sources = map[string]struct{}{"manual_correction": {}}
		}
		for source := range sources {
			key := source + "\x00" + player.CanonicalNameKey
			state := states[key]
			if state == nil {
				state = &rankingState{source: source, player: player, tournaments: make(map[string]struct{}), pointsAvailable: true, gamesAvailable: true, goalsAvailable: true}
				states[key] = state
			}
			state.correctionCount, err = addIntChecked(state.correctionCount, 1)
			if err != nil {
				return nil, err
			}
			state.tournamentDelta, err = addIntChecked(state.tournamentDelta, correction.TournamentCountDelta)
			if err != nil {
				return nil, err
			}
			if state.rows == 0 || state.pointsAvailable {
				state.totalPointsCents, err = addInt64Checked(state.totalPointsCents, correction.PointsCentsDelta)
				if err != nil {
					return nil, err
				}
			}
			if state.rows == 0 || state.gamesAvailable {
				state.gamesPlayed, err = addIntChecked(state.gamesPlayed, correction.GamesPlayedDelta)
				if err != nil {
					return nil, err
				}
			}
			if state.rows == 0 || state.goalsAvailable {
				state.goalDifference, err = addIntChecked(state.goalDifference, correction.GoalDifferenceDelta)
				if err != nil {
					return nil, err
				}
			}
		}
	}
	result := make([]domain.PlayerAggregate, 0, len(states))
	for _, state := range states {
		count, err := addIntChecked(len(state.tournaments), state.tournamentDelta)
		if err != nil {
			return nil, err
		}
		if count <= 0 {
			continue
		}
		aggregate := domain.PlayerAggregate{Source: state.source, PlayerKey: state.player.CanonicalNameKey, PlayerName: state.player.DisplayName, TournamentCount: count, PointsAvailable: state.pointsAvailable && (state.rows > 0 || state.correctionCount > 0), GamesAvailable: state.gamesAvailable, GoalsAvailable: state.goalsAvailable}
		if aggregate.PointsAvailable {
			value := state.totalPointsCents
			aggregate.TotalPointsCents = &value
		}
		if aggregate.GamesAvailable {
			value := state.gamesPlayed
			aggregate.GamesPlayed = &value
		}
		if aggregate.GoalsAvailable {
			value := state.goalDifference
			aggregate.GoalDifference = &value
		}
		if aggregate.PointsAvailable && aggregate.GamesAvailable && aggregate.TotalPointsCents != nil && aggregate.GamesPlayed != nil && *aggregate.GamesPlayed > 0 {
			value := roundCents(*aggregate.TotalPointsCents, int64(*aggregate.GamesPlayed))
			aggregate.PointsPerGameCents = &value
		}
		result = append(result, aggregate)
	}
	sortPlayerRanking(result)
	return result, nil
}

// listRankingRowsForYear is shared by the public annual reader and tests. It
// intentionally uses the same qualifying tournament rules as available years.
func (r *Repository) listRankingRowsForYear(ctx context.Context, year int) ([]StandingModel, error) {
	tournaments, err := r.qualifiedRankingTournaments(ctx, &year)
	if err != nil {
		return nil, err
	}
	if len(tournaments) == 0 {
		return []StandingModel{}, nil
	}
	refs := make([]uint, 0, len(tournaments))
	for _, tournament := range tournaments {
		refs = append(refs, tournament.ID)
	}
	var rows []StandingModel
	if err := r.db.WithContext(ctx).Where("tournament_ref IN ?", refs).Where("player_ref NOT IN (SELECT id FROM player_models WHERE merged_into_player_id IS NOT NULL)").Order("id ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list player standings for year %d: %w", year, err)
	}
	return rows, nil
}
