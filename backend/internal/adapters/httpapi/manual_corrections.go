package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"kickertool-ranking/internal/domain"
	"kickertool-ranking/internal/ports"
)

func (h *AdminAPIHandler) isCorrectionListPath(path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	return len(parts) == 5 && parts[0] == "api" && parts[1] == "admin" && parts[2] == "players" && (parts[4] == "corrections" || parts[4] == "ranking-corrections") && parseID(parts[3]) > 0
}

func (h *AdminAPIHandler) isCorrectionPreviewPath(path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	return len(parts) == 6 && parts[0] == "api" && parts[1] == "admin" && parts[2] == "players" && (parts[4] == "corrections" || parts[4] == "ranking-corrections") && parts[5] == "preview" && parseID(parts[3]) > 0
}

func (h *AdminAPIHandler) isCorrectionConfirmPath(path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 5 && parts[0] == "api" && parts[1] == "admin" && parts[2] == "players" && (parts[3] == "corrections" || parts[3] == "ranking-corrections") && parts[4] == "confirm" {
		return true
	}
	return len(parts) == 6 && parts[0] == "api" && parts[1] == "admin" && parts[2] == "players" && (parts[4] == "corrections" || parts[4] == "ranking-corrections") && parts[5] == "confirm" && parseID(parts[3]) > 0
}

func (h *AdminAPIHandler) isCorrectionRevokePath(path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	return len(parts) == 7 && parts[0] == "api" && parts[1] == "admin" && parts[2] == "players" && parseID(parts[3]) > 0 && (parts[4] == "corrections" || parts[4] == "ranking-corrections") && parts[6] == "revoke" && parseID(parts[5]) > 0
}

func parseID(value string) uint {
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil || parsed == 0 || parsed > uint64(^uint(0)) {
		return 0
	}
	return uint(parsed)
}

func correctionPlayerID(path string) uint {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 4 && parts[0] == "api" && parts[1] == "admin" && parts[2] == "players" {
		return parseID(parts[3])
	}
	return 0
}

type correctionRequest struct {
	EffectiveDate        string          `json:"effectiveDate"`
	EffectiveYear        *int            `json:"effectiveYear"`
	TournamentCountDelta int             `json:"tournamentCountDelta"`
	GamesPlayedDelta     int             `json:"gamesPlayedDelta"`
	PointsCentsDelta     *int64          `json:"pointsCentsDelta"`
	PointsDelta          json.RawMessage `json:"pointsDelta"`
	GoalDifferenceDelta  int             `json:"goalDifferenceDelta"`
	Reason               string          `json:"reason"`
	ReplaceCorrectionID  uint            `json:"replaceCorrectionId"`
}

func (h *AdminAPIHandler) correctionList(w http.ResponseWriter, r *http.Request) {
	if h.corrections == nil {
		h.writeError(w, http.StatusNotImplemented, "ranking corrections unavailable")
		return
	}
	id := correctionPlayerID(r.URL.Path)
	items, err := h.corrections.ListManualRankingCorrections(r.Context(), id)
	if err != nil {
		if errors.Is(err, ports.ErrNotFound) {
			h.writeError(w, http.StatusNotFound, "player not found")
			return
		}
		h.writeError(w, http.StatusInternalServerError, "ranking corrections unavailable")
		return
	}
	version := int64(0)
	if profile, profileErr := h.directory.GetPlayerProfile(r.Context(), id); profileErr == nil {
		version = profile.RankingCorrectionVersion
	}
	values := make([]map[string]any, 0, len(items))
	for _, item := range items {
		values = append(values, correctionDTO(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": values, "version": version})
}

func (h *AdminAPIHandler) correctionPreview(w http.ResponseWriter, r *http.Request) {
	if h.corrections == nil {
		h.writeError(w, http.StatusNotImplemented, "ranking corrections unavailable")
		return
	}
	id := correctionPlayerID(r.URL.Path)
	var body correctionRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid correction payload")
		return
	}
	input, err := body.toInput(id, adminActor(r.Context()))
	if err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	preview, err := h.corrections.PreviewManualRankingCorrection(r.Context(), input)
	if err != nil {
		h.correctionError(w, err)
		return
	}
	fingerprint, err := h.playerStateFingerprint(r.Context(), id, preview.Player)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "could not fingerprint player state")
		return
	}
	token := randomAdminToken()
	h.mu.Lock()
	h.correctionPlans[token] = manualCorrectionPlan{PlayerID: id, Input: input, ExpectedVersion: preview.ExpectedVersion, StateFingerprint: fingerprint, ExpiresAt: time.Now().Add(5 * time.Minute), Preview: preview}
	h.mu.Unlock()
	value := manualCorrectionPreviewDTO(preview)
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "preview": value, "result": map[string]any{"before": aggregateDTO(preview.Before), "after": aggregateDTO(preview.After)}, "player": value["player"], "correction": value["correction"], "before": value["before"], "after": value["after"], "expectedVersion": value["expectedVersion"]})
}

