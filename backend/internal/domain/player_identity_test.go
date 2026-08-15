package domain

import "testing"

func TestPlayerKeyNormalizesWhitespaceAndCaseButPreservesDiacritics(t *testing.T) {
	if got, want := PlayerKey("  Player One\t  Example "), "player one example"; got != want {
		t.Fatalf("key=%q want=%q", got, want)
	}
	if PlayerKey("Player Ä") == PlayerKey("Player A") {
		t.Fatal("diacritics must remain identity-significant")
	}
}
