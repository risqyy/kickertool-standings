package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"kickertool-ranking/internal/domain"
	"kickertool-ranking/internal/ports"
)

type fakePlayerStatisticsRepository struct {
	fakePlayerDirectory
	filter domain.PlayerListFilter
	id     uint
	err    error
}

func (f *fakePlayerStatisticsRepository) ListPlayers(_ context.Context, filter domain.PlayerListFilter) (domain.PlayerPage, error) {
	f.filter = filter
	return domain.PlayerPage{Items: []domain.PlayerSummary{{ID: 7, DisplayName: "Test Player", Active: true}}, Page: filter.Page, Limit: filter.Limit, Total: 51}, f.err
}

func (f *fakePlayerStatisticsRepository) GetPlayerStatistics(_ context.Context, id uint) (domain.PlayerStatistics, error) {
	f.id = id
	now := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	rank, standingID := 6, "source-result"
	return domain.PlayerStatistics{RequestedPlayer: domain.PlayerSummary{ID: id, DisplayName: "Test Player", Active: true}, Player: domain.PlayerProfile{ID: id, DisplayName: "Test Player"}, ComputedAt: now,
		Tournaments: []domain.PlayerTournamentContribution{{ID: 1, TournamentID: 2, URL: "javascript:alert(1)", Reason: "zero_games"}, {ID: 3, TournamentID: 4, URL: "https://example.test/4", Reason: "counted", StandingRank: &rank, StandingSourceID: &standingID, StandingKey: "final/source-result", SourcePlayerName: "Source Alias"}},
		Corrections: []domain.ManualRankingCorrection{
			{ID: 1, Status: "active", EffectiveDate: now.Add(-time.Hour)},
			{ID: 2, Status: "active", EffectiveDate: now.Add(time.Hour)},
			{ID: 3, Status: "revoked", EffectiveDate: now.Add(-time.Hour)},
			{ID: 4, Status: "replaced", EffectiveDate: now.Add(-time.Hour)},
		},
	}, f.err
}

func TestPlayerStatisticsAndListRequireAdminAuthentication(t *testing.T) {
	for _, path := range []string{"/api/v1/admin/players", "/api/v1/admin/players/7/statistics"} {
		repository := &fakePlayerStatisticsRepository{}
		handler := AdminBasicAuth(StripV1Prefix(NewAdminAPIHandler(nil, repository, nil, nil)), "admin", "password", nil)
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != 401 || repository.id != 0 || repository.filter.Page != 0 {
			t.Fatalf("unauthenticated read=%d", response.Code)
		}
		request.SetBasicAuth("admin", "password")
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != 200 {
			t.Fatalf("authenticated read=%d %s", response.Code, response.Body.String())
		}
	}
}

func TestPlayerStatisticsJSONContractAndValidation(t *testing.T) {
	repository := &fakePlayerStatisticsRepository{}
	handler := NewAdminAPIHandler(nil, repository, nil, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/api/admin/players?q=Test&state=merged&page=2&limit=50", nil))
	if response.Code != 200 || repository.filter.Query != "Test" || repository.filter.State != "merged" || repository.filter.Page != 2 || repository.filter.Limit != 50 {
		t.Fatalf("filter=%+v status=%d", repository.filter, response.Code)
	}
	for _, path := range []string{"players?page=0", "players?page=999999999999999999999", "players?limit=101", "players?state=invalid", "players/0/statistics", "players/abc/statistics"} {
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest("GET", "/api/admin/"+path, nil))
		if response.Code != 400 {
			t.Fatalf("path=%s status=%d", path, response.Code)
		}
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/api/admin/players/7/statistics", nil))
	var result struct {
		Requested   domain.PlayerSummary                  `json:"requestedPlayer"`
		Player      playerDTO                             `json:"player"`
		Tournaments []domain.PlayerTournamentContribution `json:"tournaments"`
		Corrections []struct {
			Effective  bool           `json:"effective"`
			Correction map[string]any `json:"correction"`
		} `json:"corrections"`
		ComputedAt time.Time `json:"computedAt"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if response.Code != 200 || result.Requested.ID != 7 || result.Player.ID != 7 || result.ComputedAt.IsZero() || len(result.Corrections) != 4 || result.Tournaments[0].URL != "" || result.Tournaments[1].URL != "https://example.test/4" {
		t.Fatalf("contract=%+v status=%d", result, response.Code)
	}
	for i, correction := range result.Corrections {
		if correction.Effective != (i == 0) || correction.Correction["effectiveDate"] == nil {
			t.Fatalf("correction=%+v", correction)
		}
	}
	known, unknown := result.Tournaments[1], result.Tournaments[0]
	if known.StandingRank == nil || *known.StandingRank != 6 || known.StandingSourceID == nil || *known.StandingSourceID != "source-result" || known.StandingKey != "final/source-result" || known.SourcePlayerName != "Source Alias" || unknown.StandingRank != nil || unknown.StandingSourceID != nil {
		t.Fatalf("standing provenance=%+v", result.Tournaments)
	}
	var wire map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &wire); err != nil {
		t.Fatal(err)
	}
	unknownWire := wire["tournaments"].([]any)[0].(map[string]any)
	for _, key := range []string{"standingRank", "standingSourceId"} {
		if value, exists := unknownWire[key]; !exists || value != nil {
			t.Fatalf("missing source values must be explicit null: %s=%v (exists=%v)", key, value, exists)
		}
	}
	repository.err = ports.ErrNotFound
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/api/admin/players/999/statistics", nil))
	if response.Code != 404 {
		t.Fatalf("unknown=%d", response.Code)
	}
}

func TestPlayerStatisticsLinksAllowOnlyAbsoluteHTTPWithoutCredentials(t *testing.T) {
	for _, input := range []string{"javascript:alert(1)", "//example.test/t", "/relative", "data:text/html,x", "https://user:password@example.test/t", "https:///missing-host"} {
		if got := safeTournamentURL(input); got != "" {
			t.Fatalf("unsafe URL retained: %q", got)
		}
	}
	if got := safeTournamentURL("https://example.test/t?id=1#results"); got != "https://example.test/t?id=1#results" {
		t.Fatalf("safe URL changed: %q", got)
	}
}
