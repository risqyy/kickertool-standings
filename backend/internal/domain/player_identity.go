package domain

import (
	"strings"

	"golang.org/x/text/unicode/norm"
)

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
