package httpapi

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"kickertool-ranking/internal/domain"
	"kickertool-ranking/internal/ports"
)

func profileVersion(profile domain.PlayerProfile) string {
	serialized, _ := json.Marshal(profile)
	digest := sha256.Sum256(serialized)
	return hex.EncodeToString(digest[:])
}

const adminCSRFTokenCookie = "kickertool_admin_csrf"

// AdminBasicAuth protects every administrative JSON endpoint.
// Credentials are compared as SHA-256 digests in constant time and are never
// placed in request context, response bodies, logs, or frontend data.
func AdminBasicAuth(next http.Handler, username, password string, logger *zerolog.Logger) http.Handler {
	expectedUser := sha256.Sum256([]byte(username))
	expectedPassword := sha256.Sum256([]byte(password))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		providedUser := sha256.Sum256([]byte(user))
		providedPassword := sha256.Sum256([]byte(pass))
		valid := ok && subtle.ConstantTimeCompare(expectedUser[:], providedUser[:]) == 1 && subtle.ConstantTimeCompare(expectedPassword[:], providedPassword[:]) == 1
		if !valid {
			w.Header().Set("WWW-Authenticate", `Basic realm="Kickertool Admin", charset="UTF-8"`)
			if logger != nil {
				logger.Warn().Msg("admin authentication failed")
			}
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type AdminAPIHandler struct {
	tournaments ports.TournamentAdminRepository
	directory   ports.PlayerDirectory
	merger      ports.PlayerMergeService
	logger      *zerolog.Logger
	mu          sync.Mutex
	plans       map[string]adminMergePlan
}

type adminMergePlan struct {
	SourceID, TargetID uint
	SourceVersion      string
	TargetVersion      string
	TargetName         string
	ExpiresAt          time.Time
	Result             domain.MergeResult
	Completed          *domain.MergeResult
}

func NewAdminAPIHandler(tournaments ports.TournamentAdminRepository, directory ports.PlayerDirectory, merger ports.PlayerMergeService, logger *zerolog.Logger) *AdminAPIHandler {
	return &AdminAPIHandler{tournaments: tournaments, directory: directory, merger: merger, logger: logger, plans: make(map[string]adminMergePlan)}
}

func (h *AdminAPIHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	setAdminHeaders(w)
	if r.URL.Path == "/api/admin/session" && r.Method == http.MethodGet {
		token := ensureAdminCSRF(w, r)
		writeJSON(w, http.StatusOK, map[string]any{"authenticated": true, "csrf_token": token})
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/admin/") && isMutation(r.Method) {
		if !validateAdminMutation(w, r) {
			return
		}
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/admin/dashboard":
		h.dashboard(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/api/admin/tournaments":
		h.tournamentList(w, r)
	case isMutation(r.Method) && strings.HasPrefix(r.URL.Path, "/api/admin/tournaments/") && strings.HasSuffix(r.URL.Path, "/inclusion"):
		h.setInclusion(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/api/admin/players/search":
		h.playerSearch(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/admin/players/"):
		h.playerDetail(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/admin/players/merge/preview":
		h.mergePreview(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/admin/players/merge/confirm":
		h.mergeConfirm(w, r)
	default:
		h.writeError(w, http.StatusNotFound, "not found")
	}
}

func setAdminHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
}

func ensureAdminCSRF(w http.ResponseWriter, r *http.Request) string {
	if cookie, err := r.Cookie(adminCSRFTokenCookie); err == nil && cookie.Value != "" {
		return cookie.Value
	}
	token := randomAdminToken()
	http.SetCookie(w, &http.Cookie{Name: adminCSRFTokenCookie, Value: token, Path: "/", HttpOnly: false, SameSite: http.SameSiteStrictMode, Secure: requestIsHTTPS(r)})
	return token
}

func validateAdminMutation(w http.ResponseWriter, r *http.Request) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		writeJSONErrorResponse(w, http.StatusBadRequest, "invalid content type", nil)
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		parsed, err := url.Parse(origin)
		expectedScheme := requestScheme(r)
		if err != nil || parsed.Host != r.Host || (expectedScheme != "" && !strings.EqualFold(parsed.Scheme, expectedScheme)) {
			writeJSONErrorResponse(w, http.StatusForbidden, "invalid origin", nil)
			return false
		}
	}
	cookie, err := r.Cookie(adminCSRFTokenCookie)
	if err != nil || cookie.Value == "" || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(r.Header.Get("X-CSRF-Token"))) != 1 {
		writeJSONErrorResponse(w, http.StatusForbidden, "invalid csrf token", nil)
		return false
	}
	return true
}

// requestScheme accounts for TLS termination at the configured reverse proxy.
// The proxy must set X-Forwarded-Proto; an absent header keeps direct HTTP
// development requests compatible while the Host check remains mandatory.
func requestScheme(r *http.Request) string {
	if forwarded := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]); forwarded == "http" || forwarded == "https" {
		return forwarded
	}
	if r.TLS != nil {
		return "https"
	}
	return ""
}

func requestIsHTTPS(r *http.Request) bool {
	return requestScheme(r) == "https"
}

func isMutation(method string) bool {
	return method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch || method == http.MethodDelete
}

func (h *AdminAPIHandler) dashboard(w http.ResponseWriter, r *http.Request) {
	value, err := h.tournaments.GetDashboard(r.Context())
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "dashboard unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": dashboardDTO(value)})
}

