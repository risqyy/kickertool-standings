package kickertoolhtml

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"golang.org/x/net/html"
	"kickertool-ranking/internal/domain"
	"kickertool-ranking/internal/ports"
)

const SourceName = domain.KickertoolHTMLSource

type Source struct {
	startURL string
	start    *url.URL
	client   ports.HTTPClient
	logger   *zerolog.Logger
}

func NewSource(startURL string, client ports.HTTPClient, logger *zerolog.Logger) (*Source, error) {
	parsed, err := url.Parse(strings.TrimSpace(startURL))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("TOURNAMENT_HTML_URL must be a valid http or https URL")
	}
	if client == nil {
		return nil, fmt.Errorf("HTML HTTP client is required")
	}
	return &Source{startURL: parsed.String(), start: parsed, client: client, logger: logger}, nil
}

func (s *Source) SourceName() string { return SourceName }

func (s *Source) FetchTournaments(ctx context.Context) ([]domain.Tournament, error) {
	pending := []string{s.startURL}
	visited := make(map[string]struct{})
	seenIDs := make(map[string]struct{})
	var tournaments []domain.Tournament
	for page := 0; page < 100 && len(pending) > 0; page++ {
		current := pending[0]
		pending = pending[1:]
		if _, exists := visited[current]; exists {
			continue
		}
		visited[current] = struct{}{}
		body, finalURL, err := s.get(ctx, current)
		if err != nil {
			return nil, fmt.Errorf("fetch HTML listing %s: %w", current, err)
		}
		parsed, nextURLs, err := parseListing(finalURL, body, s.inScope)
		if err != nil {
			return nil, fmt.Errorf("parse HTML listing %s: %w", current, err)
		}
		for _, tournament := range parsed {
			// The generic crawler invokes FetchStandings for every returned
			// tournament. HTML has no separate capability probe, so keep pure
			// Whist, unknown categories, and non-finished cards out of that sync
			// queue. A later crawl will pick up a newly completed Monster DYP.
			if !eligibleHTMLListingTournament(tournament) {
				continue
			}
			identity := tournament.SourceID
			if identity == "" {
				identity = tournament.SourceKey
			}
			if _, exists := seenIDs[identity]; exists {
				continue
			}
			seenIDs[identity] = struct{}{}
			tournaments = append(tournaments, tournament)
		}
		for _, next := range nextURLs {
			if _, visitedAlready := visited[next]; visitedAlready {
				continue
			}
			queued := false
			for _, candidate := range pending {
				if candidate == next {
					queued = true
					break
				}
			}
			if !queued {
				pending = append(pending, next)
			}
		}
	}
	if len(visited) >= 100 {
		return nil, fmt.Errorf("HTML pagination exceeded safety limit")
	}
	if s.logger != nil {
		s.logger.Info().Str("source", SourceName).Int("pages", len(visited)).Int("found", len(tournaments)).Msg("HTML listing fetched")
	}
	return tournaments, nil
}

func eligibleHTMLListingTournament(tournament domain.Tournament) bool {
	return strings.EqualFold(strings.TrimSpace(tournament.Status), "finished") && normalizeEntryType(tournament.EntryType) == "monster_dyp"
}

