package gormrepo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"gorm.io/gorm"
	"kickertool-ranking/internal/domain"
)

// PlayerStateFingerprint includes source rows, inclusion state and manual
// correction history. It lets an admin preview detect a crawl, inclusion
// change or merge that does not change the visible aggregate immediately.
func (r *Repository) PlayerStateFingerprint(ctx context.Context, playerID uint) (string, error) {
	root, err := r.rootPlayer(ctx, playerID)
	if err != nil {
		return "", err
	}
	return playerStateFingerprintTx(r.db.WithContext(ctx), root)
}

func playerStateFingerprintTx(tx *gorm.DB, root PlayerModel) (string, error) {
	var rows []StandingModel
	if err := tx.Where("player_ref = ?", root.ID).Order("id ASC").Find(&rows).Error; err != nil {
		return "", err
	}
	var corrections []ManualRankingCorrectionModel
	if err := tx.Where("player_ref = ?", root.ID).Order("id ASC").Find(&corrections).Error; err != nil {
		return "", err
	}
	var tournaments []TournamentModel
	ids := make([]uint, 0, len(rows))
	seen := make(map[uint]struct{}, len(rows))
	for _, row := range rows {
		if _, ok := seen[row.TournamentRef]; !ok {
			seen[row.TournamentRef] = struct{}{}
			ids = append(ids, row.TournamentRef)
		}
	}
	if len(ids) > 0 {
		if err := tx.Where("id IN ?", ids).Order("id ASC").Find(&tournaments).Error; err != nil {
			return "", err
		}
	}
	payload := struct {
		Player      PlayerModel
		Rows        []StandingModel
		Corrections []ManualRankingCorrectionModel
		Tournaments []TournamentModel
	}{Player: root, Rows: rows, Corrections: corrections, Tournaments: tournaments}
	serialized, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(serialized)
	return hex.EncodeToString(digest[:]), nil
}

func (r *Repository) GetPlayerProfile(ctx context.Context, playerID uint) (domain.PlayerProfile, error) {
	var requested PlayerModel
	if err := r.db.WithContext(ctx).First(&requested, playerID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.PlayerProfile{}, fmt.Errorf("player %d not found", playerID)
		}
		return domain.PlayerProfile{}, err
	}
	root, err := resolvePlayerRoot(r.db.WithContext(ctx), requested.ID)
	if err != nil {
		return domain.PlayerProfile{}, err
	}
	return r.profileForRoot(ctx, requested.ID, requested, root)
}

func (r *Repository) SearchPlayers(ctx context.Context, query string) ([]domain.PlayerProfile, error) {
	query = domain.PlayerKey(query)
	if query == "" {
		return []domain.PlayerProfile{}, nil
	}
	db := r.db.WithContext(ctx)
	var aliases []PlayerNameAliasModel
	pattern := "%" + query + "%"
	if err := db.Where("name_key LIKE ?", pattern).Limit(100).Find(&aliases).Error; err != nil {
		return nil, err
	}
	type candidate struct {
		profile domain.PlayerProfile
		score   int
		alias   string
	}
	candidates := make(map[uint]candidate)
	for _, alias := range aliases {
		root, err := resolvePlayerRoot(db, alias.PlayerID)
		if err != nil {
			return nil, err
		}
		profile, err := r.profileForRoot(ctx, root.ID, root, root)
		if err != nil {
			return nil, err
		}
		score := matchScore(alias.NameKey, query)
		current, exists := candidates[root.ID]
		if !exists || score < current.score || (score == current.score && alias.DisplayName < current.alias) {
			profile.MatchedAlias = ""
			if alias.NameKey != root.CanonicalNameKey {
				profile.MatchedAlias = alias.DisplayName
			}
			candidates[root.ID] = candidate{profile: profile, score: score, alias: alias.DisplayName}
		}
	}
	ordered := make([]candidate, 0, len(candidates))
	for _, item := range candidates {
		ordered = append(ordered, item)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].score != ordered[j].score {
			return ordered[i].score < ordered[j].score
		}
		if ordered[i].profile.DisplayName != ordered[j].profile.DisplayName {
			return ordered[i].profile.DisplayName < ordered[j].profile.DisplayName
		}
		return ordered[i].profile.ID < ordered[j].profile.ID
	})
	if len(ordered) > 15 {
		ordered = ordered[:15]
	}
	profiles := make([]domain.PlayerProfile, 0, len(ordered))
	for _, item := range ordered {
		profiles = append(profiles, item.profile)
	}
	return profiles, nil
}

