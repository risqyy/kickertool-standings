package tournamentapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"kickertool-ranking/internal/domain"
	"kickertool-ranking/internal/ports"
)

const DefaultBaseURL = "https://api.tournament.io/v1/public"

var ErrMissingToken = errors.New("tournament API token is required")

type AuthError struct{ StatusCode int }

func (e *AuthError) Error() string {
	return fmt.Sprintf("tournament API authentication failed with HTTP %d", e.StatusCode)
}

type Source struct {
	baseURL   string
	client    ports.HTTPClient
	token     string
	pageLimit int
	logger    *zerolog.Logger
}

func (s *Source) SourceName() string { return domain.KickertoolAPISource }

// SmokeHello performs the required read-only authentication check before a crawl.
func (s *Source) SmokeHello(ctx context.Context) error {
	var response json.RawMessage
	return s.getJSON(ctx, "/hello", &response)
}

func NewSource(baseURL string, client ports.HTTPClient, token string, pageLimit int, logger *zerolog.Logger) (*Source, error) {
	if strings.TrimSpace(token) == "" {
		return nil, ErrMissingToken
	}
	if client == nil {
		return nil, errors.New("tournament API HTTP client is required")
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = DefaultBaseURL
	}
	if pageLimit <= 0 {
		pageLimit = 25
	}
	return &Source{baseURL: strings.TrimRight(baseURL, "/"), client: client, token: token, pageLimit: pageLimit, logger: logger}, nil
}

func (s *Source) FetchTournaments(ctx context.Context) ([]domain.Tournament, error) {
	var result []domain.Tournament
	seen := make(map[string]struct{})
	pageCount := 0
	for offset := 0; ; offset += s.pageLimit {
		pageCount++
		if pageCount > 10000 {
			return nil, errors.New("tournament pagination exceeded safety limit")
		}
		path := fmt.Sprintf("/tournaments?limit=%d&offset=%d&state=finished", s.pageLimit, offset)
		var raw json.RawMessage
		if err := s.getJSON(ctx, path, &raw); err != nil {
			return nil, fmt.Errorf("fetch tournaments offset %d: %w", offset, err)
		}
		page, err := decodeTournamentPage(raw)
		if err != nil {
			return nil, fmt.Errorf("decode tournaments offset %d: %w", offset, err)
		}
		if len(page) == 0 {
			break
		}
		for _, item := range page {
			if item.ID == "" {
				continue
			}
			if _, duplicate := seen[item.ID]; duplicate {
				return nil, fmt.Errorf("tournament pagination repeated id %s", item.ID)
			}
			seen[item.ID] = struct{}{}
			// The list response may omit stage/group modes. Always use the detail
			// hierarchy so a mixed format cannot pass the exclusive MonsterDYP filter.
			var detail apiTournament
			if err := s.getJSON(ctx, "/tournaments/"+url.PathEscape(item.ID), &detail); err != nil {
				return nil, fmt.Errorf("fetch tournament %s for MonsterDYP filter: %w", item.ID, err)
			}
			if !hasExclusiveMonsterDYP(detail.Disciplines) {
				continue
			}
			result = append(result, s.mapTournament(item))
		}
		if len(page) < s.pageLimit {
			break
		}
	}
	return result, nil
}

// hasExclusiveMonsterDYP accepts a tournament only when every discipline is
// MonsterDYP and no group declares another tournament mode (for example Swiss
// or elimination). Empty group mode is tolerated for API responses that omit
// the optional field.
func hasExclusiveMonsterDYP(disciplines []apiDiscipline) bool {
	if len(disciplines) == 0 {
		return false
	}
	for _, discipline := range disciplines {
		if !strings.EqualFold(strings.TrimSpace(discipline.EntryType), "monster_dyp") {
			return false
		}
		for _, stage := range discipline.Stages {
			for _, group := range stage.Groups {
				mode := strings.ToLower(strings.TrimSpace(group.TournamentMode))
				if mode != "" && mode != "monster_dyp" {
					return false
				}
			}
		}
	}
	return true
}

