package gormrepo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"kickertool-ranking/internal/domain"
	"kickertool-ranking/internal/ports"
)

const (
	manualCorrectionActive   = "active"
	manualCorrectionRevoked  = "revoked"
	manualCorrectionReplaced = "replaced"
)

func (r *Repository) PreviewManualRankingCorrection(ctx context.Context, input domain.ManualRankingCorrectionInput) (domain.ManualRankingCorrectionPreview, error) {
	input.Administrator = strings.TrimSpace(input.Administrator)
	if err := validateManualCorrectionInput(input); err != nil {
		return domain.ManualRankingCorrectionPreview{}, err
	}
	var requested PlayerModel
	if err := r.db.WithContext(ctx).First(&requested, input.PlayerID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.ManualRankingCorrectionPreview{}, fmt.Errorf("player not found: %w", ports.ErrNotFound)
		}
		return domain.ManualRankingCorrectionPreview{}, err
	}
	root, err := resolvePlayerRoot(r.db.WithContext(ctx), requested.ID)
	if err != nil {
		return domain.ManualRankingCorrectionPreview{}, err
	}
	before, err := r.aggregateForPlayer(ctx, root)
	if err != nil {
		return domain.ManualRankingCorrectionPreview{}, err
	}
	var superseded *domain.ManualRankingCorrection
	baseline := before
	if input.ReplaceCorrectionID != 0 {
		old, loadErr := r.loadActiveCorrection(r.db.WithContext(ctx), root.ID, input.ReplaceCorrectionID)
		if loadErr != nil {
			return domain.ManualRankingCorrectionPreview{}, loadErr
		}
		value := fromManualCorrectionModel(old)
		superseded = &value
		baseline, err = aggregateForPlayerTxWithout(r.db.WithContext(ctx), root, old.ID, r.clock.Now())
		if err != nil {
			return domain.ManualRankingCorrectionPreview{}, err
		}
	}
	correction := correctionFromInput(input, root)
	after, err := applyCorrectionSafely(baseline, correction, baseline.TournamentCount == 0)
	if err != nil {
		return domain.ManualRankingCorrectionPreview{}, err
	}
	if err := validateCorrectionResult(after); err != nil {
		return domain.ManualRankingCorrectionPreview{}, err
	}
	profile, err := r.profileForRoot(ctx, requested.ID, requested, root)
	if err != nil {
		return domain.ManualRankingCorrectionPreview{}, err
	}
	return domain.ManualRankingCorrectionPreview{Player: profile, Correction: correction, Before: before, After: after, ExpectedVersion: root.RankingCorrectionVersion, Superseded: superseded}, nil
}

func (r *Repository) CreateManualRankingCorrection(ctx context.Context, input domain.ManualRankingCorrectionInput, expectedVersion int64) (result domain.ManualRankingCorrectionChange, err error) {
	if input.ReplaceCorrectionID != 0 {
		return r.ReplaceManualRankingCorrection(ctx, input, input.ReplaceCorrectionID, expectedVersion, "")
	}
	return r.createManualRankingCorrection(ctx, input, expectedVersion, "")
}

// CreateManualRankingCorrectionWithFingerprint validates the complete source
// state in the same transaction as the correction CAS. This closes the race
// between an admin preview and a crawl/merge committing concurrently.
func (r *Repository) CreateManualRankingCorrectionWithFingerprint(ctx context.Context, input domain.ManualRankingCorrectionInput, expectedVersion int64, expectedFingerprint string) (domain.ManualRankingCorrectionChange, error) {
	if input.ReplaceCorrectionID != 0 {
		return r.ReplaceManualRankingCorrection(ctx, input, input.ReplaceCorrectionID, expectedVersion, expectedFingerprint)
	}
	return r.createManualRankingCorrection(ctx, input, expectedVersion, expectedFingerprint)
}

