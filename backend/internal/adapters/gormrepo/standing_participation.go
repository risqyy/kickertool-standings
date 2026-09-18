package gormrepo

import "kickertool-ranking/internal/domain"

func explicitZeroGames(games *int) bool { return games != nil && *games == 0 }

// A merged alias indicating non-participation cannot displace a contribution.
// Among equally participating aliases retain the canonical source name, then a
// stable key. Never sum duplicate identities or infer missing games as zero.
func preferAliasStanding(candidate, current domain.TournamentStanding, canonicalKey string) bool {
	if explicitZeroGames(candidate.GamesPlayed) != explicitZeroGames(current.GamesPlayed) {
		return !explicitZeroGames(candidate.GamesPlayed)
	}
	candidateCanonical := domain.PlayerKey(candidate.PlayerName) == canonicalKey
	currentCanonical := domain.PlayerKey(current.PlayerName) == canonicalKey
	if candidateCanonical != currentCanonical {
		return candidateCanonical
	}
	return candidate.StandingKey < current.StandingKey
}

// Unknown games retain the existing completeness rules. An explicit zero is
// a non-participation record, not a played tournament, even if other metrics
// were supplied. Source rows and independent manual corrections are retained.
func contributingStandings(rows []StandingModel) []StandingModel {
	result := make([]StandingModel, 0, len(rows))
	for _, row := range rows {
		if row.Superseded || explicitZeroGames(row.GamesPlayed) {
			continue
		}
		result = append(result, row)
	}
	return result
}
