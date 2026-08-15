package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"kickertool-ranking/internal/domain"
)

type fakeRankingReader struct {
	ranking []domain.PlayerAggregate
	err     error
}

func (f fakeRankingReader) ListPlayerRanking(context.Context) ([]domain.PlayerAggregate, error) {
	return f.ranking, f.err
}

func TestRankingHandlerJSON(t *testing.T) {
	points := int64(1200)
	handler := NewRankingHandler(fakeRankingReader{ranking: []domain.PlayerAggregate{{PlayerName: "Player One", TotalPointsCents: &points}}}, loggerForTest())
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/standings", nil))

	var rows []rankingRow
	if err := json.Unmarshal(recorder.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	if recorder.Code != http.StatusOK || len(rows) != 1 || rows[0].Rank != 1 || rows[0].PlayerName != "Player One" {
		t.Fatalf("unexpected JSON response: status=%d rows=%+v", recorder.Code, rows)
	}
	if !strings.Contains(recorder.Body.String(), `"total_points":12.00`) || !strings.Contains(recorder.Body.String(), `"points_per_game":null`) {
		t.Fatalf("JSON must preserve exact two-decimal points and null missing PPG: %s", recorder.Body.String())
	}
}

func TestRankingHandlerErrorsAndRoutes(t *testing.T) {
	handler := NewRankingHandler(fakeRankingReader{err: errors.New("database unavailable")}, loggerForTest())

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/standings", nil))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", recorder.Code)
	}

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/standings", nil))
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", recorder.Code)
	}

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/standings", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", recorder.Code)
	}
}

func loggerForTest() *zerolog.Logger {
	logger := zerolog.Nop()
	return &logger
}
