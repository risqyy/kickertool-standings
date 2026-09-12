package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type statusRankingReader struct {
	periodRankingReader
	last *time.Time
	err  error
}

func (f statusRankingReader) LastSyncAt(context.Context) (*time.Time, error) { return f.last, f.err }
func TestPublicRankingSyncStatusIsIndependentOfPeriod(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 45, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		last   *time.Time
		err    error
		status string
	}{
		{name: "success", last: &now, status: "ok"},
		{name: "never", status: "never"},
		{name: "error", last: &now, err: errors.New("database unavailable"), status: "error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, query := range []string{"", "?year=2026"} {
				recorder := httptest.NewRecorder()
				NewPublicRankingAPIHandler(statusRankingReader{last: tc.last, err: tc.err}).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/public/rankings"+query, nil))
				var response struct {
					LastSyncAt     *time.Time
					LastSyncStatus string
				}
				if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				if recorder.Code != 200 || response.LastSyncStatus != tc.status {
					t.Fatalf("status=%d response=%+v", recorder.Code, response)
				}
				if tc.status == "ok" {
					if response.LastSyncAt == nil || !response.LastSyncAt.Equal(now) {
						t.Fatalf("wrong time: %+v", response)
					}
				} else if response.LastSyncAt != nil {
					t.Fatalf("failure/empty exposed time: %+v", response)
				}
			}
		})
	}
}
