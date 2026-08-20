package kickertoolhtml

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
	"kickertool-ranking/internal/domain"
)

type standingDocument struct {
	Rows          []domain.TournamentStanding
	Meta          standingsMeta
	CandidateURLs []string
	TablesFound   int
}

func parseStandingDocument(pageURL string, body []byte, inScope func(*url.URL) bool) (standingDocument, error) {
	result := standingDocument{}
	root, htmlErr := html.Parse(strings.NewReader(string(body)))
	if htmlErr == nil {
		rows, meta, err := parseStandings(pageURL, body)
		if err != nil {
			return result, err
		}
		result.Rows = append(result.Rows, rows...)
		result.Meta = mergeStandingsMeta(result.Meta, meta)
		result.TablesFound = countTables(root)
		result.CandidateURLs = append(result.CandidateURLs, discoverStandingLinks(pageURL, root, body, inScope)...)
	}
	if jsonResult, ok := parseJSONStandingDocument(pageURL, body, inScope); ok {
		result.Rows = append(result.Rows, jsonResult.Rows...)
		result.Meta = mergeStandingsMeta(result.Meta, jsonResult.Meta)
		result.CandidateURLs = append(result.CandidateURLs, jsonResult.CandidateURLs...)
	}
	result.CandidateURLs = dedupeStrings(result.CandidateURLs)
	return result, nil
}

func discoverStandingLinks(pageURL string, root *html.Node, body []byte, inScope func(*url.URL) bool) []string {
	page, err := url.Parse(pageURL)
	if err != nil {
		return nil
	}
	result := make([]string, 0)
	walk(root, func(node *html.Node) {
		if node.Type != html.ElementNode {
			return
		}
		if node.Data == "a" {
			if href := attr(node, "href"); href != "" {
				if resolved := resolveURL(page, href, inScope); resolved != "" && isStandingDiscoveryCandidate(resolved, nodeText(node), false) {
					result = append(result, resolved)
				}
			}
		}
		for _, key := range []string{"data-url", "data-href", "data-endpoint", "data-api-url", "data-standings-url", "data-group-url"} {
			if raw := attr(node, key); raw != "" {
				if resolved := resolveURL(page, raw, inScope); resolved != "" && isStandingDiscoveryCandidate(resolved, key, true) {
					result = append(result, resolved)
				}
			}
		}
	})
	for _, raw := range discoverEmbeddedURLs(string(body)) {
		if resolved := resolveURL(page, raw, inScope); resolved != "" && isStandingDiscoveryCandidate(resolved, "embedded endpoint", true) {
			result = append(result, resolved)
		}
	}
	return dedupeStrings(result)
}

func discoverEmbeddedURLs(body string) []string {
	pattern := regexp.MustCompile(`(?i)(?:https?://[^"'<>\s]+|/[^"'<>\s]+)`)
	return dedupeStrings(pattern.FindAllString(strings.ReplaceAll(body, `\/`, `/`), -1))
}

func isStandingDiscoveryCandidate(raw, hint string, explicit bool) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	path := strings.ToLower(parsed.Path)
	if strings.HasSuffix(path, ".js") || strings.HasSuffix(path, ".css") || strings.HasSuffix(path, ".map") || strings.HasSuffix(path, ".png") || strings.HasSuffix(path, ".svg") {
		return false
	}
	value := strings.ToLower(path + " " + parsed.RawQuery + " " + hint)
	for _, marker := range []string{"standings", "standing", "results", "result", "group", "groups", "discipline", "stage", "table", "ranking", "xhr", "ajax"} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return isTournamentURL(raw)
}

func countTables(root *html.Node) int {
	count := 0
	walk(root, func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "table" {
			count++
		}
	})
	return count
}

func entryTypeEvidence(root *html.Node, body []byte) (string, bool) {
	result, evidence, _ := competitionEvidence(root, body)
	return result, evidence
}