func (s *Source) FetchStandings(ctx context.Context, tournament domain.Tournament) (domain.StandingSnapshot, error) {
	if tournament.SourceID == "" {
		return domain.StandingSnapshot{}, fmt.Errorf("tournament API standings requires source id")
	}
	var detail apiTournament
	if err := s.getJSON(ctx, "/tournaments/"+url.PathEscape(tournament.SourceID), &detail); err != nil {
		return domain.StandingSnapshot{}, fmt.Errorf("fetch tournament hierarchy %s: %w", tournament.SourceID, err)
	}
	if !strings.EqualFold(strings.TrimSpace(tournament.Status), "finished") || !hasExclusiveMonsterDYP(detail.Disciplines) {
		return domain.StandingSnapshot{Source: domain.KickertoolAPISource, TournamentID: tournament.SourceID, URL: s.baseURL + "/tournaments/" + url.PathEscape(tournament.SourceID), Complete: false, FetchedAt: time.Now()}, nil
	}
	snapshot := domain.StandingSnapshot{Source: domain.KickertoolAPISource, TournamentID: tournament.SourceID, URL: s.baseURL + "/tournaments/" + url.PathEscape(tournament.SourceID), Complete: true, FetchedAt: time.Now()}
	for _, discipline := range detail.Disciplines {
		snapshot.Disciplines = append(snapshot.Disciplines, domain.Discipline{SourceID: discipline.ID, TournamentID: tournament.SourceID, Name: discipline.Name, ShortName: discipline.ShortName, EntryType: normalizeEntryType(discipline.EntryType)})
		for _, stage := range discipline.Stages {
			snapshot.Stages = append(snapshot.Stages, domain.Stage{SourceID: stage.ID, DisciplineID: discipline.ID, TournamentID: tournament.SourceID, State: stage.State})
			for _, group := range stage.Groups {
				snapshot.Groups = append(snapshot.Groups, domain.StandingGroup{SourceID: group.ID, StageID: stage.ID, DisciplineID: discipline.ID, TournamentID: tournament.SourceID, Name: group.Name, State: group.State})
				entries, err := s.fetchEntries(ctx, tournament.SourceID, group.ID)
				if err != nil {
					return domain.StandingSnapshot{}, fmt.Errorf("fetch group entries %s: %w", group.ID, err)
				}
				for _, entry := range entries {
					snapshot.Entries = append(snapshot.Entries, domain.Entry{SourceID: entry.ID, TournamentID: tournament.SourceID, Name: entry.Name, EntryType: normalizeEntryType(discipline.EntryType)})
					members := entry.Members
					if len(members) == 0 {
						members = entry.AlternateMembers
					}
					if len(members) == 0 {
						members = entry.Players
					}
					for _, member := range members {
						if member.ID == "" {
							continue
						}
						snapshot.Memberships = append(snapshot.Memberships, domain.EntryMembership{EntryID: entry.ID, PlayerID: member.ID, PlayerName: member.Name, TournamentID: tournament.SourceID})
					}
				}
				standingRows, err := s.fetchGroupStandings(ctx, tournament.SourceID, group.ID)
				if err != nil {
					return domain.StandingSnapshot{}, fmt.Errorf("fetch group standings %s: %w", group.ID, err)
				}
				allocatedBefore := len(snapshot.Standings)
				for _, row := range standingRows {
					standing := mapStanding(row, tournament.SourceID, discipline.ID, stage.ID, group.ID, s.baseURL+"/tournaments/"+url.PathEscape(tournament.SourceID)+"/groups/"+url.PathEscape(group.ID)+"/standings")
					if standing.TournamentID == "" {
						snapshot.Complete = false
						continue
					}
					snapshot.GroupStandings = append(snapshot.GroupStandings, toGroupStanding(standing))
					members := make([]domain.EntryMembership, 0)
					for _, membership := range snapshot.Memberships {
						if membership.EntryID == standing.EntryID && membership.TournamentID == tournament.SourceID {
							members = append(members, membership)
						}
					}
					if len(members) == 0 {
						if standing.PlayerID != "" || standing.PlayerName != "" {
							snapshot.Standings = append(snapshot.Standings, standing)
						}
						continue
					}
					for _, membership := range members {
						allocated := standing
						allocated.StandingID = standing.StandingID + "/" + membership.PlayerID
						allocated.StandingKey = standing.StandingKey + "/" + membership.PlayerID
						allocated.PlayerID = membership.PlayerID
						allocated.PlayerKey = domain.PlayerKey(membership.PlayerName)
						allocated.PlayerName = membership.PlayerName
						snapshot.Standings = append(snapshot.Standings, allocated)
						snapshot.Allocations = append(snapshot.Allocations, domain.StandingAllocation{StandingID: allocated.StandingID, EntryID: standing.EntryID, PlayerID: membership.PlayerID, PointsCents: valueOrZero(allocated.PointsCents)})
					}
				}
				if s.logger != nil {
					s.logger.Info().Str("source", domain.KickertoolAPISource).Str("tournament_id", tournament.SourceID).Str("group_id", group.ID).
						Int("standing_rows", len(standingRows)).Int("allocated_rows", len(snapshot.Standings)-allocatedBefore).
						Msg("group standings mapped")
				}
			}
		}
	}
	if len(snapshot.Groups) == 0 || len(snapshot.GroupStandings) == 0 {
		snapshot.Complete = false
	}
	for _, standing := range snapshot.Standings {
		if standing.PointsCents != nil {
			snapshot.PointsExplicit = true
		}
	}
	return snapshot, nil
}