func (s *Source) FetchStandings(ctx context.Context, tournament domain.Tournament) (domain.StandingSnapshot, error) {
	if tournament.SourceID == "" || tournament.URL == "" {
		return domain.StandingSnapshot{}, fmt.Errorf("HTML standings require tournament ID and URL")
	}
	started := time.Now()
	discovered := []string{tournament.URL}
	visited := make(map[string]struct{})
	rowsByKey := make(map[string]domain.TournamentStanding)
	meta := standingsMeta{EntryType: normalizeEntryType(tournament.EntryType), EntryTypeEvidence: normalizeEntryType(tournament.EntryType) != ""}
	var finalURL string
	tablesFound, pagesFetched := 0, 0
	requiredFailure := ""
	for len(discovered) > 0 && pagesFetched < maxStandingsDiscoveryPages {
		pageURL := discovered[0]
		discovered = discovered[1:]
		if _, exists := visited[pageURL]; exists {
			continue
		}
		visited[pageURL] = struct{}{}
		body, resolvedURL, fetchErr := s.get(ctx, pageURL)
		if fetchErr != nil {
			if pageURL != tournament.URL {
				requiredFailure = pageURL
				break
			}
			return domain.StandingSnapshot{}, fmt.Errorf("fetch HTML standings %s: %w", tournament.SourceID, fetchErr)
		}
		pagesFetched++
		if finalURL == "" {
			finalURL = resolvedURL
		}
		document, parseErr := parseStandingDocument(resolvedURL, body, s.inScope)
		if parseErr != nil {
			return domain.StandingSnapshot{}, fmt.Errorf("parse HTML standings %s: %w", tournament.SourceID, parseErr)
		}
		tablesFound += document.TablesFound
		meta = mergeStandingsMeta(meta, document.Meta)
		for _, row := range document.Rows {
			key := row.StandingID
			if key == "" {
				key = row.StandingKey
			}
			if key == "" {
				key = fallbackID(tournament.SourceID, row.PlayerName, row.Rank)
			}
			rowsByKey[key] = row
		}
		for _, candidate := range document.CandidateURLs {
			if _, exists := visited[candidate]; !exists {
				discovered = append(discovered, candidate)
			}
		}
	}
	if len(discovered)+pagesFetched >= maxStandingsDiscoveryPages {
		requiredFailure = "discovery safety limit"
	}
	if requiredFailure != "" {
		s.logStandingsDiagnostics(tournament, visited, tablesFound, len(rowsByKey), 0, "required discovery page failed: "+requiredFailure, meta, started)
		return domain.StandingSnapshot{}, fmt.Errorf("HTML standings discovery incomplete for %s: %s", tournament.SourceID, requiredFailure)
	}
	rows := make([]domain.TournamentStanding, 0, len(rowsByKey))
	for _, row := range rowsByKey {
		rows = append(rows, row)
	}
	if len(rows) == 0 || !strings.EqualFold(strings.TrimSpace(tournament.Status), "finished") || !eligibleHTMLTournament(tournament, meta) {
		s.logStandingsDiagnostics(tournament, visited, tablesFound, len(rows), 0, "no complete finished Monster DYP standings discovered", meta, started)
		return domain.StandingSnapshot{Source: SourceName, TournamentID: tournament.SourceID, URL: finalURL, Complete: false, FetchedAt: time.Now()}, nil
	}
	disciplineID := "html:discipline:" + tournament.SourceID
	stageID := "html:stage:" + tournament.SourceID
	groupID := "html:group:" + tournament.SourceID
	entryType := meta.EntryType
	snapshot := domain.StandingSnapshot{
		Source: SourceName, TournamentID: tournament.SourceID, URL: finalURL, Complete: true, FetchedAt: time.Now(),
		Disciplines: []domain.Discipline{{SourceID: disciplineID, TournamentID: tournament.SourceID, Name: meta.DisciplineName, ShortName: meta.DisciplineName, EntryType: entryType}},
		Stages:      []domain.Stage{{SourceID: stageID, DisciplineID: disciplineID, TournamentID: tournament.SourceID, State: tournament.Status}},
		Groups:      []domain.StandingGroup{{SourceID: groupID, StageID: stageID, DisciplineID: disciplineID, TournamentID: tournament.SourceID, Name: meta.GroupName, State: tournament.Status}},
	}
	for _, row := range rows {
		row.Source = SourceName
		row.TournamentID = tournament.SourceID
		row.DisciplineID = disciplineID
		row.StageID = stageID
		row.Group = groupID
		row.URL = finalURL
		row.PlayerKey = domain.PlayerKey(row.PlayerName)
		if row.StandingID == "" {
			row.StandingID = fallbackID(tournament.SourceID, row.PlayerName, row.Rank)
		}
		row.StandingKey = row.StandingID
		if row.PlayerName == "" || (row.PlayerID == "" && row.Team != "") {
			continue
		}
		snapshot.Standings = append(snapshot.Standings, row)
		snapshot.GroupStandings = append(snapshot.GroupStandings, domain.GroupStanding{
			SourceID: row.StandingID, TournamentID: row.TournamentID, DisciplineID: row.DisciplineID, StageID: row.StageID, GroupID: row.Group,
			EntryID: row.EntryID, EntryName: row.EntryName, PlayerID: row.PlayerID, PlayerName: row.PlayerName, Rank: row.Rank,
			Result: row.Result, Preliminary: row.Preliminary, FinalResult: row.FinalResult, PointsCents: row.PointsCents,
			PointsPerMatchCents: row.PointsPerMatchCents, CorrectedPointsPerMatchCents: row.CorrectedPointsPerMatchCents,
			HasCorrectedValue: row.HasCorrectedValue, GamesPlayed: row.GamesPlayed, GoalDifference: row.GoalDifference, URL: row.URL,
		})
		if row.PointsCents != nil {
			snapshot.PointsExplicit = true
		}
	}
	snapshot.Complete = len(snapshot.Standings) > 0 && len(snapshot.GroupStandings) > 0
	s.logStandingsDiagnostics(tournament, visited, tablesFound, len(rows), len(snapshot.Standings), "detail/discipline/group discovery completed", meta, started)
	return snapshot, nil
}

