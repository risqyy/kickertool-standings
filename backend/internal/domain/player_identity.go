package domain

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const (
	MinPlayerDisplayNameLength = 2
	MaxPlayerDisplayNameLength = 120
)

// ValidatePlayerDisplayName applies the same normalization used for player
// identity and rejects input that cannot be represented as a useful display
// name. The returned display name is trimmed, while the key is always derived
// through NormalizePlayerName.
func ValidatePlayerDisplayName(name string) (displayName, nameKey string, err error) {
	if !utf8.ValidString(name) {
		return "", "", ErrInvalidPlayerName
	}
	if utf8.RuneCountInString(name) > MaxPlayerDisplayNameLength {
		return "", "", ErrPlayerNameTooLong
	}
	// Preserve the administrator's casing while applying the same NFC and
	// whitespace cleanup that defines the canonical identity.
	displayName = norm.NFC.String(strings.Join(strings.Fields(name), " "))
	nameKey = NormalizePlayerName(displayName)
	if nameKey == "" {
		return "", "", ErrInvalidPlayerName
	}
	hasLetterOrNumber := false
	for _, r := range displayName {
		// Tabs and line separators have already been normalized to spaces.
		// Other control characters are not valid names.
		if unicode.IsControl(r) && !unicode.IsSpace(r) {
			return "", "", ErrInvalidPlayerName
		}
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			hasLetterOrNumber = true
		}
	}
	if !hasLetterOrNumber {
		return "", "", ErrInvalidPlayerName
	}
	if utf8.RuneCountInString(displayName) < MinPlayerDisplayNameLength {
		return "", "", ErrPlayerNameTooShort
	}
	return displayName, nameKey, nil
}

var (
	ErrInvalidPlayerName  = &PlayerNameValidationError{Message: "player name is invalid"}
	ErrPlayerNameTooShort = &PlayerNameValidationError{Message: "player name is too short"}
	ErrPlayerNameTooLong  = &PlayerNameValidationError{Message: "player name is too long"}
)

type PlayerNameValidationError struct{ Message string }

func (e *PlayerNameValidationError) Error() string { return e.Message }

// NormalizePlayerName creates the canonical, name-based player identity.
// Unicode normalization preserves diacritics; only whitespace and case are
// normalized. An accented spelling therefore remains distinct from an
// unaccented spelling.
func NormalizePlayerName(name string) string {
	name = norm.NFC.String(strings.TrimSpace(name))
	name = strings.Join(strings.Fields(name), " ")
	return strings.ToLower(name)
}

func PlayerKey(name string) string {
	return NormalizePlayerName(name)
}
