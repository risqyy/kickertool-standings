package httpapi

import (
	"context"
	"strings"

	"kickertool-ranking/internal/domain"
	"kickertool-ranking/internal/ports"
)

type fakePlayerDirectory struct {
	profiles map[uint]domain.PlayerProfile
}

func (f fakePlayerDirectory) SearchPlayers(_ context.Context, query string) ([]domain.PlayerProfile, error) {
	var result []domain.PlayerProfile
	for _, profile := range f.profiles {
		if strings.Contains(strings.ToLower(profile.DisplayName), strings.ToLower(query)) {
			result = append(result, profile)
		}
	}
	return result, nil
}

func (f fakePlayerDirectory) GetPlayerProfile(_ context.Context, playerID uint) (domain.PlayerProfile, error) {
	profile, ok := f.profiles[playerID]
	if !ok {
		return domain.PlayerProfile{}, ports.ErrNotFound
	}
	return profile, nil
}

type fakePlayerMerger struct {
	result domain.MergeResult
}

func (f *fakePlayerMerger) MergePlayers(_ context.Context, sourcePlayerID, targetPlayerID uint, options domain.PlayerMergeOptions) (domain.MergeResult, error) {
	result := f.result
	result.SourcePlayerID = sourcePlayerID
	result.TargetPlayerID = targetPlayerID
	result.DryRun = options.DryRun
	return result, nil
}