// competitionEvidence keeps the tournament category (nameType/entryType)
// separate from modes. A Monster-DYP category may expose a Whist mode too;
// the mode must not replace the category used by the ranking policy.
func competitionEvidence(root *html.Node, body []byte) (string, bool, []string) {
	var result string
	var evidence bool
	var modes []string
	walk(root, func(node *html.Node) {
		if node.Type != html.ElementNode {
			return
		}
		for _, key := range []string{"data-name-type", "data-nametype", "data-category", "data-entry-type", "data-entrytype", "data-discipline-type", "data-format", "name-type", "nametype", "name_type", "category", "entry-type", "entry_type", "discipline-type", "discipline_type"} {
			if value := normalizeEntryType(attr(node, key)); value != "" {
				if !evidence {
					result, evidence = value, true
				}
				break
			}
		}
		classAndID := strings.ToLower(attr(node, "class") + " " + attr(node, "id"))
		if !evidence && (strings.Contains(classAndID, "discipline") || strings.Contains(classAndID, "entry-type") || strings.Contains(classAndID, "name-type") || strings.Contains(classAndID, "nametype") || strings.Contains(classAndID, "category") || strings.Contains(classAndID, "format")) {
			if value := normalizeEntryType(nodeText(node)); value != "" {
				result, evidence = value, true
			}
		}
		for _, key := range []string{"data-modes", "data-mode", "data-tournament-mode", "modes", "mode", "tournament-mode", "tournament_mode"} {
			if value := attr(node, key); value != "" {
				modes = append(modes, normalizeModes(value)...)
			}
		}
		if strings.Contains(classAndID, "mode") {
			modes = append(modes, normalizeModes(nodeText(node))...)
		}
	})
	text := string(body)
	for _, pattern := range []string{
		`(?i)(?:name[_-]?type|category|entry[_-]?type|discipline[_-]?type|format)\s*[=:]\s*["']?([a-z][a-z _+/-]+)`,
		`(?i)"(?:nameType|name_type|category|entryType|entry_type|disciplineType|discipline_type)"\s*:\s*"([^"]+)"`,
	} {
		match := regexp.MustCompile(pattern).FindStringSubmatch(text)
		if len(match) == 2 {
			if value := normalizeEntryType(match[1]); value != "" {
				if !evidence {
					result, evidence = value, true
				}
			}
		}
	}
	for _, pattern := range []string{
		`(?i)"(?:modes|mode|tournamentMode|tournament_mode)"\s*:\s*(?:\[([^\]]*)\]|"([^"]+)"|([^,}\s]+))`,
		`(?i)(?:data-modes|data-mode|modes|mode|tournament-mode|tournament_mode)\s*=\s*["']([^"']+)`,
	} {
		for _, match := range regexp.MustCompile(pattern).FindAllStringSubmatch(text, -1) {
			for _, value := range match[1:] {
				if value != "" {
					modes = append(modes, normalizeModes(value)...)
				}
			}
		}
	}
	return result, evidence, dedupeStrings(modes)
}

func parseJSONStandingDocument(pageURL string, body []byte, inScope func(*url.URL) bool) (standingDocument, bool) {
	var value any
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return standingDocument{}, false
	}
	result := standingDocument{}
	walkJSON(value, func(object map[string]any) {
		if value, ok := firstJSONValue(object, "entryType", "entry_type", "disciplineType", "discipline_type", "format"); ok {
			if entryType := normalizeEntryType(jsonString(value)); entryType != "" {
				result.Meta.EntryType = entryType
				result.Meta.EntryTypeEvidence = true
			}
		}
		if value, ok := firstJSONValue(object, "nameType", "name_type", "category"); ok {
			if entryType := normalizeEntryType(jsonString(value)); entryType != "" {
				result.Meta.EntryType = entryType
				result.Meta.EntryTypeEvidence = true
			}
		}
		if value, ok := firstJSONValue(object, "modes", "mode", "tournamentMode", "tournament_mode"); ok {
			result.Meta.Modes = append(result.Meta.Modes, jsonModes(value)...)
			if len(result.Meta.Modes) > 0 {
				result.Meta.ModeEvidence = true
			}
		}
		if looksLikeJSONStanding(object) {
			result.Rows = append(result.Rows, standingFromJSON(object))
		}
		for key, raw := range object {
			keyLower := strings.ToLower(key)
			if !strings.Contains(keyLower, "url") && !strings.Contains(keyLower, "endpoint") && keyLower != "href" && keyLower != "path" {
				continue
			}
			if candidate := jsonString(raw); candidate != "" {
				if resolved := resolveURLFromString(pageURL, candidate, inScope); resolved != "" {
					result.CandidateURLs = append(result.CandidateURLs, resolved)
				}
			}
		}
	})
	result.CandidateURLs = dedupeStrings(result.CandidateURLs)
	result.Meta.Modes = dedupeStrings(result.Meta.Modes)
	return result, true
}

func jsonModes(value any) []string {
	switch typed := value.(type) {
	case []any:
		var result []string
		for _, item := range typed {
			result = append(result, jsonModes(item)...)
		}
		return result
	case map[string]any:
		var result []string
		for _, key := range []string{"name", "type", "mode", "value"} {
			if item, ok := typed[key]; ok {
				result = append(result, jsonModes(item)...)
			}
		}
		return result
	default:
		return normalizeModes(jsonString(value))
	}
}

func walkJSON(value any, visit func(map[string]any)) {
	switch typed := value.(type) {
	case map[string]any:
		visit(typed)
		for _, child := range typed {
			walkJSON(child, visit)
		}
	case []any:
		for _, child := range typed {
			walkJSON(child, visit)
		}
	}
}

func looksLikeJSONStanding(object map[string]any) bool {
	_, hasRank := firstJSONValue(object, "rank", "position", "place", "preliminary", "finalResult")
	_, hasEntry := firstJSONValue(object, "entry", "entryId", "entry_id", "player", "playerId", "player_id", "name", "playerName", "player_name")
	return hasRank && hasEntry
}