const maxStandingsDiscoveryPages = 48

func (s *Source) logStandingsDiagnostics(tournament domain.Tournament, visited map[string]struct{}, tables, rows, allocated int, reason string, meta standingsMeta, started time.Time) {
	if s.logger == nil {
		return
	}
	urls := make([]string, 0, len(visited))
	for value := range visited {
		urls = append(urls, value)
	}
	s.logger.Info().Str("source", SourceName).Str("tournament_id", tournament.SourceID).
		Strs("fetched_candidate_urls", urls).Int("pages_fetched", len(visited)).Int("tables_found", tables).
		Int("rows_found", rows).Int("accepted_allocated_rows", allocated).Str("discovery_reason", reason).
		Str("entry_type", meta.EntryType).Bool("entry_type_evidence", meta.EntryTypeEvidence).
		Dur("duration", time.Since(started)).Msg("HTML standings discovery diagnostics")
}

func (s *Source) get(ctx context.Context, rawURL string) ([]byte, string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || !s.inScope(parsed) {
		return nil, "", fmt.Errorf("discovered URL outside configured HTML scope")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/json;q=0.9")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, "", err
	}
	if resp == nil || resp.Body == nil {
		return nil, "", fmt.Errorf("empty HTML response")
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 32<<20+1))
	closeErr := resp.Body.Close()
	if readErr != nil {
		return nil, "", readErr
	}
	if closeErr != nil {
		return nil, "", closeErr
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("HTML HTTP status %d", resp.StatusCode)
	}
	return body, parsed.String(), nil
}

func (s *Source) inScope(candidate *url.URL) bool {
	if candidate == nil || candidate.Scheme != s.start.Scheme || !strings.EqualFold(candidate.Host, s.start.Host) {
		return false
	}
	basePath := strings.TrimRight(s.start.Path, "/")
	if basePath == "" {
		return true
	}
	return candidate.Path == basePath || strings.HasPrefix(candidate.Path, basePath+"/")
}

type listingResult struct {
	Tournaments []domain.Tournament
	NextURLs    []string
}

func parseListing(pageURL string, body []byte, inScope func(*url.URL) bool) ([]domain.Tournament, []string, error) {
	root, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil, nil, err
	}
	page, err := url.Parse(pageURL)
	if err != nil {
		return nil, nil, err
	}
	var tournaments []domain.Tournament
	var nextURLs []string
	walk(root, func(node *html.Node) {
		if node.Type != html.ElementNode {
			return
		}
		if node.Data == "a" {
			if href := attr(node, "href"); href != "" {
				if resolved := resolveURL(page, href, inScope); resolved != "" {
					if isTournamentURL(resolved) {
						tournaments = append(tournaments, tournamentFromAnchor(node, resolved))
					}
					if isNextLink(node, resolved) {
						nextURLs = append(nextURLs, resolved)
					}
				}
			}
		}
		for _, key := range []string{"data-next-url", "data-next-page", "data-page-url", "data-load-more-url", "data-url", "data-href"} {
			if resolved := resolveURL(page, attr(node, key), inScope); resolved != "" && !isTournamentURL(resolved) {
				nextURLs = append(nextURLs, resolved)
			}
		}
	})
	for _, discovered := range discoverURLs(page, body, inScope) {
		if !isTournamentURL(discovered) {
			nextURLs = append(nextURLs, discovered)
		}
	}
	return dedupeTournaments(tournaments), dedupeStrings(nextURLs), nil
}