func (h *AdminAPIHandler) correctionConfirm(w http.ResponseWriter, r *http.Request) {
	if h.corrections == nil {
		h.writeError(w, http.StatusNotImplemented, "ranking corrections unavailable")
		return
	}
	var body struct {
		Token           string `json:"token"`
		Confirmed       bool   `json:"confirmed"`
		ExpectedVersion *int64 `json:"expectedVersion"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Token) == "" || !body.Confirmed {
		h.writeError(w, http.StatusBadRequest, "confirmation is required")
		return
	}
	h.mu.Lock()
	plan, ok := h.correctionPlans[body.Token]
	h.mu.Unlock()
	if !ok || time.Now().After(plan.ExpiresAt) {
		h.writeError(w, http.StatusConflict, "preview expired; create a new preview")
		return
	}
	if plan.Completed != nil {
		writeJSON(w, http.StatusOK, manualCorrectionChangeDTO(*plan.Completed))
		return
	}
	if body.ExpectedVersion != nil && *body.ExpectedVersion != plan.ExpectedVersion {
		h.writeError(w, http.StatusConflict, "player state changed; create a new preview")
		return
	}
	profile, err := h.directory.GetPlayerProfile(r.Context(), plan.PlayerID)
	if err != nil {
		h.writeError(w, http.StatusConflict, "player state changed; create a new preview")
		return
	}
	fingerprint, err := h.playerStateFingerprint(r.Context(), plan.PlayerID, profile)
	if err != nil || fingerprint != plan.StateFingerprint {
		h.writeError(w, http.StatusConflict, "player state changed; create a new preview")
		return
	}
	var result domain.ManualRankingCorrectionChange
	if creator, ok := h.corrections.(interface {
		CreateManualRankingCorrectionWithFingerprint(context.Context, domain.ManualRankingCorrectionInput, int64, string) (domain.ManualRankingCorrectionChange, error)
	}); ok {
		result, err = creator.CreateManualRankingCorrectionWithFingerprint(r.Context(), plan.Input, plan.ExpectedVersion, plan.StateFingerprint)
	} else {
		result, err = h.corrections.CreateManualRankingCorrection(r.Context(), plan.Input, plan.ExpectedVersion)
	}
	if err != nil {
		h.correctionError(w, err)
		return
	}
	h.mu.Lock()
	plan.Completed = &result
	h.correctionPlans[body.Token] = plan
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, manualCorrectionChangeDTO(result))
}

func (h *AdminAPIHandler) playerStateFingerprint(ctx context.Context, playerID uint, profile domain.PlayerProfile) (string, error) {
	if reader, ok := h.directory.(interface {
		PlayerStateFingerprint(context.Context, uint) (string, error)
	}); ok {
		return reader.PlayerStateFingerprint(ctx, playerID)
	}
	return profileVersion(profile), nil
}

func (h *AdminAPIHandler) correctionRevoke(w http.ResponseWriter, r *http.Request) {
	if h.corrections == nil {
		h.writeError(w, http.StatusNotImplemented, "ranking corrections unavailable")
		return
	}
	var body struct {
		ExpectedVersion int64  `json:"expectedVersion"`
		Reason          string `json:"reason"`
		Confirmed       bool   `json:"confirmed"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || !body.Confirmed {
		h.writeError(w, http.StatusBadRequest, "confirmation is required")
		return
	}
	body.Reason = strings.TrimSpace(body.Reason)
	if len([]rune(body.Reason)) < 3 || len([]rune(body.Reason)) > 500 {
		h.writeError(w, http.StatusBadRequest, "revocation reason must contain 3 to 500 characters")
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	playerID, correctionID := parseID(parts[3]), parseID(parts[5])
	result, err := h.corrections.RevokeManualRankingCorrection(r.Context(), playerID, correctionID, body.ExpectedVersion, adminActor(r.Context()), body.Reason)
	if err != nil {
		h.correctionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"correction": correctionDTO(result.Correction), "before": aggregateDTO(result.Before), "after": aggregateDTO(result.After), "version": result.Version})
}

func (body correctionRequest) toInput(playerID uint, actor string) (domain.ManualRankingCorrectionInput, error) {
	// Do not trim or truncate: the wire contract is exactly YYYY-MM-DD.
	dateText := body.EffectiveDate
	if len(dateText) != len("2006-01-02") || dateText[4] != '-' || dateText[7] != '-' {
		return domain.ManualRankingCorrectionInput{}, errors.New("effectiveDate must be exactly YYYY-MM-DD")
	}
	location, locationErr := time.LoadLocation(domain.RankingLocation)
	if locationErr != nil {
		return domain.ManualRankingCorrectionInput{}, errors.New("ranking timezone unavailable")
	}
	date, err := time.ParseInLocation("2006-01-02", dateText, location)
	if err != nil {
		return domain.ManualRankingCorrectionInput{}, errors.New("effectiveDate must be YYYY-MM-DD")
	}
	if body.EffectiveYear != nil && *body.EffectiveYear != date.Year() {
		return domain.ManualRankingCorrectionInput{}, errors.New("effectiveYear must match effectiveDate")
	}
	points := int64(0)
	if body.PointsCentsDelta != nil {
		points = *body.PointsCentsDelta
	} else if len(body.PointsDelta) > 0 && string(body.PointsDelta) != "null" {
		var number json.Number
		if err := json.Unmarshal(body.PointsDelta, &number); err != nil {
			return domain.ManualRankingCorrectionInput{}, errors.New("pointsDelta must be a number")
		}
		parsed, err := strconv.ParseFloat(number.String(), 64)
		if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) || math.Abs(parsed) > 9e15 {
			return domain.ManualRankingCorrectionInput{}, errors.New("pointsDelta must be a finite number")
		}
		points = int64(math.Round(parsed * 100))
	}
	return domain.ManualRankingCorrectionInput{PlayerID: playerID, EffectiveDate: date, TournamentCountDelta: body.TournamentCountDelta, GamesPlayedDelta: body.GamesPlayedDelta, PointsCentsDelta: points, GoalDifferenceDelta: body.GoalDifferenceDelta, Reason: strings.TrimSpace(body.Reason), Administrator: actor, ReplaceCorrectionID: body.ReplaceCorrectionID}, nil
}

