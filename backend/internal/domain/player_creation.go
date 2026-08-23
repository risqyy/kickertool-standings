package domain

import "time"

// PlayerCreationInput is deliberately small: player creation never accepts
// ranking values, source IDs, aliases, or tournament data.
type PlayerCreationInput struct {
	DisplayName   string
	Administrator string
}

// PlayerCreationResult describes both the insert and an idempotent name
// conflict. A conflict returns the active profile that owns the normalized
// name, including when that owner is the root of a merged player.
type PlayerCreationResult struct {
	Player        PlayerProfile
	Created       bool
	CreatedAt     time.Time
	Administrator string
	Origin        string
}
