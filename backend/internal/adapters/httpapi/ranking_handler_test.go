package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"kickertool-ranking/internal/domain"
)

type periodRankingReader struct {
	ranking        []domain.PlayerAggregate
	byYear         map[int][]domain.PlayerAggregate
	availableYears []int
}

func (f periodRankingReader) ListPlayerRanking(context.Context) ([]domain.PlayerAggregate, error) {
	return f.ranking, nil
}

func (f periodRankingReader) ListPlayerRankingForYear(_ context.Context, year int) ([]domain.PlayerAggregate, error) {
	return f.byYear[year], nil
}

func (f periodRankingReader) ListAvailableRankingYears(context.Context) ([]int, error) {
	return f.availableYears, nil
}

func (f periodRankingReader) LastSyncAt(context.Context) (*time.Time, error) { return nil, nil }

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

func TestPublicRankingYearContractAndValidation(t *testing.T) {
	points := int64(125)
	reader := periodRankingReader{
		byYear:         map[int][]domain.PlayerAggregate{2025: {{PlayerName: "Player One", TotalPointsCents: &points, Trend: domain.RankingTrendUp}}},
		availableYears: []int{2026, 2025},
	}
	handler := NewPublicRankingAPIHandler(reader)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/public/rankings?year=2025", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"selectedYear":2025`) || !strings.Contains(response.Body.String(), `"availableYears":[2026,2025]`) || !strings.Contains(response.Body.String(), `"name":"Player One"`) || !strings.Contains(response.Body.String(), `"trend":"up"`) {
		t.Fatalf("valid year response=%d body=%s", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/public/rankings?year=2024", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"items":[]`) || !strings.Contains(response.Body.String(), `"selectedYear":2024`) || !strings.Contains(response.Body.String(), `"availableYears":[2026,2025]`) {
		t.Fatalf("unavailable year response=%d body=%s", response.Code, response.Body.String())
	}

	for _, invalid := range []string{"20", "202A", "0000", "0999", "2025&year=2026"} {
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/public/rankings?year="+invalid, nil))
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "four-digit calendar year") {
			t.Fatalf("invalid year %q response=%d body=%s", invalid, response.Code, response.Body.String())
		}
	}
}

func TestPublicRankingSerializesEveryTrendState(t *testing.T) {
	reader := periodRankingReader{ranking: []domain.PlayerAggregate{
		{PlayerName: "Up", Trend: domain.RankingTrendUp},
		{PlayerName: "Down", Trend: domain.RankingTrendDown},
		{PlayerName: "Same", Trend: domain.RankingTrendSame},
		{PlayerName: "New", Trend: domain.RankingTrendNew},
	}}
	response := httptest.NewRecorder()
	NewPublicRankingAPIHandler(reader).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/public/rankings", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("trend response=%d body=%s", response.Code, response.Body.String())
	}
	for _, trend := range []string{`"trend":"up"`, `"trend":"down"`, `"trend":"same"`, `"trend":"new"`} {
		if !strings.Contains(response.Body.String(), trend) {
			t.Fatalf("missing %s in response=%s", trend, response.Body.String())
		}
	}
}

func TestVersionedPublicRankingEmptyAvailableYearsIsArray(t *testing.T) {
	handler := StripV1Prefix(NewPublicRankingAPIHandler(periodRankingReader{availableYears: []int{}}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/public/rankings", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("empty available years response=%d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		AvailableYears json.RawMessage `json:"availableYears"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode empty available years response: %v", err)
	}
	if got := string(payload.AvailableYears); got != "[]" {
		t.Fatalf("availableYears must be an empty JSON array, got %s", got)
	}
}

func loggerForTest() *zerolog.Logger {
	logger := zerolog.Nop()
	return &logger
}