func matchScore(nameKey, query string) int {
	if nameKey == query {
		return 0
	}
	if strings.HasPrefix(nameKey, query) {
		return 1
	}
	return 2
}

func (r *Repository) profileForRoot(ctx context.Context, requestedID uint, requested, root PlayerModel) (domain.PlayerProfile, error) {
	var aliases []PlayerNameAliasModel
	if err := r.db.WithContext(ctx).Where("player_id = ?", root.ID).Order("name_key ASC").Find(&aliases).Error; err != nil {
		return domain.PlayerProfile{}, err
	}
	profile := domain.PlayerProfile{
		ID: root.ID, RequestedPlayerID: requestedID, CanonicalNameKey: root.CanonicalNameKey,
		DisplayName: root.DisplayName, Active: root.MergedIntoPlayerID == nil && requested.MergedIntoPlayerID == nil,
		MergedIntoPlayerID:       requested.MergedIntoPlayerID,
		RankingCorrectionVersion: root.RankingCorrectionVersion,
		Aliases:                  make([]domain.PlayerAlias, 0, len(aliases)),
	}
	for _, alias := range aliases {
		profile.Aliases = append(profile.Aliases, domain.PlayerAlias{NameKey: alias.NameKey, DisplayName: alias.DisplayName})
	}
	aggregate, err := r.aggregateForPlayer(ctx, root)
	if err != nil {
		return domain.PlayerProfile{}, err
	}
	profile.Aggregate = aggregate
	return profile, nil
}

func (r *Repository) aggregateForPlayer(ctx context.Context, player PlayerModel) (domain.PlayerAggregate, error) {
	return aggregateForPlayerTx(r.db.WithContext(ctx), player, r.clock.Now())
}

func aggregateFromRows(rows []StandingModel, player PlayerModel) (domain.PlayerAggregate, error) {
	result := domain.PlayerAggregate{PlayerKey: player.CanonicalNameKey, PlayerName: player.DisplayName, RecalculatedAt: player.UpdatedAt}
	if len(rows) == 0 {
		return result, nil
	}
	tournaments := make(map[string]struct{})
	pointsAvailable, gamesAvailable, goalsAvailable := true, true, true
	var totalPoints int64
	totalGames, totalGoals := 0, 0
	for _, row := range rows {
		tournaments[row.TournamentID] = struct{}{}
		if row.PointsCents == nil {
			pointsAvailable = false
		} else {
			var err error
			totalPoints, err = addInt64Checked(totalPoints, *row.PointsCents)
			if err != nil {
				return domain.PlayerAggregate{}, err
			}
		}
		if row.GamesPlayed == nil {
			gamesAvailable = false
		} else {
			var err error
			totalGames, err = addIntChecked(totalGames, *row.GamesPlayed)
			if err != nil {
				return domain.PlayerAggregate{}, err
			}
		}
		if row.GoalDifference == nil {
			goalsAvailable = false
		} else {
			var err error
			totalGoals, err = addIntChecked(totalGoals, *row.GoalDifference)
			if err != nil {
				return domain.PlayerAggregate{}, err
			}
		}
	}
	result.TournamentCount = len(tournaments)
	result.PointsAvailable, result.GamesAvailable, result.GoalsAvailable = pointsAvailable, gamesAvailable, goalsAvailable
	if pointsAvailable {
		result.TotalPointsCents = &totalPoints
	}
	if gamesAvailable {
		result.GamesPlayed = &totalGames
	}
	if goalsAvailable {
		result.GoalDifference = &totalGoals
	}
	if pointsAvailable && gamesAvailable && totalGames > 0 {
		ppg := roundCents(totalPoints, int64(totalGames))
		result.PointsPerGameCents = &ppg
	}
	return result, nil
}