// discoverURLs covers pagination hints embedded in JSON or small scripts
// without executing JavaScript. Scope validation prevents foreign hosts.
func discoverURLs(page *url.URL, body []byte, inScope func(*url.URL) bool) []string {
	pattern := regexp.MustCompile(`(?i)(?:https?://[^"'\s<>]+|/[^"'\s<>]+)`)
	result := make([]string, 0)
	for _, match := range pattern.FindAllString(string(body), -1) {
		resolved := resolveURL(page, match, inScope)
		if resolved == "" {
			continue
		}
		parsed, err := url.Parse(resolved)
		if err == nil && hasPaginationQuery(parsed) {
			result = append(result, resolved)
		}
	}
	return result
}

func hasPaginationQuery(parsed *url.URL) bool {
	if parsed == nil {
		return false
	}
	query := parsed.Query()
	for _, key := range []string{"page", "offset", "cursor", "p"} {
		if query.Get(key) != "" {
			return true
		}
	}
	return false
}

func isTournamentURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	for i := range segments {
		if segments[i] == "tournaments" && i+1 < len(segments) {
			return true
		}
	}
	return false
}

func isNextLink(node *html.Node, resolved string) bool {
	rel := strings.ToLower(attr(node, "rel"))
	class := strings.ToLower(attr(node, "class"))
	text := strings.ToLower(strings.TrimSpace(nodeText(node)))
	if strings.Contains(rel, "next") || strings.Contains(class, "next") || strings.Contains(class, "pagination") && (strings.Contains(text, "next") || strings.Contains(text, "weiter") || strings.Contains(text, "näch")) {
		return true
	}
	parsed, err := url.Parse(resolved)
	return err == nil && hasPaginationQuery(parsed)
}

func tournamentFromAnchor(anchor *html.Node, resolved string) domain.Tournament {
	parsed, _ := url.Parse(resolved)
	id := tournamentID(parsed)
	text := nodeText(anchor)
	date, startTime := parseHTMLDateComponent(anchor)
	if date == nil {
		if component := tournamentComponentScope(anchor); component != nil {
			date, startTime = parseHTMLDateComponent(component)
		}
	}
	componentText := text
	if component := tournamentComponentScope(anchor); component != nil {
		componentText += " " + nodeText(component)
	}
	status := statusFromText(componentText+" "+attr(anchor, "class"), date)
	participants := parseParticipants(componentText)
	key := ""
	if id == "" {
		hash := sha256.Sum256([]byte(strings.TrimRight(resolved, "/")))
		key = "sha256:" + hex.EncodeToString(hash[:])
	}
	typeText := tournamentTypeText(anchor)
	if normalizeEntryType(typeText) == "" {
		if component := tournamentComponentScope(anchor); component != nil {
			typeText = tournamentTypeText(component)
		}
	}
	entryType := normalizeEntryType(typeText)
	return domain.Tournament{Source: SourceName, SourceID: id, SourceKey: key, Name: anchorTitle(anchor, id), Date: date, StartTime: startTime, Status: status, EntryType: entryType, IsLive: strings.Contains(status, "running"), Participants: participants, URL: resolved}
}

func tournamentComponentScope(anchor *html.Node) *html.Node {
	for parent := anchor.Parent; parent != nil; parent = parent.Parent {
		if parent.Type != html.ElementNode {
			continue
		}
		classAndID := strings.ToLower(attr(parent, "class") + " " + attr(parent, "id"))
		if parent.Data == "li" || strings.Contains(classAndID, "card") || strings.Contains(classAndID, "tournament") || strings.Contains(classAndID, "event") || strings.Contains(classAndID, "list-item") {
			return parent
		}
	}
	return nil
}

func tournamentID(parsed *url.URL) string {
	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	for i := range segments {
		if segments[i] == "tournaments" && i+1 < len(segments) {
			value, err := url.PathUnescape(segments[i+1])
			if err == nil {
				return value
			}
			return segments[i+1]
		}
	}
	return ""
}