// ReplaceManualRankingCorrection atomically supersedes one active booking
// with a new immutable booking. The old row remains in history and is linked
// to the replacement; the fingerprint is checked inside the same transaction
// as the player-version CAS.
func (r *Repository) ReplaceManualRankingCorrection(ctx context.Context, input domain.ManualRankingCorrectionInput, replaceCorrectionID uint, expectedVersion int64, expectedFingerprint string) (result domain.ManualRankingCorrectionChange, err error) {
	input.ReplaceCorrectionID = replaceCorrectionID
	input.Administrator = strings.TrimSpace(input.Administrator)
	if err := validateManualCorrectionInput(input); err != nil {
		return result, err
	}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var requested PlayerModel
		if err := tx.First(&requested, input.PlayerID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("player not found: %w", ports.ErrNotFound)
			}
			return err
		}
		root, err := resolvePlayerRoot(tx, requested.ID)
		if err != nil {
			return err
		}
		if expectedFingerprint != "" {
			fingerprint, fingerprintErr := playerStateFingerprintTx(tx, root)
			if fingerprintErr != nil {
				return fingerprintErr
			}
			if fingerprint != expectedFingerprint {
				return ports.ErrVersionConflict
			}
		}
		if expectedVersion < 0 {
			expectedVersion = root.RankingCorrectionVersion
		}
		update := tx.Model(&PlayerModel{}).Where("id = ? AND ranking_correction_version = ? AND merged_into_player_id IS NULL", root.ID, expectedVersion)
		if err := update.Update("ranking_correction_version", gorm.Expr("ranking_correction_version + 1")).Error; err != nil {
			return fmt.Errorf("reserve correction version: %w", err)
		}
		if update.RowsAffected != 1 {
			return ports.ErrVersionConflict
		}
		old, err := r.loadActiveCorrection(tx, root.ID, replaceCorrectionID)
		if err != nil {
			return err
		}
		before, err := aggregateForPlayerTx(tx, root, r.clock.Now())
		if err != nil {
			return err
		}
		baseline, err := aggregateForPlayerTxWithout(tx, root, old.ID, r.clock.Now())
		if err != nil {
			return err
		}
		correction := correctionFromInput(input, root)
		after, err := applyCorrectionSafely(baseline, correction, baseline.TournamentCount == 0)
		if err != nil {
			return err
		}
		if err := validateCorrectionResult(after); err != nil {
			return err
		}
		model := toManualCorrectionModel(correction)
		model.SupersedesCorrectionID = &old.ID
		if err := tx.Create(&model).Error; err != nil {
			return fmt.Errorf("insert replacement ranking correction: %w", err)
		}
		now := r.clock.Now()
		if err := tx.Model(&old).Updates(map[string]any{"status": manualCorrectionReplaced, "replaced_by_correction_id": model.ID, "revision": gorm.Expr("revision + 1"), "version": gorm.Expr("version + 1")}).Error; err != nil {
			return fmt.Errorf("mark ranking correction replaced: %w", err)
		}
		old.Status = manualCorrectionReplaced
		old.ReplacedByCorrectionID = &model.ID
		old.Revision++
		old.Version++
		if err := appendCorrectionRevision(tx, old, manualCorrectionReplaced, input.Administrator, now, input.Reason); err != nil {
			return err
		}
		if err := appendCorrectionRevision(tx, model, "created", input.Administrator, now, ""); err != nil {
			return err
		}
		if err := r.recalculatePlayerAggregates(tx, root, now); err != nil {
			return err
		}
		oldValue := fromManualCorrectionModel(old)
		result = domain.ManualRankingCorrectionChange{Correction: fromManualCorrectionModel(model), Before: before, After: after, Version: expectedVersion + 1, Superseded: &oldValue}
		return nil
	})
	return result, err
}

func (r *Repository) loadActiveCorrection(tx *gorm.DB, playerID, correctionID uint) (ManualRankingCorrectionModel, error) {
	var model ManualRankingCorrectionModel
	if err := tx.Where("id = ? AND player_ref = ?", correctionID, playerID).First(&model).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model, fmt.Errorf("correction not found: %w", ports.ErrNotFound)
		}
		return model, fmt.Errorf("load correction: %w", err)
	}
	if model.Status != manualCorrectionActive {
		return model, fmt.Errorf("correction is no longer active: %w", ports.ErrVersionConflict)
	}
	return model, nil
}

