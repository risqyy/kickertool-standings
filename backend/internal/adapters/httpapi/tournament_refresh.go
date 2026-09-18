package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"kickertool-ranking/internal/ports"
)

// WithTournamentRefresher wires the optional application service before serving.
func (h *AdminAPIHandler) WithTournamentRefresher(service ports.TournamentRefresher) *AdminAPIHandler {
	h.refresher = service
	return h
}

func (h *AdminAPIHandler) startTournamentRefresh(w http.ResponseWriter, r *http.Request) {
	value := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/admin/tournaments/"), "/refresh")
	id, err := strconv.ParseUint(value, 10, 32)
	if err != nil || id == 0 {
		h.writeError(w, http.StatusBadRequest, "invalid tournament id")
		return
	}
	if h.refresher == nil {
		h.writeError(w, http.StatusServiceUnavailable, "refresh unavailable")
		return
	}
	job, err := h.refresher.Start(r.Context(), uint(id))
	if err != nil {
		switch {
		case errors.Is(err, ports.ErrSyncBusy):
			h.writeError(w, http.StatusConflict, "sync_busy")
		case errors.Is(err, ports.ErrRefreshSource):
			h.writeError(w, http.StatusConflict, "source_unavailable")
		case errors.Is(err, ports.ErrNotFound):
			h.writeError(w, http.StatusNotFound, "tournament not found")
		default:
			h.writeError(w, http.StatusInternalServerError, "refresh could not start")
		}
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}

func (h *AdminAPIHandler) tournamentRefreshStatus(w http.ResponseWriter, r *http.Request) {
	if h.refresher == nil {
		h.writeError(w, http.StatusServiceUnavailable, "refresh unavailable")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/admin/tournament-refreshes/")
	job, err := h.refresher.Status(r.Context(), id)
	if err != nil {
		if errors.Is(err, ports.ErrNotFound) {
			h.writeError(w, http.StatusNotFound, "refresh status no longer available")
			return
		}
		h.writeError(w, http.StatusInternalServerError, "refresh status unavailable")
		return
	}
	writeJSON(w, http.StatusOK, job)
}