func anchorTitle(anchor *html.Node, fallback string) string {
	var title string
	walk(anchor, func(node *html.Node) {
		if title != "" || node.Type != html.ElementNode {
			return
		}
		class := strings.ToLower(attr(node, "class"))
		if node.Data == "h1" || node.Data == "h2" || node.Data == "h3" || strings.Contains(class, "title") || strings.Contains(class, "tournament-name") || classHasToken(class, "name") {
			title = strings.TrimSpace(nodeText(node))
		}
	})
	if title == "" {
		title = strings.TrimSpace(nodeText(anchor))
	}
	if title == "" {
		return fallback
	}
	return title
}

func classHasToken(class, wanted string) bool {
	for _, token := range strings.Fields(strings.ToLower(class)) {
		if token == wanted {
			return true
		}
	}
	return false
}

// tournamentTypeText returns text from explicit category/type components first.
// The listing contains unrelated metadata (date, participant count, status) in
// the same card, so using the complete anchor text would make a date or status
// look like a tournament type.
func tournamentTypeText(anchor *html.Node) string {
	var values []string
	walk(anchor, func(node *html.Node) {
		if node.Type != html.ElementNode {
			return
		}
		classAndID := strings.ToLower(attr(node, "class") + " " + attr(node, "id"))
		for _, key := range []string{"data-name-type", "data-nametype", "data-category", "data-entry-type", "data-entrytype", "name-type", "nametype", "name_type", "category", "entry-type", "entry_type"} {
			if value := strings.TrimSpace(attr(node, key)); value != "" {
				values = append(values, value)
				return
			}
		}
		if strings.Contains(classAndID, "name-type") || strings.Contains(classAndID, "nametype") || strings.Contains(classAndID, "category") || strings.Contains(classAndID, "entry-type") || strings.Contains(classAndID, "discipline-type") {
			if value := strings.TrimSpace(nodeText(node)); value != "" {
				values = append(values, value)
			}
		}
	})
	return strings.Join(values, " ")
}

func parseParticipants(text string) *int {
	match := regexp.MustCompile(`(?i)(\d+)\s*(player|players|teilnehmer|participants)`).FindStringSubmatch(text)
	if len(match) != 2 {
		return nil
	}
	value, err := strconv.Atoi(match[1])
	if err != nil {
		return nil
	}
	return &value
}

// parseHTMLDateComponent reads a semantic <time> (or an equivalent data
// attribute) before falling back to visible text. A timestamp is interpreted
// as an instant, converted to Europe/Berlin, and its local calendar day is
// stored as Date. This is important around midnight: e.g.
// 2025-05-03T22:17:32Z is 04.05.2025 in Berlin.
func parseHTMLDateComponent(root *html.Node) (*time.Time, *time.Time) {
	if root == nil {
		return nil, nil
	}
	var date *time.Time
	var start *time.Time
	walk(root, func(node *html.Node) {
		if node.Type != html.ElementNode || date != nil && start != nil {
			return
		}
		for _, key := range []string{"datetime", "data-datetime", "data-date", "data-tournament-date", "data-event-date", "data-start", "data-start-date", "data-tournament-start", "data-timestamp", "start-date", "startDate"} {
			if raw := strings.TrimSpace(attr(node, key)); raw != "" {
				if value, hasClock := parseHTMLDateValue(raw); value != nil {
					if date == nil {
						date = value
					}
					if hasClock && start == nil {
						if instant, ok := parseHTMLTimeInstant(raw); ok {
							start = instant
						} else {
							start = value
						}
					}
					break
				}
			}
		}
	})
	if date != nil {
		return date, start
	}
	walk(root, func(node *html.Node) {
		if node.Type != html.ElementNode || date != nil {
			return
		}
		classAndID := strings.ToLower(attr(node, "class") + " " + attr(node, "id"))
		if !strings.Contains(classAndID, "date") && !strings.Contains(classAndID, "time") && !strings.Contains(classAndID, "start") {
			return
		}
		if value, hasClock := parseHTMLDateValue(nodeText(node)); value != nil {
			date = value
			if hasClock {
				if instant, ok := parseHTMLTimeInstant(nodeText(node)); ok {
					start = instant
				} else {
					start = value
				}
			}
		}
	})
	return date, start
}