func (r *Repository) createManualRankingCorrection(ctx context.Context, input domain.ManualRankingCorrectionInput, expectedVersion int64, expectedFingerprint string) (result domain.ManualRankingCorrectionChange, err error) {
	input.Administrator = strings.TrimSpace(input.Administrator)
	if err := validateManualCorrectionInput(input); err != nil {
		return result, err
	}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var requested PlayerModel
		if err := tx.First(&requested, input.PlayerID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("player not found: %w", ports.ErrNotFound)
			}
			return err
		}
		root, err := resolvePlayerRoot(tx, requested.ID)
		if err != nil {
			return err
		}
		if expectedFingerprint != "" {
			fingerprint, fingerprintErr := playerStateFingerprintTx(tx, root)
			if fingerprintErr != nil {
				return fingerprintErr
			}
			if fingerprint != expectedFingerprint {
				return ports.ErrVersionConflict
			}
		}
		// The compare-and-swap is the concurrency boundary for both preview
		// confirmation and two administrators confirming simultaneously.
		if expectedVersion < 0 {
			expectedVersion = root.RankingCorrectionVersion
		}
		update := tx.Model(&PlayerModel{}).Where("id = ? AND ranking_correction_version = ? AND merged_into_player_id IS NULL", root.ID, expectedVersion)
		if err := update.Update("ranking_correction_version", gorm.Expr("ranking_correction_version + 1")).Error; err != nil {
			return fmt.Errorf("reserve correction version: %w", err)
		}
		if update.RowsAffected != 1 {
			return ports.ErrVersionConflict
		}
		before, err := aggregateForPlayerTx(tx, root, r.clock.Now())
		if err != nil {
			return err
		}
		correction := correctionFromInput(input, root)
		after, err := applyCorrectionSafely(before, correction, before.TournamentCount == 0)
		if err != nil {
			return err
		}
		if err := validateCorrectionResult(after); err != nil {
			return err
		}
		model := toManualCorrectionModel(correction)
		if err := tx.Create(&model).Error; err != nil {
			return fmt.Errorf("insert manual ranking correction: %w", err)
		}
		if err := appendCorrectionRevision(tx, model, "created", input.Administrator, r.clock.Now(), ""); err != nil {
			return err
		}
		if err := r.recalculatePlayerAggregates(tx, root, r.clock.Now()); err != nil {
			return err
		}
		result = domain.ManualRankingCorrectionChange{Correction: fromManualCorrectionModel(model), Before: before, After: after, Version: expectedVersion + 1}
		return nil
	})
	return result, err
}

func (r *Repository) ListManualRankingCorrections(ctx context.Context, playerID uint) ([]domain.ManualRankingCorrection, error) {
	root, err := r.rootPlayer(ctx, playerID)
	if err != nil {
		return nil, err
	}
	var models []ManualRankingCorrectionModel
	if err := r.db.WithContext(ctx).Where("player_ref = ?", root.ID).Order("effective_date ASC").Order("id ASC").Find(&models).Error; err != nil {
		return nil, fmt.Errorf("list ranking corrections: %w", err)
	}
	result := make([]domain.ManualRankingCorrection, 0, len(models))
	for _, model := range models {
		result = append(result, fromManualCorrectionModel(model))
	}
	return result, nil
}