func toGroupStanding(standing domain.TournamentStanding) domain.GroupStanding {
	return domain.GroupStanding{SourceID: standing.StandingID, TournamentID: standing.TournamentID, DisciplineID: standing.DisciplineID, StageID: standing.StageID, GroupID: standing.Group, EntryID: standing.EntryID, EntryName: standing.EntryName, PlayerID: standing.PlayerID, PlayerName: standing.PlayerName, Rank: standing.Rank, Result: standing.Result, Preliminary: standing.Preliminary, FinalResult: standing.FinalResult, PointsCents: standing.PointsCents, PointsPerMatchCents: standing.PointsPerMatchCents, CorrectedPointsPerMatchCents: standing.CorrectedPointsPerMatchCents, HasCorrectedValue: standing.HasCorrectedValue, GamesPlayed: standing.GamesPlayed, GoalDifference: standing.GoalDifference, URL: standing.URL}
}

func valueOrZero(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func (s *Source) fetchEntries(ctx context.Context, tournamentID, groupID string) ([]apiEntry, error) {
	var raw json.RawMessage
	if err := s.getJSON(ctx, "/tournaments/"+url.PathEscape(tournamentID)+"/group/"+url.PathEscape(groupID)+"/entries", &raw); err != nil {
		return nil, err
	}
	var entries []apiEntry
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&entries); err == nil {
		return entries, nil
	}
	var wrapped struct {
		Entries []apiEntry `json:"entries"`
	}
	decoder = json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&wrapped); err != nil {
		return nil, fmt.Errorf("decode entries: %w", err)
	}
	return wrapped.Entries, nil
}

func (s *Source) fetchGroupStandings(ctx context.Context, tournamentID, groupID string) ([]map[string]any, error) {
	var raw json.RawMessage
	if err := s.getJSON(ctx, "/tournaments/"+url.PathEscape(tournamentID)+"/groups/"+url.PathEscape(groupID)+"/standings", &raw); err != nil {
		return nil, err
	}
	var rows []map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&rows); err == nil {
		return rows, nil
	}
	var wrapped struct {
		Standings []map[string]any `json:"standings"`
	}
	decoder = json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&wrapped); err != nil {
		return nil, fmt.Errorf("decode standings: %w", err)
	}
	return wrapped.Standings, nil
}

func (s *Source) getJSON(ctx context.Context, path string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", s.token)
	req.Header.Set("Accept", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	if resp == nil || resp.Body == nil {
		return fmt.Errorf("empty API response")
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 16<<20+1))
	closeErr := resp.Body.Close()
	if readErr != nil {
		return fmt.Errorf("read API response: %w", readErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close API response: %w", closeErr)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return &AuthError{StatusCode: resp.StatusCode}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("API HTTP status %d", resp.StatusCode)
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode API response: %w", err)
	}
	return nil
}

type apiTournament struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Date        string          `json:"date"`
	StartTime   string          `json:"startTime"`
	EndTime     string          `json:"endTime"`
	State       string          `json:"state"`
	NumPlayers  *int            `json:"numPlayers"`
	NumTeams    *int            `json:"numTeams"`
	Disciplines []apiDiscipline `json:"disciplines"`
}
type apiDiscipline struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	ShortName string     `json:"shortName"`
	EntryType string     `json:"entryType"`
	Stages    []apiStage `json:"stages"`
}
type apiStage struct {
	ID     string     `json:"id"`
	State  string     `json:"state"`
	Groups []apiGroup `json:"groups"`
}
type apiGroup struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	TournamentMode string `json:"tournamentMode"`
	State          string `json:"state"`
}
type apiEntry struct {
	ID               string      `json:"id"`
	Name             string      `json:"name"`
	Members          []apiMember `json:"entries"`
	AlternateMembers []apiMember `json:"members"`
	Players          []apiMember `json:"players"`
}
type apiMember struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (s *Source) mapTournament(item apiTournament) domain.Tournament {
	date := parseTime(item.Date)
	if date == nil {
		date = parseTime(item.StartTime)
	}
	return domain.Tournament{Source: domain.KickertoolAPISource, SourceID: item.ID, SourceKey: item.ID, Name: strings.TrimSpace(item.Name), Date: date, StartTime: parseTime(item.StartTime), EndTime: parseTime(item.EndTime), Status: strings.ToLower(strings.TrimSpace(item.State)), IsLive: strings.EqualFold(item.State, "running"), Participants: item.NumPlayers, URL: s.baseURL + "/tournaments/" + url.PathEscape(item.ID)}
}

