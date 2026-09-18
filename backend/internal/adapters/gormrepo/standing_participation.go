package gormrepo

// Unknown games retain the existing completeness rules. An explicit zero is
// a non-participation record, not a played tournament, even if other metrics
// were supplied. Source rows and independent manual corrections are retained.
func contributingStandings(rows []StandingModel) []StandingModel {
	result := make([]StandingModel, 0, len(rows))
	for _, row := range rows {
		if row.Superseded || (row.GamesPlayed != nil && *row.GamesPlayed == 0) {
			continue
		}
		result = append(result, row)
	}
	return result
}