func parseHTMLDate(text string) *time.Time {
	value, _ := parseHTMLDateValue(text)
	if value != nil {
		return value
	}
	match := regexp.MustCompile(`\b\d{1,4}[./-]\d{1,2}[./-]\d{1,4}\b`).FindString(text)
	if match == "" {
		return nil
	}
	value, _ = parseHTMLDateValue(match)
	return value
}

func parseHTMLDateValue(raw string) (*time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, false
	}
	location, locationErr := time.LoadLocation("Europe/Berlin")
	if locationErr != nil {
		location = time.UTC
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999Z07:00"} {
		if value, err := time.Parse(layout, raw); err == nil {
			local := value.In(location)
			day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
			return &day, true
		}
	}
	for _, layout := range []string{"2006-01-02T15:04:05.999999999", "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02", "02.01.2006", "2.1.2006", "02/01/2006", "2/1/2006"} {
		if value, err := time.ParseInLocation(layout, raw, location); err == nil {
			if !strings.Contains(layout, "15:") {
				value = time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, location)
				return &value, false
			}
			return localDateValue(value), true
		}
	}
	return nil, false
}

func localDateValue(value time.Time) *time.Time {
	local := value.In(value.Location())
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, local.Location())
	return &day
}

func parseHTMLTimeInstant(raw string) (*time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, false
	}
	location, locationErr := time.LoadLocation("Europe/Berlin")
	if locationErr != nil {
		location = time.UTC
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999Z07:00"} {
		if value, err := time.Parse(layout, raw); err == nil {
			value = value.In(location)
			return &value, true
		}
	}
	for _, layout := range []string{"2006-01-02T15:04:05.999999999", "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02 15:04"} {
		if value, err := time.ParseInLocation(layout, raw, location); err == nil {
			return &value, true
		}
	}
	return nil, false
}

func statusFromText(text string, date *time.Time) string {
	lower := strings.ToLower(text)
	if strings.Contains(lower, "cancel") || strings.Contains(lower, "abgesagt") {
		return "cancelled"
	}
	if strings.Contains(lower, "running") || strings.Contains(lower, "live") {
		return "running"
	}
	if strings.Contains(lower, "finished") || strings.Contains(lower, "completed") || strings.Contains(lower, "beendet") || strings.Contains(lower, "abgeschlossen") {
		return "finished"
	}
	if date != nil && date.Before(time.Now()) {
		return "finished"
	}
	return "planned"
}

func fallbackID(tournamentID, name string, rank *int) string {
	rankText := ""
	if rank != nil {
		rankText = strconv.Itoa(*rank)
	}
	hash := sha256.Sum256([]byte(tournamentID + "|" + domain.PlayerKey(name) + "|" + rankText))
	return "sha256:" + hex.EncodeToString(hash[:])
}

type standingsMeta struct {
	DisciplineName    string
	GroupName         string
	EntryType         string
	EntryTypeEvidence bool
	Modes             []string
	ModeEvidence      bool
}

// eligibleHTMLTournament deliberately uses the tournament category (the
// nameType/entryType evidence), not a mode list. A Monster-DYP category may
// expose a Whist mode alongside it and remains eligible; a Whist category is
// never eligible by itself.
func eligibleHTMLTournament(tournament domain.Tournament, meta standingsMeta) bool {
	category := normalizeEntryType(meta.EntryType)
	if category == "" {
		category = normalizeEntryType(tournament.EntryType)
	}
	return meta.EntryTypeEvidence && category == "monster_dyp"
}

