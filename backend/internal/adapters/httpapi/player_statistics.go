package httpapi

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"kickertool-ranking/internal/domain"
	"kickertool-ranking/internal/ports"
)

func (h *AdminAPIHandler) playerList(w http.ResponseWriter, r *http.Request) {
	repository, ok := h.directory.(ports.PlayerStatisticsRepository)
	if !ok {
		h.writeError(w, http.StatusServiceUnavailable, "player statistics unavailable")
		return
	}
	filter := domain.PlayerListFilter{Query: r.URL.Query().Get("q"), State: r.URL.Query().Get("state"), Page: 1, Limit: 25}
	if filter.State != "" && filter.State != "all" && filter.State != "active" && filter.State != "merged" {
		h.writeError(w, http.StatusBadRequest, "invalid player state")
		return
	}
	for _, parameter := range []struct {
		name   string
		target *int
		max    int
	}{{"page", &filter.Page, 1000000}, {"limit", &filter.Limit, 100}} {
		if raw := r.URL.Query().Get(parameter.name); raw != "" {
			value, err := strconv.Atoi(raw)
			if err != nil || value < 1 || value > parameter.max {
				h.writeError(w, http.StatusBadRequest, "invalid "+parameter.name)
				return
			}
			*parameter.target = value
		}
	}
	page, err := repository.ListPlayers(r.Context(), filter)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "players could not be loaded")
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (h *AdminAPIHandler) playerStatistics(w http.ResponseWriter, r *http.Request) {
	repository, ok := h.directory.(ports.PlayerStatisticsRepository)
	if !ok {
		h.writeError(w, http.StatusServiceUnavailable, "player statistics unavailable")
		return
	}
	raw := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/admin/players/"), "/statistics")
	id, err := strconv.ParseUint(raw, 10, 32)
	if err != nil || id == 0 {
		h.writeError(w, http.StatusBadRequest, "invalid player id")
		return
	}
	statistics, err := repository.GetPlayerStatistics(r.Context(), uint(id))
	if err != nil {
		if errors.Is(err, ports.ErrNotFound) {
			h.writeError(w, http.StatusNotFound, "player not found")
		} else {
			h.writeError(w, http.StatusInternalServerError, "player statistics could not be loaded")
		}
		return
	}
	for index := range statistics.Tournaments {
		statistics.Tournaments[index].URL = safeTournamentURL(statistics.Tournaments[index].URL)
	}
	corrections := make([]map[string]any, 0, len(statistics.Corrections))
	for _, correction := range statistics.Corrections {
		corrections = append(corrections, map[string]any{"correction": correctionDTO(correction), "effective": correction.Status == "active" && !correction.EffectiveDate.After(statistics.ComputedAt)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"requestedPlayer": statistics.RequestedPlayer, "player": playerDTOFrom(statistics.Player), "tournaments": statistics.Tournaments, "corrections": corrections, "computedAt": statistics.ComputedAt})
}

func safeTournamentURL(value string) string {
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" || parsed.User != nil {
		return ""
	}
	return parsed.String()
}