func (r *Repository) RevokeManualRankingCorrection(ctx context.Context, playerID, correctionID uint, expectedVersion int64, administrator, reason string) (result domain.ManualRankingCorrectionRevocation, err error) {
	administrator = strings.TrimSpace(administrator)
	reason = strings.TrimSpace(reason)
	if administrator == "" {
		return result, errors.New("administrator is required")
	}
	if len([]rune(reason)) < 3 || len([]rune(reason)) > 500 {
		return result, errors.New("revocation reason must contain 3 to 500 characters")
	}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		root, err := resolvePlayerRoot(tx, playerID)
		if err != nil {
			return err
		}
		var model ManualRankingCorrectionModel
		if err := tx.Where("id = ? AND player_ref = ?", correctionID, root.ID).First(&model).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("correction not found: %w", ports.ErrNotFound)
			}
			return err
		}
		if model.Status != manualCorrectionActive {
			return ports.ErrVersionConflict
		}
		if expectedVersion < 0 {
			expectedVersion = root.RankingCorrectionVersion
		}
		update := tx.Model(&PlayerModel{}).Where("id = ? AND ranking_correction_version = ? AND merged_into_player_id IS NULL", root.ID, expectedVersion)
		if err := update.Update("ranking_correction_version", gorm.Expr("ranking_correction_version + 1")).Error; err != nil {
			return fmt.Errorf("reserve correction version: %w", err)
		}
		if update.RowsAffected != 1 {
			return ports.ErrVersionConflict
		}
		before, err := aggregateForPlayerTx(tx, root, r.clock.Now())
		if err != nil {
			return err
		}
		now := r.clock.Now()
		if err := tx.Model(&model).Updates(map[string]any{"status": manualCorrectionRevoked, "revoked_at": now, "revoked_by": administrator, "revocation_reason": reason, "revision": gorm.Expr("revision + 1"), "version": gorm.Expr("version + 1")}).Error; err != nil {
			return fmt.Errorf("revoke ranking correction: %w", err)
		}
		model.Status = manualCorrectionRevoked
		model.RevokedAt = &now
		model.RevokedBy = administrator
		model.RevocationReason = reason
		model.Revision++
		model.Version++
		if err := appendCorrectionRevision(tx, model, "revoked", administrator, now, reason); err != nil {
			return err
		}
		if err := r.recalculatePlayerAggregates(tx, root, now); err != nil {
			return err
		}
		after, err := aggregateForPlayerTx(tx, root, r.clock.Now())
		if err != nil {
			return err
		}
		result = domain.ManualRankingCorrectionRevocation{Correction: fromManualCorrectionModel(model), Before: before, After: after, Version: expectedVersion + 1}
		return nil
	})
	return result, err
}

func (r *Repository) rootPlayer(ctx context.Context, playerID uint) (PlayerModel, error) {
	var requested PlayerModel
	if err := r.db.WithContext(ctx).First(&requested, playerID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return PlayerModel{}, fmt.Errorf("player not found: %w", ports.ErrNotFound)
		}
		return PlayerModel{}, err
	}
	return resolvePlayerRoot(r.db.WithContext(ctx), requested.ID)
}

func aggregateForPlayerTx(tx *gorm.DB, player PlayerModel, cutoffs ...time.Time) (domain.PlayerAggregate, error) {
	return aggregateForPlayerTxWithout(tx, player, 0, cutoffs...)
}

func aggregateForPlayerTxWithout(tx *gorm.DB, player PlayerModel, excludedCorrectionID uint, cutoffs ...time.Time) (domain.PlayerAggregate, error) {
	var rows []StandingModel
	if err := tx.Joins("JOIN tournament_models ON tournament_models.id = standing_models.tournament_ref").Where("standing_models.player_ref = ? AND tournament_models.included_in_ranking = ?", player.ID, true).Find(&rows).Error; err != nil {
		return domain.PlayerAggregate{}, err
	}
	var corrections []ManualRankingCorrectionModel
	correctionQuery := tx.Where("player_ref = ? AND status = ?", player.ID, manualCorrectionActive)
	if len(cutoffs) > 0 && !cutoffs[0].IsZero() {
		correctionQuery = correctionQuery.Where("effective_date <= ?", cutoffs[0])
	}
	if err := correctionQuery.Order("id ASC").Find(&corrections).Error; err != nil {
		return domain.PlayerAggregate{}, err
	}
	result, err := aggregateFromRows(rows, player)
	if err != nil {
		return domain.PlayerAggregate{}, err
	}
	for _, correction := range corrections {
		if correction.ID == excludedCorrectionID {
			continue
		}
		result, err = applyCorrectionSafely(result, fromManualCorrectionModel(correction), len(rows) == 0)
		if err != nil {
			return domain.PlayerAggregate{}, err
		}
	}
	return result, nil
}