func decodeTournamentPage(raw json.RawMessage) ([]apiTournament, error) {
	var page []apiTournament
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&page); err == nil {
		return page, nil
	}
	var wrapped struct {
		Tournaments []apiTournament `json:"tournaments"`
	}
	decoder = json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&wrapped); err != nil {
		return nil, fmt.Errorf("decode tournament list: %w", err)
	}
	return wrapped.Tournaments, nil
}

func parseTime(value string) *time.Time {
	if value == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err == nil {
		return &parsed
	}
	location, locationErr := time.LoadLocation("Europe/Berlin")
	if locationErr != nil {
		location = time.UTC
	}
	parsed, err = time.ParseInLocation("2006-01-02", value, location)
	if err != nil {
		return nil
	}
	return &parsed
}
func normalizeEntryType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "single", "team_name", "dyp", "byp", "monster_dyp":
		return value
	default:
		return value
	}
}

func mapStanding(row map[string]any, tournamentID, disciplineID, stageID, groupID, standingURL string) domain.TournamentStanding {
	entryID := firstString(row, "entryId", "entryID")
	entryName := firstString(row, "entryName", "name")
	if nested, ok := extractEntryValue(row["entry"]); ok {
		entryID = firstNonEmpty(entryID, nested.ID)
		entryName = firstNonEmpty(entryName, nested.Name)
	}
	playerID := firstString(row, "playerId", "playerID", "participantId")
	playerName := firstString(row, "playerName", "participantName")
	if nested, ok := row["player"].(map[string]any); ok {
		playerID = firstNonEmpty(playerID, firstString(nested, "id"))
		playerName = firstNonEmpty(playerName, firstString(nested, "name"))
	}
	standingID := firstString(row, "id", "standingId", "resultId")
	points, pointsOK := firstCents(row, "points", "totalPoints", "score")
	pointsPerMatch, pointsPerMatchOK := firstCents(row, "pointsPerMatch", "points_per_match")
	correctedPointsPerMatch, correctedPointsPerMatchOK := firstCents(row, "correctedPointsPerMatch", "corrected_points_per_match")
	standing := domain.TournamentStanding{Source: domain.KickertoolAPISource, TournamentID: tournamentID, EntryID: entryID, EntryName: entryName, StandingID: standingID, StandingKey: standingID, DisciplineID: disciplineID, StageID: stageID, Group: groupID, PlayerID: playerID, PlayerKey: domain.PlayerKey(playerName), PlayerName: strings.TrimSpace(playerName), URL: standingURL, PointsCents: nil}
	if pointsOK {
		standing.PointsCents = &points
	}
	if pointsPerMatchOK {
		standing.PointsPerMatchCents = &pointsPerMatch
	}
	if correctedPointsPerMatchOK {
		standing.CorrectedPointsPerMatchCents = &correctedPointsPerMatch
	}
	if _, ok := row["hasCorrectedValue"]; ok {
		value, present := firstBool(row, "hasCorrectedValue", "has_corrected_value")
		if present {
			standing.HasCorrectedValue = &value
		}
	} else if correctedPointsPerMatchOK {
		value := true
		standing.HasCorrectedValue = &value
	}
	if value, ok := firstInt(row, "rank", "position", "place"); ok {
		standing.Rank = &value
	}
	if value, ok := firstInt(row, "result", "resultRank"); ok {
		standing.Result = &value
	}
	if value, ok := firstInt(row, "preliminary", "prelim", "preliminaryResult"); ok {
		standing.Preliminary = &value
	}
	if value, ok := firstInt(row, "final", "finalResult"); ok {
		standing.FinalResult = &value
	}
	if value, ok := firstInt(row, "games", "matches", "numMatches"); ok {
		standing.GamesPlayed = &value
	}
	if value, ok := firstInt(row, "goalDifference", "goalDiff", "gplusminus"); ok {
		standing.GoalDifference = &value
	}
	if standing.StandingID == "" {
		standing.StandingID = tournamentID + "/" + groupID + "/" + entryID
	}
	standing.StandingKey = standing.StandingID
	if standing.StandingKey == "" {
		standing.StandingKey = tournamentID + "/" + groupID + "/" + entryID
	}
	if standing.PlayerKey == "" {
		standing.PlayerKey = domain.PlayerKey(standing.PlayerName)
	}
	return standing
}