func standingFromJSON(object map[string]any) domain.TournamentStanding {
	standing := domain.TournamentStanding{
		StandingID:     jsonStringValue(object, "standingId", "standing_id", "resultId", "result_id", "id"),
		EntryID:        jsonNestedID(object, "entry", "entryId", "entry_id"),
		PlayerID:       jsonNestedID(object, "player", "playerId", "player_id"),
		PlayerName:     jsonNestedName(object, "player", "playerName", "player_name", "name"),
		EntryName:      jsonNestedName(object, "entry", "entryName", "entry_name"),
		Team:           jsonStringValue(object, "team", "teamName", "team_name"),
		Rank:           jsonIntPointer(object, "rank", "position", "place"),
		Result:         jsonIntPointer(object, "result"),
		Preliminary:    jsonIntPointer(object, "preliminary"),
		FinalResult:    jsonIntPointer(object, "finalResult", "final_result"),
		PointsCents:    jsonCentsPointer(object, "points", "pointsCents", "points_cents"),
		GamesPlayed:    jsonIntPointer(object, "matches", "games", "gamesPlayed", "games_played"),
		GoalDifference: jsonIntPointer(object, "goalDifference", "goal_difference", "goaldiff", "goalDiff"),
	}
	if standing.EntryID == "" {
		standing.EntryID = standing.PlayerID
	}
	if standing.EntryName == "" {
		standing.EntryName = standing.Team
	}
	if standing.EntryName == "" {
		standing.EntryName = standing.PlayerName
	}
	return standing
}

func jsonNestedID(object map[string]any, nested string, keys ...string) string {
	if value, ok := object[nested]; ok {
		if child, ok := value.(map[string]any); ok {
			if result := jsonStringValue(child, append([]string{"id"}, keys...)...); result != "" {
				return result
			}
		}
	}
	return jsonStringValue(object, keys...)
}

func jsonNestedName(object map[string]any, nested string, keys ...string) string {
	if value, ok := object[nested]; ok {
		if child, ok := value.(map[string]any); ok {
			if result := jsonStringValue(child, append([]string{"name", "displayName", "display_name"}, keys...)...); result != "" {
				return result
			}
		}
	}
	return jsonStringValue(object, keys...)
}

func firstJSONValue(object map[string]any, keys ...string) (any, bool) {
	for _, key := range keys {
		if value, ok := object[key]; ok {
			return value, true
		}
	}
	return nil, false
}

func jsonStringValue(object map[string]any, keys ...string) string {
	value, ok := firstJSONValue(object, keys...)
	if !ok {
		return ""
	}
	return jsonString(value)
}

func jsonString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return ""
	}
}

func jsonIntPointer(object map[string]any, keys ...string) *int {
	value := jsonStringValue(object, keys...)
	if value == "" {
		return nil
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return nil
	}
	return &parsed
}

func jsonCentsPointer(object map[string]any, keys ...string) *int64 {
	value := jsonStringValue(object, keys...)
	if value == "" {
		return nil
	}
	return parseOptionalCents(value)
}

func resolveURLFromString(pageURL, raw string, inScope func(*url.URL) bool) string {
	page, err := url.Parse(pageURL)
	if err != nil {
		return ""
	}
	return resolveURL(page, raw, inScope)
}

func normalizeEntryType(value string) string {
	value = strings.ToLower(strings.TrimSpace(strings.ReplaceAll(value, "-", "_")))
	value = strings.Join(strings.Fields(value), "_")
	switch {
	case strings.Contains(value, "monster") && strings.Contains(value, "dyp"):
		return "monster_dyp"
	case strings.Contains(value, "whist"):
		return "whist"
	case value == "dyp":
		return "dyp"
	case strings.Contains(value, "team") || strings.Contains(value, "double") || strings.Contains(value, "doppel"):
		return "team_name"
	case value == "single" || value == "singles":
		return "single"
	default:
		return ""
	}
}

func normalizeModes(value string) []string {
	value = strings.ToLower(strings.TrimSpace(strings.ReplaceAll(value, "-", "_")))
	result := make([]string, 0, 2)
	if strings.Contains(value, "monster") && strings.Contains(value, "dyp") {
		result = append(result, "monster_dyp")
	}
	if strings.Contains(value, "whist") {
		result = append(result, "whist")
	}
	return result
}

func mergeStandingsMeta(left, right standingsMeta) standingsMeta {
	if left.DisciplineName == "" {
		left.DisciplineName = right.DisciplineName
	}
	if left.GroupName == "" {
		left.GroupName = right.GroupName
	}
	if right.EntryTypeEvidence {
		left.EntryType = right.EntryType
		left.EntryTypeEvidence = true
	} else if left.EntryType == "" {
		left.EntryType = right.EntryType
	}
	if right.ModeEvidence {
		left.Modes = dedupeStrings(append(left.Modes, right.Modes...))
		left.ModeEvidence = true
	} else if len(left.Modes) == 0 {
		left.Modes = right.Modes
	}
	return left
}