func applyCorrectionSafely(before domain.PlayerAggregate, correction domain.ManualRankingCorrection, correctionsOnly bool) (domain.PlayerAggregate, error) {
	after := before
	var err error
	after.TournamentCount, err = addIntChecked(after.TournamentCount, correction.TournamentCountDelta)
	if err != nil {
		return domain.PlayerAggregate{}, err
	}
	if before.GamesAvailable || correctionsOnly {
		value := 0
		if before.GamesPlayed != nil {
			value = *before.GamesPlayed
		}
		value, err = addIntChecked(value, correction.GamesPlayedDelta)
		if err != nil {
			return domain.PlayerAggregate{}, err
		}
		after.GamesPlayed = &value
		after.GamesAvailable = true
	}
	if before.PointsAvailable || correctionsOnly {
		value := int64(0)
		if before.TotalPointsCents != nil {
			value = *before.TotalPointsCents
		}
		value, err = addInt64Checked(value, correction.PointsCentsDelta)
		if err != nil {
			return domain.PlayerAggregate{}, err
		}
		after.TotalPointsCents = &value
		after.PointsAvailable = true
	}
	if before.GoalsAvailable || correctionsOnly {
		value := 0
		if before.GoalDifference != nil {
			value = *before.GoalDifference
		}
		value, err = addIntChecked(value, correction.GoalDifferenceDelta)
		if err != nil {
			return domain.PlayerAggregate{}, err
		}
		after.GoalDifference = &value
		after.GoalsAvailable = true
	}
	if after.PointsAvailable && after.GamesAvailable && after.TotalPointsCents != nil && after.GamesPlayed != nil && *after.GamesPlayed > 0 {
		value := roundCents(*after.TotalPointsCents, int64(*after.GamesPlayed))
		after.PointsPerGameCents = &value
	} else {
		after.PointsPerGameCents = nil
	}
	return after, nil
}

func (r *Repository) recalculatePlayerAggregates(tx *gorm.DB, player PlayerModel, now time.Time) error {
	var sources []string
	if err := tx.Model(&StandingModel{}).Where("player_ref = ?", player.ID).Distinct("source").Pluck("source", &sources).Error; err != nil {
		return fmt.Errorf("find ranking correction sources: %w", err)
	}
	var aggregateSources []string
	if err := tx.Model(&PlayerAggregateModel{}).Where("player_ref = ?", player.ID).Distinct("source").Pluck("source", &aggregateSources).Error; err != nil {
		return fmt.Errorf("find materialized correction sources: %w", err)
	}
	seen := make(map[string]struct{}, len(sources)+len(aggregateSources))
	for _, source := range append(sources, aggregateSources...) {
		if source != "" {
			seen[source] = struct{}{}
		}
	}
	if len(seen) == 0 {
		seen["manual_correction"] = struct{}{}
	}
	for source := range seen {
		if err := r.recalculateAggregate(tx, source, player.CanonicalNameKey, now); err != nil {
			return err
		}
	}
	return nil
}

// ListPlayerRankingBefore returns the ranking represented immediately before
// an exclusive snapshot boundary. It shares the same additive correction
// semantics as the current/year readers and is intentionally a repository
// method so audit comparisons cannot accidentally mutate source standings.
func (r *Repository) ListPlayerRankingBefore(ctx context.Context, cutoff time.Time) ([]domain.PlayerAggregate, error) {
	return r.listPlayerRankingBefore(ctx, cutoff, nil)
}

