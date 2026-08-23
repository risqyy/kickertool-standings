package gormrepo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"kickertool-ranking/internal/domain"
	"kickertool-ranking/internal/ports"
)

const manualPlayerOrigin = "manual"

// CreateManualPlayer creates only the player identity and its canonical alias.
// It intentionally does not create aggregate, standing, allocation, source
// identity, or tournament rows. The unique canonical and alias indexes make
// the transaction safe for concurrent identical requests; a losing request
// returns the active owner as a conflict result.
func (r *Repository) CreateManualPlayer(ctx context.Context, input domain.PlayerCreationInput) (result domain.PlayerCreationResult, err error) {
	displayName, nameKey, err := domain.ValidatePlayerDisplayName(input.DisplayName)
	if err != nil {
		return result, err
	}
	actor := strings.TrimSpace(input.Administrator)
	if actor == "" {
		actor = "admin"
	}
	now := r.clock.Now()
	var ownerID uint
	created := false
	for attempt := 0; attempt < 8; attempt++ {
		ownerID = 0
		created = false
		err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			owner, found, findErr := findPlayerOwnerByNameKey(tx, nameKey)
			if findErr != nil {
				return findErr
			}
			if found {
				ownerID = owner.ID
				return nil
			}

			model := PlayerModel{
				CanonicalNameKey: nameKey,
				DisplayName:      displayName,
				CreatedBy:        actor,
				Origin:           manualPlayerOrigin,
				LastSeenAt:       now,
				CreatedAt:        now,
				UpdatedAt:        now,
			}
			if createErr := tx.Create(&model).Error; createErr != nil {
				// A concurrent transaction may have committed the same key between
				// the lookup and insert. Resolve that winner instead of surfacing a
				// database-specific unique-index error.
				owner, found, findErr = findPlayerOwnerByNameKey(tx, nameKey)
				if findErr == nil && found {
					ownerID = owner.ID
					return nil
				}
				return fmt.Errorf("insert manual player %q: %w", nameKey, createErr)
			}
			if createErr := tx.Create(&PlayerNameAliasModel{NameKey: nameKey, DisplayName: displayName, PlayerID: model.ID, CreatedAt: now, UpdatedAt: now}).Error; createErr != nil {
				owner, found, findErr = findPlayerOwnerByNameKey(tx, nameKey)
				if findErr == nil && found && owner.ID != model.ID {
					ownerID = owner.ID
					return nil
				}
				return fmt.Errorf("insert manual player alias %q: %w", nameKey, createErr)
			}
			ownerID = model.ID
			created = true
			return nil
		})
		if err == nil || !isSQLiteBusyError(err) || ctx.Err() != nil {
			break
		}
		time.Sleep(time.Duration(attempt+1) * 5 * time.Millisecond)
	}
	if err != nil {
		return result, err
	}
	profile, err := r.GetPlayerProfile(ctx, ownerID)
	if err != nil {
		return result, err
	}
	result = domain.PlayerCreationResult{Player: profile, Created: created, CreatedAt: profile.CreatedAt, Administrator: profile.CreatedBy, Origin: profile.Origin}
	return result, nil
}

func isSQLiteBusyError(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "database is locked") || strings.Contains(message, "database table is locked") || strings.Contains(message, "sqlite_busy")
}

// findPlayerOwnerByNameKey checks aliases first because aliases are the
// complete identity surface after a merge. The canonical fallback repairs
// compatibility with databases created before alias backfilling.
func findPlayerOwnerByNameKey(tx *gorm.DB, nameKey string) (PlayerModel, bool, error) {
	var alias PlayerNameAliasModel
	err := tx.Where("name_key = ?", nameKey).First(&alias).Error
	if err == nil {
		root, rootErr := resolvePlayerRoot(tx, alias.PlayerID)
		if rootErr != nil {
			return PlayerModel{}, false, rootErr
		}
		return root, true, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return PlayerModel{}, false, fmt.Errorf("find player alias %q: %w", nameKey, err)
	}
	var player PlayerModel
	err = tx.Where("canonical_name_key = ?", nameKey).First(&player).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return PlayerModel{}, false, nil
	}
	if err != nil {
		return PlayerModel{}, false, fmt.Errorf("find player %q: %w", nameKey, err)
	}
	root, err := resolvePlayerRoot(tx, player.ID)
	if err != nil {
		return PlayerModel{}, false, err
	}
	return root, true, nil
}

var _ ports.PlayerCreator = (*Repository)(nil)