func (h *AdminAPIHandler) tournamentList(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	filter := domain.TournamentListFilter{Query: query.Get("q"), State: query.Get("state"), Source: query.Get("source"), Page: parsePositive(query.Get("page"), 1), Limit: parsePositive(query.Get("limit"), 25), Sort: query.Get("sort"), Desc: strings.EqualFold(query.Get("direction"), "desc")}
	if value := query.Get("included"); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			h.writeError(w, http.StatusBadRequest, "included must be true or false")
			return
		}
		filter.Included = &parsed
	}
	page, err := h.tournaments.ListTournaments(r.Context(), filter)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "tournaments unavailable")
		return
	}
	items := make([]tournamentDTO, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, tournamentDTOFrom(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "page": page.Page, "limit": page.Limit, "total": page.Total, "last_sync_at": page.LastSyncAt})
}

func (h *AdminAPIHandler) setInclusion(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 5 || parts[0] != "api" || parts[1] != "admin" || parts[2] != "tournaments" || parts[4] != "inclusion" {
		h.writeError(w, http.StatusNotFound, "not found")
		return
	}
	id, err := strconv.ParseUint(parts[3], 10, 64)
	if err != nil || id == 0 {
		h.writeError(w, http.StatusBadRequest, "invalid tournament id")
		return
	}
	var input struct {
		Included        *bool  `json:"included"`
		ExpectedVersion int64  `json:"expectedVersion"`
		Reason          string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil || input.Included == nil {
		h.writeError(w, http.StatusBadRequest, "included and expectedVersion are required")
		return
	}
	result, err := h.tournaments.SetTournamentRankingInclusion(r.Context(), uint(id), *input.Included, input.ExpectedVersion, input.Reason)
	if err != nil {
		if errors.Is(err, ports.ErrNotFound) {
			h.writeError(w, http.StatusNotFound, "tournament not found")
			return
		}
		if errors.Is(err, ports.ErrVersionConflict) {
			h.writeJSONError(w, http.StatusConflict, "version conflict", map[string]any{"code": "version_conflict"})
			return
		}
		h.writeError(w, http.StatusInternalServerError, "inclusion update failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"changed": result.Changed, "audit_id": result.AuditID, "tournament": tournamentDTOFrom(result.Tournament)})
}

func (h *AdminAPIHandler) playerSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if len([]rune(strings.TrimSpace(query))) < 2 {
		writeJSON(w, http.StatusOK, map[string]any{"items": []any{}, "message": "Bitte mindestens 2 Zeichen eingeben."})
		return
	}
	profiles, err := h.directory.SearchPlayers(r.Context(), query)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "player search unavailable")
		return
	}
	items := make([]playerDTO, 0, len(profiles))
	for _, profile := range profiles {
		items = append(items, playerDTOFrom(profile))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *AdminAPIHandler) playerDetail(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 4 || parts[0] != "api" || parts[1] != "admin" || parts[2] != "players" {
		h.writeError(w, http.StatusNotFound, "not found")
		return
	}
	id, err := strconv.ParseUint(parts[3], 10, 64)
	if err != nil || id == 0 {
		h.writeError(w, http.StatusBadRequest, "invalid player id")
		return
	}
	profile, err := h.directory.GetPlayerProfile(r.Context(), uint(id))
	if err != nil {
		h.writeError(w, http.StatusNotFound, "player not found")
		return
	}
	writeJSON(w, http.StatusOK, playerDTOFrom(profile))
}

func (h *AdminAPIHandler) mergePreview(w http.ResponseWriter, r *http.Request) {
	var input struct {
		SourcePlayerID uint `json:"sourcePlayerId"`
		TargetPlayerID uint `json:"targetPlayerId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil || input.SourcePlayerID == 0 || input.TargetPlayerID == 0 || input.SourcePlayerID == input.TargetPlayerID {
		h.writeError(w, http.StatusBadRequest, "source and target must be different players")
		return
	}
	source, err := h.directory.GetPlayerProfile(r.Context(), input.SourcePlayerID)
	if err != nil {
		h.writeError(w, http.StatusNotFound, "source player not found")
		return
	}
	target, err := h.directory.GetPlayerProfile(r.Context(), input.TargetPlayerID)
	if err != nil {
		h.writeError(w, http.StatusNotFound, "target player not found")
		return
	}
	result, err := h.merger.MergePlayers(r.Context(), source.ID, target.ID, domain.PlayerMergeOptions{DryRun: true})
	if err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	token := randomAdminToken()
	h.mu.Lock()
	h.plans[token] = adminMergePlan{SourceID: source.ID, TargetID: target.ID, SourceVersion: profileVersion(source), TargetVersion: profileVersion(target), TargetName: target.DisplayName, ExpiresAt: time.Now().Add(5 * time.Minute), Result: result}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "result": mergeResultDTO(result)})
}

func (h *AdminAPIHandler) mergeConfirm(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Token             string `json:"token"`
		Confirmed         bool   `json:"confirmed"`
		TargetDisplayName string `json:"targetDisplayName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil || input.Token == "" || !input.Confirmed {
		h.writeError(w, http.StatusBadRequest, "confirmation is required")
		return
	}
	h.mu.Lock()
	plan, ok := h.plans[input.Token]
	h.mu.Unlock()
	if !ok || time.Now().After(plan.ExpiresAt) {
		h.writeError(w, http.StatusConflict, "preview expired; create a new preview")
		return
	}
	if plan.Completed != nil {
		writeJSON(w, http.StatusOK, map[string]any{"alreadyMerged": true, "result": mergeResultDTO(*plan.Completed)})
		return
	}
	if input.TargetDisplayName != plan.TargetName {
		h.writeError(w, http.StatusBadRequest, "exact target display name is required")
		return
	}
	source, err := h.directory.GetPlayerProfile(r.Context(), plan.SourceID)
	if err != nil {
		h.writeError(w, http.StatusConflict, "player state changed; create a new preview")
		return
	}
	target, err := h.directory.GetPlayerProfile(r.Context(), plan.TargetID)
	if err != nil || target.DisplayName != plan.TargetName || profileVersion(source) != plan.SourceVersion || profileVersion(target) != plan.TargetVersion {
		h.writeError(w, http.StatusConflict, "player state changed; create a new preview")
		return
	}
	result, err := h.merger.MergePlayers(r.Context(), plan.SourceID, plan.TargetID, domain.PlayerMergeOptions{})
	if err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.mu.Lock()
	plan.Completed = &result
	h.plans[input.Token] = plan
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"alreadyMerged": result.AlreadyMerged, "result": mergeResultDTO(result)})
}