// listPlayerRankingBefore is also used by the public trend read model. The
// optional year keeps an annual baseline inside the selected calendar year;
// without it, a December annual ranking could accidentally compare against
// cumulative history from earlier years.
func (r *Repository) listPlayerRankingBefore(ctx context.Context, cutoff time.Time, year *int) ([]domain.PlayerAggregate, error) {
	tournaments, err := r.qualifiedRankingTournaments(ctx, year)
	if err != nil {
		return nil, err
	}
	refs := make([]uint, 0, len(tournaments))
	for _, tournament := range tournaments {
		if tournament.Date != nil && tournament.Date.Before(cutoff) {
			refs = append(refs, tournament.ID)
		}
	}
	var rows []StandingModel
	if len(refs) > 0 {
		if err := r.db.WithContext(ctx).Where("tournament_ref IN ?", refs).Where("player_ref NOT IN (SELECT id FROM player_models WHERE merged_into_player_id IS NOT NULL)").Order("id ASC").Find(&rows).Error; err != nil {
			return nil, fmt.Errorf("list snapshot standings: %w", err)
		}
	}
	corrections, err := r.activeCorrections(ctx, r.clock.Now(), year, &cutoff)
	if err != nil {
		return nil, fmt.Errorf("list snapshot corrections: %w", err)
	}
	return r.aggregateRankingRows(ctx, rows, corrections)
}

// ListPlayerRankingAt is a named alias for callers that use an inclusive
// current snapshot: callers pass the next boundary; corrections at that
// instant are therefore included in the current result.
func (r *Repository) ListPlayerRankingAt(ctx context.Context, boundary time.Time) ([]domain.PlayerAggregate, error) {
	return r.ListPlayerRankingBefore(ctx, boundary.Add(time.Nanosecond))
}

func validateManualCorrectionInput(input domain.ManualRankingCorrectionInput) error {
	if input.PlayerID == 0 || input.EffectiveDate.IsZero() {
		return errors.New("player and effective date are required")
	}
	if input.TournamentCountDelta == 0 && input.GamesPlayedDelta == 0 && input.PointsCentsDelta == 0 && input.GoalDifferenceDelta == 0 {
		return errors.New("at least one correction delta is required")
	}
	const maxCountDelta = int64(1_000_000_000)
	const maxPointsDelta = int64(9_000_000_000_000_000)
	if int64(input.TournamentCountDelta) > maxCountDelta || int64(input.TournamentCountDelta) < -maxCountDelta || int64(input.GamesPlayedDelta) > maxCountDelta || int64(input.GamesPlayedDelta) < -maxCountDelta || int64(input.GoalDifferenceDelta) > maxCountDelta || int64(input.GoalDifferenceDelta) < -maxCountDelta {
		return errors.New("correction count deltas exceed the safe limit")
	}
	if input.PointsCentsDelta > maxPointsDelta || input.PointsCentsDelta < -maxPointsDelta {
		return errors.New("points delta exceeds the safe limit")
	}
	if len([]rune(strings.TrimSpace(input.Reason))) < 3 || len([]rune(strings.TrimSpace(input.Reason))) > 500 {
		return errors.New("reason must contain 3 to 500 characters")
	}
	if len([]rune(strings.TrimSpace(input.Administrator))) == 0 {
		return errors.New("administrator is required")
	}
	return nil
}

func validateCorrectionResult(value domain.PlayerAggregate) error {
	if value.TournamentCount < 0 {
		return errors.New("tournament count cannot become negative")
	}
	if value.GamesPlayed != nil && *value.GamesPlayed < 0 {
		return errors.New("games played cannot become negative")
	}
	return nil
}

func correctionFromInput(input domain.ManualRankingCorrectionInput, player PlayerModel) domain.ManualRankingCorrection {
	location, _ := time.LoadLocation(domain.RankingLocation)
	date := input.EffectiveDate
	if location != nil {
		// Effective dates are calendar dates, not arbitrary instants. Normalize
		// direct repository callers to the start of that date in Berlin so the
		// same cutoff semantics apply as for the HTTP YYYY-MM-DD parser.
		local := date.In(location)
		date = time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
	}
	return domain.ManualRankingCorrection{PlayerID: player.ID, PlayerKey: player.CanonicalNameKey, PlayerName: player.DisplayName, EffectiveDate: date, EffectiveYear: date.Year(), TournamentCountDelta: input.TournamentCountDelta, GamesPlayedDelta: input.GamesPlayedDelta, PointsCentsDelta: input.PointsCentsDelta, GoalDifferenceDelta: input.GoalDifferenceDelta, Reason: strings.TrimSpace(input.Reason), Administrator: strings.TrimSpace(input.Administrator), Status: manualCorrectionActive, Revision: 1, Version: 1}
}