func parseStandings(pageURL string, body []byte) ([]domain.TournamentStanding, standingsMeta, error) {
	root, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil, standingsMeta{}, err
	}
	entryType, evidence, modes := competitionEvidence(root, body)
	meta := standingsMeta{DisciplineName: "HTML standings", GroupName: "HTML standings", EntryType: entryType, EntryTypeEvidence: evidence, Modes: modes, ModeEvidence: len(modes) > 0}
	var result []domain.TournamentStanding
	walk(root, func(node *html.Node) {
		if node.Type != html.ElementNode || node.Data != "table" || strings.Contains(strings.ToLower(attr(node, "class")), "nested") {
			return
		}
		rows := childRows(node)
		if len(rows) < 2 {
			return
		}
		header := cellTexts(rows[0])
		indices := columnIndices(header)
		for _, rowNode := range rows[1:] {
			cells := cellTexts(rowNode)
			if len(cells) < 2 {
				continue
			}
			standing := standingFromCells(rowNode, cells, indices)
			if standing.PlayerName != "" {
				result = append(result, standing)
			}
		}
	})
	for _, rows := range semanticTableRows(root) {
		if len(rows) < 2 {
			continue
		}
		header := semanticCellTexts(rows[0])
		indices := columnIndices(header)
		for _, rowNode := range rows[1:] {
			cells := semanticCellTexts(rowNode)
			if len(cells) < 2 {
				continue
			}
			standing := standingFromCells(rowNode, cells, indices)
			if standing.PlayerName != "" {
				result = append(result, standing)
			}
		}
	}
	return result, meta, nil
}

type columnIndex struct{ rank, player, points, games, goals, pointsPerMatch int }

func columnIndices(headers []string) columnIndex {
	result := columnIndex{rank: 0, player: 1, points: -1, games: -1, goals: -1, pointsPerMatch: -1}
	for i, header := range headers {
		value := strings.ToLower(header)
		switch {
		case strings.Contains(value, "rank") || value == "#" || strings.Contains(value, "platz"):
			result.rank = i
		case strings.Contains(value, "player") || strings.Contains(value, "spieler") || strings.Contains(value, "participant") || strings.Contains(value, "name"):
			result.player = i
		case strings.Contains(value, "point") || strings.Contains(value, "pkt") || strings.Contains(value, "punkte"):
			result.points = i
		case strings.Contains(value, "match") || strings.Contains(value, "game") || strings.Contains(value, "spiel") || strings.Contains(value, "num"):
			result.games = i
		case strings.Contains(value, "goal") || strings.Contains(value, "tor") || strings.Contains(value, "diff") || strings.Contains(value, "g±") || strings.Contains(value, "g+"):
			result.goals = i
		case strings.Contains(value, "øp") || strings.Contains(value, "avg") || strings.Contains(value, "average"):
			result.pointsPerMatch = i
		}
	}
	return result
}

func standingFromCells(row *html.Node, cells []string, indices columnIndex) domain.TournamentStanding {
	standing := domain.TournamentStanding{StandingID: attrAny(row, "data-standing-id", "data-result-id", "data-id"), EntryID: attrAny(row, "data-entry-id", "data-team-id"), PlayerID: attrAny(row, "data-player-id", "data-participant-id"), Team: attrAny(row, "data-team-name")}
	standing.PlayerName = cellAt(cells, indices.player)
	standing.EntryName = standing.Team
	if standing.EntryName == "" {
		standing.EntryName = standing.PlayerName
	}
	standing.Rank = parseOptionalInt(cellAt(cells, indices.rank))
	standing.PointsCents = parseOptionalCents(cellAt(cells, indices.points))
	standing.PointsPerMatchCents = parseOptionalCents(cellAt(cells, indices.pointsPerMatch))
	standing.GamesPlayed = parseOptionalInt(cellAt(cells, indices.games))
	standing.GoalDifference = parseOptionalInt(cellAt(cells, indices.goals))
	if standing.EntryID == "" && standing.PlayerID != "" {
		standing.EntryID = standing.PlayerID
	}
	return standing
}

func semanticTableRows(root *html.Node) [][]*html.Node {
	var result [][]*html.Node
	walk(root, func(node *html.Node) {
		if node.Type != html.ElementNode || node.Data != "div" {
			return
		}
		class := strings.ToLower(attr(node, "class"))
		if !strings.Contains(class, "table") || strings.Contains(class, "table-row") {
			return
		}
		var rows []*html.Node
		walk(node, func(child *html.Node) {
			if child != node && child.Type == html.ElementNode && child.Data == "div" && strings.Contains(strings.ToLower(attr(child, "class")), "table-row") {
				rows = append(rows, child)
			}
		})
		if len(rows) > 1 {
			result = append(result, rows)
		}
	})
	return result
}

