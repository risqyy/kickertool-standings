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
	result      domain.MergeResult
	merges      []domain.PlayerMergeAudit
	undoPreview domain.PlayerMergeUndoPreview
	undoResult  domain.PlayerMergeUndoResult
	undoOptions domain.PlayerMergeUndoOptions
	listErr     error
	previewErr  error
	undoErr     error
	undoCalls   int
}

func (f *fakePlayerMerger) MergePlayers(_ context.Context, sourcePlayerID, targetPlayerID uint, options domain.PlayerMergeOptions) (domain.MergeResult, error) {
	result := f.result
	result.SourcePlayerID = sourcePlayerID
	result.TargetPlayerID = targetPlayerID
	result.DryRun = options.DryRun
	return result, nil
}

func (f *fakePlayerMerger) ListPlayerMerges(context.Context) ([]domain.PlayerMergeAudit, error) {
	return f.merges, f.listErr
}

func (f *fakePlayerMerger) PreviewPlayerMergeUndo(_ context.Context, mergeID uint) (domain.PlayerMergeUndoPreview, error) {
	if f.previewErr != nil {
		return domain.PlayerMergeUndoPreview{}, f.previewErr
	}
	result := f.undoPreview
	result.Merge.ID = mergeID
	return result, nil
}

func (f *fakePlayerMerger) UndoPlayerMerge(_ context.Context, mergeID uint, options domain.PlayerMergeUndoOptions) (domain.PlayerMergeUndoResult, error) {
	f.undoCalls++
	if f.undoErr != nil {
		return domain.PlayerMergeUndoResult{}, f.undoErr
	}
	f.undoOptions = options
	result := f.undoResult
	result.Merge.ID = mergeID
	return result, nil
}
