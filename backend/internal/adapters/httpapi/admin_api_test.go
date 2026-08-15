package httpapi

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"kickertool-ranking/internal/domain"
	"kickertool-ranking/internal/ports"
)

type fakeTournamentAdminRepository struct {
	page      domain.TournamentPage
	dashboard domain.Dashboard
	changes   []struct {
		id       uint
		included bool
		version  int64
	}
}

func (f *fakeTournamentAdminRepository) ListTournaments(context.Context, domain.TournamentListFilter) (domain.TournamentPage, error) {
	return f.page, nil
}
func (f *fakeTournamentAdminRepository) GetDashboard(context.Context) (domain.Dashboard, error) {
	return f.dashboard, nil
}
func (f *fakeTournamentAdminRepository) SetTournamentRankingInclusion(_ context.Context, id uint, included bool, version int64, _ string) (domain.TournamentInclusionChange, error) {
	f.changes = append(f.changes, struct {
		id       uint
		included bool
		version  int64
	}{id, included, version})
	return domain.TournamentInclusionChange{Changed: true, Tournament: domain.TournamentAdminRow{Tournament: domain.Tournament{ID: id, Name: "Test", IncludedInRanking: included}, InclusionVersion: version + 1}}, nil
}

func adminAuth(username, password string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))
}