func semanticCellTexts(row *html.Node) []string {
	var result []string
	for child := row.FirstChild; child != nil; child = child.NextSibling {
		if child.Type != html.ElementNode {
			continue
		}
		class := strings.ToLower(attr(child, "class"))
		if child.Data == "div" || child.Data == "span" || strings.Contains(class, "cell") || strings.Contains(class, "name") || strings.Contains(class, "int") || strings.Contains(class, "float") || strings.Contains(class, "pos") {
			result = append(result, strings.Join(strings.Fields(nodeText(child)), " "))
		}
	}
	return result
}

func parseOptionalInt(value string) *int {
	value = strings.TrimSpace(value)
	if value == "" || value == "—" || value == "-" {
		return nil
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(strings.Trim(value, "+")))
	if err != nil {
		return nil
	}
	return &parsed
}

func parseOptionalCents(value string) *int64 {
	value = strings.TrimSpace(strings.ReplaceAll(value, ",", "."))
	if value == "" || value == "—" || value == "-" {
		return nil
	}
	negative := strings.HasPrefix(value, "-")
	if negative {
		value = value[1:]
	}
	parts := strings.SplitN(value, ".", 2)
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return nil
	}
	fraction := ""
	if len(parts) == 2 {
		fraction = parts[1]
	}
	for len(fraction) < 2 {
		fraction += "0"
	}
	if len(fraction) > 2 {
		fraction = fraction[:2]
	}
	cents, err := strconv.ParseInt(fraction, 10, 64)
	if err != nil {
		return nil
	}
	result := whole*100 + cents
	if negative {
		result = -result
	}
	return &result
}

func detectEntryType(text string) string {
	text = strings.ToLower(text)
	switch {
	case strings.Contains(text, "monster dyp") || strings.Contains(text, "monster_dyp"):
		return "monster_dyp"
	case strings.Contains(text, "team") || strings.Contains(text, "doppel"):
		return "team_name"
	default:
		return "single"
	}
}

func childRows(table *html.Node) []*html.Node {
	var result []*html.Node
	walk(table, func(node *html.Node) {
		if node != table && node.Type == html.ElementNode && node.Data == "tr" {
			result = append(result, node)
		}
	})
	return result
}

func cellTexts(row *html.Node) []string {
	var result []string
	for child := row.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.ElementNode && (child.Data == "td" || child.Data == "th" || strings.Contains(strings.ToLower(attr(child, "class")), "cell")) {
			result = append(result, strings.Join(strings.Fields(nodeText(child)), " "))
		}
	}
	return result
}

func cellAt(cells []string, index int) string {
	if index < 0 || index >= len(cells) {
		return ""
	}
	return cells[index]
}

func attrAny(node *html.Node, keys ...string) string {
	for _, key := range keys {
		if value := attr(node, key); value != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func resolveURL(base *url.URL, raw string, inScope func(*url.URL) bool) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	resolved, err := base.Parse(strings.TrimSpace(raw))
	if err != nil || !inScope(resolved) {
		return ""
	}
	resolved.Fragment = ""
	return resolved.String()
}

func walk(node *html.Node, visit func(*html.Node)) {
	visit(node)
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		walk(child, visit)
	}
}

func attr(node *html.Node, key string) string {
	for _, attribute := range node.Attr {
		if attribute.Key == key {
			return attribute.Val
		}
	}
	return ""
}

func nodeText(node *html.Node) string {
	var builder strings.Builder
	walk(node, func(child *html.Node) {
		if child.Type == html.TextNode {
			builder.WriteString(child.Data)
			builder.WriteByte(' ')
		}
	})
	return strings.Join(strings.Fields(builder.String()), " ")
}

func dedupeStrings(values []string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok || value == "" {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func dedupeTournaments(values []domain.Tournament) []domain.Tournament {
	seen := make(map[string]struct{})
	result := make([]domain.Tournament, 0, len(values))
	for _, value := range values {
		key := value.SourceID
		if key == "" {
			key = value.SourceKey
		}
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
}