type tournamentDTO struct {
	ID                 uint       `json:"id"`
	Source             string     `json:"source"`
	SourceID           string     `json:"sourceId"`
	SourceKey          string     `json:"sourceKey"`
	Name               string     `json:"name"`
	Date               *time.Time `json:"date"`
	StartTime          *time.Time `json:"startTime"`
	EndTime            *time.Time `json:"endTime"`
	Status             string     `json:"status"`
	IsLive             bool       `json:"isLive"`
	EntryType          string     `json:"entryType"`
	IncludedInRanking  bool       `json:"includedInRanking"`
	InclusionUpdatedAt *time.Time `json:"inclusionUpdatedAt"`
	InclusionVersion   int64      `json:"inclusionVersion"`
	InclusionReason    string     `json:"inclusionReason"`
	URL                string     `json:"url"`
	Participants       *int       `json:"participants"`
	StandingCount      int        `json:"standingCount"`
	PlayerCount        int        `json:"playerCount"`
	StandingsComplete  bool       `json:"standingsComplete"`
	LastSyncError      bool       `json:"lastSyncError"`
	StandingsSyncedAt  *time.Time `json:"standingsSyncedAt"`
	LastSeenAt         time.Time  `json:"lastSeenAt"`
}

func tournamentDTOFrom(row domain.TournamentAdminRow) tournamentDTO {
	t := row.Tournament
	return tournamentDTO{ID: t.ID, Source: t.Source, SourceID: t.SourceID, SourceKey: t.SourceKey, Name: t.Name, Date: t.Date, StartTime: t.StartTime, EndTime: t.EndTime, Status: t.Status, IsLive: t.IsLive, EntryType: t.EntryType, IncludedInRanking: t.IncludedInRanking, InclusionUpdatedAt: t.InclusionUpdatedAt, InclusionVersion: row.InclusionVersion, InclusionReason: t.InclusionReason, URL: t.URL, Participants: t.Participants, StandingCount: row.StandingCount, PlayerCount: row.PlayerCount, StandingsComplete: row.StandingsComplete, LastSyncError: row.LastSyncError, StandingsSyncedAt: t.StandingsSyncedAt, LastSeenAt: t.LastSeenAt}
}

