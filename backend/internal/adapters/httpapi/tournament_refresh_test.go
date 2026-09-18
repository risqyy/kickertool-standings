package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"kickertool-ranking/internal/domain"
	"kickertool-ranking/internal/ports"
)

type fakeTournamentRefresher struct {
	err      error
	started  []uint
	statusID string
}

func (f *fakeTournamentRefresher) Start(_ context.Context, id uint) (domain.TournamentRefreshJob, error) {
	f.started = append(f.started, id)
	return domain.TournamentRefreshJob{ID: "job-1", TournamentID: id, State: "running"}, f.err
}
func (f *fakeTournamentRefresher) Status(_ context.Context, id string) (domain.TournamentRefreshJob, error) {
	f.statusID = id
	return domain.TournamentRefreshJob{ID: id, TournamentID: 7, State: "succeeded"}, f.err
}

func TestTournamentRefreshAuthenticationAndMutationProtection(t *testing.T) {
	service := &fakeTournamentRefresher{}
	handler := AdminBasicAuth(StripV1Prefix(NewAdminAPIHandler(nil, nil, nil, nil).WithTournamentRefresher(service)), "admin", "password", nil)
	for _, tc := range []struct {
		name             string
		auth, csrf, json bool
		origin           string
		want             int
	}{
		{"unauthenticated", false, true, true, "", 401},
		{"missing csrf", true, false, true, "", 403},
		{"foreign origin", true, true, true, "https://evil.test", 403},
		{"non json", true, true, false, "", 400},
		{"accepted", true, true, true, "http://example.com", 202},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/tournaments/7/refresh", strings.NewReader("{}"))
			if tc.auth {
				req.SetBasicAuth("admin", "password")
			}
			if tc.csrf {
				req.AddCookie(&http.Cookie{Name: adminCSRFTokenCookie, Value: "token"})
				req.Header.Set("X-CSRF-Token", "token")
			}
			if tc.json {
				req.Header.Set("Content-Type", "application/json")
			}
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			if response.Code != tc.want {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if tc.want == 202 {
				var job domain.TournamentRefreshJob
				if err := json.Unmarshal(response.Body.Bytes(), &job); err != nil || job.ID != "job-1" || job.TournamentID != 7 || job.State != "running" {
					t.Fatalf("job=%+v err=%v", job, err)
				}
			}
		})
	}
	if len(service.started) != 1 || service.started[0] != 7 {
		t.Fatalf("unauthorized starts: %v", service.started)
	}
	for _, authenticated := range []bool{false, true} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/tournament-refreshes/job-1", nil)
		if authenticated {
			req.SetBasicAuth("admin", "password")
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		want := 401
		if authenticated {
			want = 200
		}
		if response.Code != want {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
	}
	if service.statusID != "job-1" {
		t.Fatalf("status id=%q", service.statusID)
	}
}

func TestTournamentRefreshErrors(t *testing.T) {
	for _, tc := range []struct {
		name, method, path string
		err                error
		want               int
		message            string
	}{
		{"busy", "POST", "tournaments/7/refresh", ports.ErrSyncBusy, 409, "sync_busy"},
		{"source", "POST", "tournaments/7/refresh", ports.ErrRefreshSource, 409, "source_unavailable"},
		{"unknown tournament", "POST", "tournaments/7/refresh", ports.ErrNotFound, 404, "tournament not found"},
		{"invalid id", "POST", "tournaments/0/refresh", nil, 400, "invalid tournament id"},
		{"unknown job", "GET", "tournament-refreshes/old", ports.ErrNotFound, 404, "refresh status no longer available"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := &fakeTournamentRefresher{err: tc.err}
			handler := NewAdminAPIHandler(nil, nil, nil, nil).WithTournamentRefresher(service)
			req := httptest.NewRequest(tc.method, "/api/admin/"+tc.path, strings.NewReader("{}"))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-CSRF-Token", "token")
			req.AddCookie(&http.Cookie{Name: adminCSRFTokenCookie, Value: "token"})
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			if response.Code != tc.want || !strings.Contains(response.Body.String(), tc.message) {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}
