package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"kickertool-ranking/internal/domain"
)

type monthlyRankingReader struct{ periodRankingReader }

func (monthlyRankingReader) ListAvailableRankingMonths(context.Context) ([]domain.RankingMonth, error) {
	return []domain.RankingMonth{{Year: 2026, Month: 9}}, nil
}

func (monthlyRankingReader) ListPlayerRankingForMonth(_ context.Context, year, month int) ([]domain.PlayerAggregate, error) {
	if year == 2026 && month == 9 {
		return []domain.PlayerAggregate{{PlayerName: "Monthly Player"}}, nil
	}
	return nil, nil
}

func TestPublicRankingMonthContract(t *testing.T) {
	handler := StripV1Prefix(NewPublicRankingAPIHandler(monthlyRankingReader{}))
	for _, item := range []struct{ query, expected string }{
		{"?year=2026&month=9", `"name":"Monthly Player"`},
		{"?year=2026&month=09", `"selectedMonth":9`},
		{"?year=2026&month=8", `"items":[]`},
		{"?year=2026", `"selectedMonth":null`},
		{"", `"selectedYear":null`},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/public/rankings"+item.query, nil))
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), item.expected) || !strings.Contains(response.Body.String(), `"availableMonths":[{"year":2026,"month":9}]`) {
			t.Fatalf("query=%s status=%d body=%s", item.query, response.Code, response.Body.String())
		}
	}
	for _, query := range []string{"?month=9", "?year=2026&month=0", "?year=2026&month=13", "?year=2026&month=", "?year=2026&month=9&month=10", "?year=2026&month=-1", "?year=2026&month=1.5", "?year=2026&month=%2B1", "?year=2026&month=001"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/public/rankings"+query, nil))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid query=%s status=%d body=%s", query, response.Code, response.Body.String())
		}
	}
}

func TestPublicRankingLegacyReaderHasEmptyMonthlyMetadata(t *testing.T) {
	response := httptest.NewRecorder()
	NewPublicRankingAPIHandler(fakeRankingReader{}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/public/rankings", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"availableMonths":[]`) || !strings.Contains(response.Body.String(), `"selectedMonth":null`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