type dashboardDTOValue struct {
	TournamentCount         int64      `json:"tournamentCount"`
	IncludedTournamentCount int64      `json:"includedTournamentCount"`
	ExcludedTournamentCount int64      `json:"excludedTournamentCount"`
	PlayerCount             int64      `json:"playerCount"`
	LastSyncAt              *time.Time `json:"lastSyncAt"`
}

func dashboardDTO(value domain.Dashboard) dashboardDTOValue {
	return dashboardDTOValue{TournamentCount: value.TournamentCount, IncludedTournamentCount: value.IncludedTournamentCount, ExcludedTournamentCount: value.ExcludedTournamentCount, PlayerCount: value.PlayerCount, LastSyncAt: value.LastSyncAt}
}

type playerDTO struct {
	ID                 uint     `json:"id"`
	DisplayName        string   `json:"displayName"`
	CanonicalNameKey   string   `json:"canonicalNameKey"`
	Aliases            []string `json:"aliases"`
	MatchedAlias       string   `json:"matchedAlias,omitempty"`
	Active             bool     `json:"active"`
	MergedIntoPlayerID *uint    `json:"mergedIntoPlayerId,omitempty"`
	TournamentCount    int      `json:"tournamentCount"`
	GamesPlayed        *int     `json:"gamesPlayed"`
	TotalPointsCents   *int64   `json:"totalPointsCents"`
	PointsPerGameCents *int64   `json:"pointsPerGameCents"`
	GoalDifference     *int     `json:"goalDifference"`
}

func playerDTOFrom(profile domain.PlayerProfile) playerDTO {
	aliases := make([]string, 0, len(profile.Aliases))
	for _, alias := range profile.Aliases {
		aliases = append(aliases, alias.DisplayName)
	}
	return playerDTO{ID: profile.ID, DisplayName: profile.DisplayName, CanonicalNameKey: profile.CanonicalNameKey, Aliases: aliases, MatchedAlias: profile.MatchedAlias, Active: profile.Active, MergedIntoPlayerID: profile.MergedIntoPlayerID, TournamentCount: profile.Aggregate.TournamentCount, GamesPlayed: profile.Aggregate.GamesPlayed, TotalPointsCents: profile.Aggregate.TotalPointsCents, PointsPerGameCents: profile.Aggregate.PointsPerGameCents, GoalDifference: profile.Aggregate.GoalDifference}
}

func mergeResultDTO(result domain.MergeResult) map[string]any {
	return map[string]any{"sourcePlayerId": result.SourcePlayerID, "targetPlayerId": result.TargetPlayerID, "sourceDisplayName": result.SourceDisplayName, "targetDisplayName": result.TargetDisplayName, "alreadyMerged": result.AlreadyMerged, "transferredAliases": result.TransferredAliases, "transferredSourceIdentities": result.TransferredSourceIdentities, "transferredAllocations": result.TransferredAllocations, "deduplicatedAllocations": result.DeduplicatedAllocations, "sourceBefore": playerAggregateDTO(result.SourceBefore), "targetBefore": playerAggregateDTO(result.TargetBefore), "targetAfter": playerAggregateDTO(result.TargetAfter)}
}

func playerAggregateDTO(value *domain.PlayerAggregate) any {
	if value == nil {
		return nil
	}
	return map[string]any{"tournamentCount": value.TournamentCount, "gamesPlayed": value.GamesPlayed, "totalPointsCents": value.TotalPointsCents, "pointsPerGameCents": value.PointsPerGameCents, "goalDifference": value.GoalDifference}
}

func (h *AdminAPIHandler) writeError(w http.ResponseWriter, status int, message string) {
	h.writeJSONError(w, status, message, nil)
}

func (h *AdminAPIHandler) writeJSONError(w http.ResponseWriter, status int, message string, extra map[string]any) {
	writeJSONErrorResponse(w, status, message, extra)
}

func writeJSONErrorResponse(w http.ResponseWriter, status int, message string, extra map[string]any) {
	body := map[string]any{"error": message}
	for key, value := range extra {
		body[key] = value
	}
	writeJSON(w, status, body)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func parsePositive(value string, fallback int) int {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		return fallback
	}
	return parsed
}

func randomAdminToken() string {
	var value [24]byte
	if _, err := rand.Read(value[:]); err != nil {
		sum := sha256.Sum256([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
		return hex.EncodeToString(sum[:])
	}
	return hex.EncodeToString(value[:])
}