func TestAdminAPIRequiresBasicAuthAndCSRF(t *testing.T) {
	logger := zerolog.Nop()
	repo := &fakeTournamentAdminRepository{dashboard: domain.Dashboard{TournamentCount: 3}}
	handler := AdminBasicAuth(NewAdminAPIHandler(repo, fakePlayerDirectory{profiles: map[uint]domain.PlayerProfile{}}, &fakePlayerMerger{}, &logger), "example-admin", "example-password", &logger)

	unauthenticated := httptest.NewRequest(http.MethodGet, "/api/admin/dashboard", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, unauthenticated)
	if response.Code != http.StatusUnauthorized || response.Header().Get("WWW-Authenticate") != `Basic realm="Kickertool Admin", charset="UTF-8"` || strings.Contains(response.Body.String(), "example-password") {
		t.Fatalf("unauthenticated response=%d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}

	session := httptest.NewRequest(http.MethodGet, "/api/admin/session", nil)
	session.Header.Set("Authorization", adminAuth("example-admin", "example-password"))
	sessionResponse := httptest.NewRecorder()
	handler.ServeHTTP(sessionResponse, session)
	if sessionResponse.Code != http.StatusOK || len(sessionResponse.Result().Cookies()) == 0 {
		t.Fatalf("session response=%d body=%s", sessionResponse.Code, sessionResponse.Body.String())
	}
	cookie := sessionResponse.Result().Cookies()[0]

	mutation := httptest.NewRequest(http.MethodPatch, "/api/admin/tournaments/9/inclusion", strings.NewReader("{\"included\":false,\"expectedVersion\":1}"))
	mutation.Header.Set("Authorization", adminAuth("example-admin", "example-password"))
	mutation.Header.Set("Content-Type", "application/json")
	mutation.AddCookie(cookie)
	mutationResponse := httptest.NewRecorder()
	handler.ServeHTTP(mutationResponse, mutation)
	if mutationResponse.Code != http.StatusForbidden || !strings.HasPrefix(mutationResponse.Header().Get("Content-Type"), "application/json") || !strings.Contains(mutationResponse.Body.String(), "invalid csrf token") {
		t.Fatalf("missing csrf status=%d body=%s", mutationResponse.Code, mutationResponse.Body.String())
	}

	mutation.Header.Set("X-CSRF-Token", cookie.Value)
	mutationResponse = httptest.NewRecorder()
	handler.ServeHTTP(mutationResponse, mutation)
	if mutationResponse.Code != http.StatusOK || len(repo.changes) != 1 {
		t.Fatalf("valid mutation status=%d changes=%+v body=%s", mutationResponse.Code, repo.changes, mutationResponse.Body.String())
	}
}

func TestAdminSessionCookieAndPreviewWorkThroughSameOriginProxy(t *testing.T) {
	logger := zerolog.Nop()
	directory := fakePlayerDirectory{profiles: map[uint]domain.PlayerProfile{
		1: {ID: 1, DisplayName: "Quelle", CanonicalNameKey: "quelle"},
		2: {ID: 2, DisplayName: "Ziel", CanonicalNameKey: "ziel"},
	}}
	admin := NewAdminAPIHandler(&fakeTournamentAdminRepository{}, directory, &fakePlayerMerger{}, &logger)
	handler := AdminBasicAuth(admin, "example-admin", "example-password", &logger)
	server := httptest.NewServer(handler)
	defer server.Close()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	sessionRequest, err := http.NewRequest(http.MethodGet, server.URL+"/api/admin/session", nil)
	if err != nil {
		t.Fatal(err)
	}
	sessionRequest.SetBasicAuth("example-admin", "example-password")
	sessionResponse, err := client.Do(sessionRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer sessionResponse.Body.Close()
	if sessionResponse.StatusCode != http.StatusOK || len(sessionResponse.Cookies()) != 1 {
		t.Fatalf("session status=%d cookies=%v", sessionResponse.StatusCode, sessionResponse.Cookies())
	}
	cookie := sessionResponse.Cookies()[0]
	if cookie.Path != "/" || cookie.SameSite != http.SameSiteStrictMode || cookie.Secure {
		t.Fatalf("development cookie attributes=%+v", cookie)
	}

	previewRequest, err := http.NewRequest(http.MethodPost, server.URL+"/api/admin/players/merge/preview", strings.NewReader(`{"sourcePlayerId":1,"targetPlayerId":2}`))
	if err != nil {
		t.Fatal(err)
	}
	previewRequest.SetBasicAuth("example-admin", "example-password")
	previewRequest.Header.Set("Origin", server.URL)
	previewRequest.Header.Set("Content-Type", "application/json")
	previewRequest.Header.Set("X-CSRF-Token", cookie.Value)
	previewResponse, err := client.Do(previewRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer previewResponse.Body.Close()
	if previewResponse.StatusCode != http.StatusOK {
		t.Fatalf("preview status=%d content-type=%q", previewResponse.StatusCode, previewResponse.Header.Get("Content-Type"))
	}

	invalidRequest, err := http.NewRequest(http.MethodPost, server.URL+"/api/admin/players/merge/preview", strings.NewReader(`{"sourcePlayerId":1,"targetPlayerId":2}`))
	if err != nil {
		t.Fatal(err)
	}
	invalidRequest.SetBasicAuth("example-admin", "example-password")
	invalidRequest.Header.Set("Origin", server.URL)
	invalidRequest.Header.Set("Content-Type", "application/json")
	invalidRequest.AddCookie(cookie)
	invalidRequest.Header.Set("X-CSRF-Token", "wrong")
	invalidResponse, err := client.Do(invalidRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer invalidResponse.Body.Close()
	if invalidResponse.StatusCode != http.StatusForbidden || !strings.HasPrefix(invalidResponse.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("invalid csrf status=%d content-type=%q", invalidResponse.StatusCode, invalidResponse.Header.Get("Content-Type"))
	}
}

func TestAdminSessionAndPreviewAcceptForwardedHTTPSHost(t *testing.T) {
	logger := zerolog.Nop()
	directory := fakePlayerDirectory{profiles: map[uint]domain.PlayerProfile{
		1: {ID: 1, DisplayName: "Quelle", CanonicalNameKey: "quelle"},
		2: {ID: 2, DisplayName: "Ziel", CanonicalNameKey: "ziel"},
	}}
	admin := NewAdminAPIHandler(&fakeTournamentAdminRepository{}, directory, &fakePlayerMerger{}, &logger)
	handler := AdminBasicAuth(admin, "example-admin", "example-password", &logger)

	session := httptest.NewRequest(http.MethodGet, "https://public.example:5173/api/admin/session", nil)
	session.Host = "public.example:5173"
	session.Header.Set("X-Forwarded-Proto", "https")
	session.SetBasicAuth("example-admin", "example-password")
	sessionResponse := httptest.NewRecorder()
	handler.ServeHTTP(sessionResponse, session)
	if sessionResponse.Code != http.StatusOK || len(sessionResponse.Result().Cookies()) != 1 {
		t.Fatalf("forwarded session status=%d cookies=%v", sessionResponse.Code, sessionResponse.Result().Cookies())
	}
	cookie := sessionResponse.Result().Cookies()[0]
	if !cookie.Secure || cookie.Path != "/" || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("forwarded cookie attributes=%+v", cookie)
	}

	preview := httptest.NewRequest(http.MethodPost, "https://public.example:5173/api/admin/players/merge/preview", strings.NewReader(`{"sourcePlayerId":1,"targetPlayerId":2}`))
	preview.Host = "public.example:5173"
	preview.Header.Set("Origin", "https://public.example:5173")
	preview.Header.Set("X-Forwarded-Proto", "https")
	preview.Header.Set("Content-Type", "application/json")
	preview.Header.Set("X-CSRF-Token", cookie.Value)
	preview.AddCookie(cookie)
	preview.SetBasicAuth("example-admin", "example-password")
	previewResponse := httptest.NewRecorder()
	handler.ServeHTTP(previewResponse, preview)
	if previewResponse.Code != http.StatusOK {
		t.Fatalf("forwarded preview status=%d body=%s", previewResponse.Code, previewResponse.Body.String())
	}
}

func TestPublicRankingAPIIsUnauthenticatedAndExplicit(t *testing.T) {
	now := time.Now()
	points := int64(150)
	reader := fakePublicRankingReader{rows: []domain.PlayerAggregate{{PlayerName: "Player One", TournamentCount: 2, TotalPointsCents: &points, RecalculatedAt: now}}}
	handler := NewPublicRankingAPIHandler(reader)
	request := httptest.NewRequest(http.MethodGet, "/api/public/rankings", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "\"includedTournamentCount\":2") || !strings.Contains(response.Body.String(), "\"totalPoints\":\"1.50\"") {
		t.Fatalf("public response=%d body=%s", response.Code, response.Body.String())
	}
}

func TestVersionedPublicRankingRouteStripsOnlyV1Prefix(t *testing.T) {
	points := int64(150)
	handler := StripV1Prefix(NewPublicRankingAPIHandler(fakePublicRankingReader{rows: []domain.PlayerAggregate{{PlayerName: "Player One", TotalPointsCents: &points}}}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/public/rankings", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Player One") {
		t.Fatalf("versioned public response=%d body=%s", response.Code, response.Body.String())
	}
}

func TestVersionedAdminRouteKeepsBasicAuthBoundary(t *testing.T) {
	logger := zerolog.Nop()
	admin := NewAdminAPIHandler(&fakeTournamentAdminRepository{}, fakePlayerDirectory{profiles: map[uint]domain.PlayerProfile{}}, &fakePlayerMerger{}, &logger)
	handler := AdminBasicAuth(StripV1Prefix(admin), "example-admin", "example-password", &logger)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/session", nil)
	request.Header.Set("Authorization", adminAuth("example-admin", "example-password"))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("versioned admin response=%d body=%s", response.Code, response.Body.String())
	}
}

type fakePublicRankingReader struct{ rows []domain.PlayerAggregate }

func (f fakePublicRankingReader) ListPlayerRanking(context.Context) ([]domain.PlayerAggregate, error) {
	return f.rows, nil
}
func (f fakePublicRankingReader) LastSyncAt(context.Context) (*time.Time, error) { return nil, nil }

func TestAdminVersionErrorIsMappable(t *testing.T) {
	if ports.ErrVersionConflict == nil {
		t.Fatal("version conflict error must be exported")
	}
}