type entryValue struct {
	ID   string
	Name string
}

// extractEntryValue accepts the API's object and list representations without
// ever confusing a standing/result ID with an entry ID.
func extractEntryValue(value any) (entryValue, bool) {
	switch typed := value.(type) {
	case map[string]any:
		entry := entryValue{ID: firstString(typed, "id", "entryId", "entryID"), Name: firstString(typed, "name", "entryName")}
		if entry.ID != "" || entry.Name != "" {
			return entry, true
		}
		for _, key := range []string{"entry", "team", "groupEntry"} {
			if nested, ok := extractEntryValue(typed[key]); ok {
				return nested, true
			}
		}
	case []any:
		for _, item := range typed {
			if nested, ok := extractEntryValue(item); ok {
				return nested, true
			}
		}
	}
	return entryValue{}, false
}

func firstString(row map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := row[key]; ok {
			if text, ok := value.(string); ok {
				return strings.TrimSpace(text)
			}
		}
	}
	return ""
}
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
func firstBool(row map[string]any, keys ...string) (bool, bool) {
	for _, key := range keys {
		if value, ok := row[key]; ok {
			switch typed := value.(type) {
			case bool:
				return typed, true
			case string:
				parsed, err := strconv.ParseBool(strings.TrimSpace(typed))
				return parsed, err == nil
			}
		}
	}
	return false, false
}
func firstInt(row map[string]any, keys ...string) (int, bool) {
	for _, key := range keys {
		if value, ok := row[key]; ok {
			switch typed := value.(type) {
			case float64:
				return int(typed), true
			case json.Number:
				n, err := typed.Int64()
				return int(n), err == nil
			case string:
				n, err := strconv.Atoi(typed)
				return n, err == nil
			}
		}
	}
	return 0, false
}
func firstCents(row map[string]any, keys ...string) (int64, bool) {
	for _, key := range keys {
		if value, ok := row[key]; ok {
			switch typed := value.(type) {
			case float64:
				return parseCents(strconv.FormatFloat(typed, 'f', -1, 64))
			case json.Number:
				return parseCents(string(typed))
			case string:
				return parseCents(typed)
			}
		}
	}
	return 0, false
}

func parseCents(value string) (int64, bool) {
	value = strings.TrimSpace(strings.ReplaceAll(value, ",", "."))
	if value == "" {
		return 0, false
	}
	negative := strings.HasPrefix(value, "-")
	if negative || strings.HasPrefix(value, "+") {
		value = value[1:]
	}
	parts := strings.SplitN(value, ".", 2)
	wholeText := parts[0]
	if wholeText == "" {
		wholeText = "0"
	}
	whole, err := strconv.ParseInt(wholeText, 10, 64)
	if err != nil || whole < 0 {
		return 0, false
	}
	fraction := ""
	if len(parts) == 2 {
		fraction = parts[1]
	}
	for _, character := range fraction {
		if character < '0' || character > '9' {
			return 0, false
		}
	}
	cents := int64(0)
	if len(fraction) > 0 {
		cents += int64(fraction[0]-'0') * 10
	}
	if len(fraction) > 1 {
		cents += int64(fraction[1] - '0')
	}
	if len(fraction) > 2 && fraction[2] >= '5' {
		cents++
	}
	if cents == 100 {
		whole++
		cents = 0
	}
	result := whole*100 + cents
	if negative {
		result = -result
	}
	return result, true
}