func (h *AdminAPIHandler) correctionError(w http.ResponseWriter, err error) {
	if errors.Is(err, ports.ErrNotFound) {
		h.writeError(w, http.StatusNotFound, "player or correction not found")
		return
	}
	if errors.Is(err, ports.ErrVersionConflict) {
		h.writeJSONError(w, http.StatusConflict, "version conflict", map[string]any{"code": "version_conflict"})
		return
	}
	h.writeError(w, http.StatusBadRequest, err.Error())
}

func correctionDTO(value domain.ManualRankingCorrection) map[string]any {
	return map[string]any{"id": value.ID, "playerId": value.PlayerID, "playerKey": value.PlayerKey, "effectiveDate": value.EffectiveDate.Format("2006-01-02"), "effectiveYear": value.EffectiveYear, "tournamentCountDelta": value.TournamentCountDelta, "gamesPlayedDelta": value.GamesPlayedDelta, "pointsCentsDelta": value.PointsCentsDelta, "goalDifferenceDelta": value.GoalDifferenceDelta, "reason": value.Reason, "administrator": value.Administrator, "createdAt": value.CreatedAt, "status": value.Status, "revokedAt": value.RevokedAt, "revokedBy": value.RevokedBy, "revocationReason": value.RevocationReason, "revision": value.Revision, "version": value.Version, "supersedesCorrectionId": value.SupersedesCorrectionID, "replacedByCorrectionId": value.ReplacedByCorrectionID}
}

func aggregateDTO(value domain.PlayerAggregate) map[string]any {
	return map[string]any{"tournamentCount": value.TournamentCount, "gamesPlayed": value.GamesPlayed, "totalPointsCents": value.TotalPointsCents, "pointsPerGameCents": value.PointsPerGameCents, "goalDifference": value.GoalDifference}
}

func manualCorrectionPreviewDTO(value domain.ManualRankingCorrectionPreview) map[string]any {
	result := map[string]any{"player": playerDTOFrom(value.Player), "correction": correctionDTO(value.Correction), "before": aggregateDTO(value.Before), "after": aggregateDTO(value.After), "expectedVersion": value.ExpectedVersion}
	if value.Superseded != nil {
		result["superseded"] = correctionDTO(*value.Superseded)
	}
	return result
}

func manualCorrectionChangeDTO(value domain.ManualRankingCorrectionChange) map[string]any {
	result := map[string]any{"correction": correctionDTO(value.Correction), "before": aggregateDTO(value.Before), "after": aggregateDTO(value.After), "version": value.Version}
	if value.Superseded != nil {
		result["superseded"] = correctionDTO(*value.Superseded)
	}
	return result
}