func toManualCorrectionModel(value domain.ManualRankingCorrection) ManualRankingCorrectionModel {
	return ManualRankingCorrectionModel{ID: value.ID, PlayerRef: value.PlayerID, PlayerKey: value.PlayerKey, EffectiveDate: value.EffectiveDate, EffectiveYear: value.EffectiveYear, TournamentCountDelta: value.TournamentCountDelta, GamesPlayedDelta: value.GamesPlayedDelta, PointsCentsDelta: value.PointsCentsDelta, GoalDifferenceDelta: value.GoalDifferenceDelta, Reason: value.Reason, Administrator: value.Administrator, CreatedAt: value.CreatedAt, Status: value.Status, RevokedAt: value.RevokedAt, RevokedBy: value.RevokedBy, RevocationReason: value.RevocationReason, Revision: value.Revision, Version: value.Version, SupersedesCorrectionID: value.SupersedesCorrectionID, ReplacedByCorrectionID: value.ReplacedByCorrectionID}
}

func fromManualCorrectionModel(value ManualRankingCorrectionModel) domain.ManualRankingCorrection {
	return domain.ManualRankingCorrection{ID: value.ID, PlayerID: value.PlayerRef, PlayerKey: value.PlayerKey, EffectiveDate: value.EffectiveDate, EffectiveYear: value.EffectiveYear, TournamentCountDelta: value.TournamentCountDelta, GamesPlayedDelta: value.GamesPlayedDelta, PointsCentsDelta: value.PointsCentsDelta, GoalDifferenceDelta: value.GoalDifferenceDelta, Reason: value.Reason, Administrator: value.Administrator, CreatedAt: value.CreatedAt, Status: value.Status, RevokedAt: value.RevokedAt, RevokedBy: value.RevokedBy, RevocationReason: value.RevocationReason, Revision: value.Revision, Version: value.Version, SupersedesCorrectionID: value.SupersedesCorrectionID, ReplacedByCorrectionID: value.ReplacedByCorrectionID}
}

func appendCorrectionRevision(tx *gorm.DB, correction ManualRankingCorrectionModel, action, administrator string, occurredAt time.Time, reason string) error {
	var previous ManualRankingCorrectionRevisionModel
	previousErr := tx.Where("correction_id = ?", correction.ID).Order("revision DESC").First(&previous).Error
	if previousErr != nil && !errors.Is(previousErr, gorm.ErrRecordNotFound) {
		return fmt.Errorf("load previous correction revision: %w", previousErr)
	}
	previousHash := previous.Hash
	data := strings.Join([]string{strconv.FormatUint(uint64(correction.ID), 10), strconv.FormatInt(correction.Revision, 10), action, correction.EffectiveDate.UTC().Format(time.RFC3339Nano), strconv.Itoa(correction.TournamentCountDelta), strconv.Itoa(correction.GamesPlayedDelta), strconv.FormatInt(correction.PointsCentsDelta, 10), strconv.Itoa(correction.GoalDifferenceDelta), correction.Reason, administrator, reason, occurredAt.UTC().Format(time.RFC3339Nano), previousHash}, "|")
	digest := sha256.Sum256([]byte(data))
	revision := ManualRankingCorrectionRevisionModel{CorrectionID: correction.ID, Revision: correction.Revision, Action: action, EffectiveDate: correction.EffectiveDate, TournamentCountDelta: correction.TournamentCountDelta, GamesPlayedDelta: correction.GamesPlayedDelta, PointsCentsDelta: correction.PointsCentsDelta, GoalDifferenceDelta: correction.GoalDifferenceDelta, Reason: reasonOrOriginal(reason, correction.Reason), Administrator: administrator, OccurredAt: occurredAt, PreviousHash: previousHash, Hash: hex.EncodeToString(digest[:])}
	if err := tx.Create(&revision).Error; err != nil {
		return fmt.Errorf("write correction revision: %w", err)
	}
	return nil
}

func reasonOrOriginal(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}
