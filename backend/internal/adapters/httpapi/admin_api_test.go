package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
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

type fakeManualCorrectionRepository struct {
	preview        domain.ManualRankingCorrectionPreview
	created        int
	revokeExpected int64
	revoked        int
}

func (f *fakeManualCorrectionRepository) PreviewManualRankingCorrection(context.Context, domain.ManualRankingCorrectionInput) (domain.ManualRankingCorrectionPreview, error) {
	return f.preview, nil
}
func (f *fakeManualCorrectionRepository) CreateManualRankingCorrection(_ context.Context, input domain.ManualRankingCorrectionInput, expectedVersion int64) (domain.ManualRankingCorrectionChange, error) {
	f.created++
	return domain.ManualRankingCorrectionChange{Correction: domain.ManualRankingCorrection{ID: 1, PlayerID: input.PlayerID, EffectiveDate: input.EffectiveDate, EffectiveYear: input.EffectiveDate.Year(), Status: "active", Reason: input.Reason, Administrator: input.Administrator, Revision: 1, Version: 1}, Before: domain.PlayerAggregate{TournamentCount: 1}, After: domain.PlayerAggregate{TournamentCount: 2}, Version: expectedVersion + 1}, nil
}
func (f *fakeManualCorrectionRepository) ListManualRankingCorrections(context.Context, uint) ([]domain.ManualRankingCorrection, error) {
	return []domain.ManualRankingCorrection{}, nil
}
func (f *fakeManualCorrectionRepository) RevokeManualRankingCorrection(_ context.Context, _ uint, _ uint, expected int64, _ string, _ string) (domain.ManualRankingCorrectionRevocation, error) {
	if expected != f.revokeExpected {
		return domain.ManualRankingCorrectionRevocation{}, ports.ErrVersionConflict
	}
	f.revoked++
	return domain.ManualRankingCorrectionRevocation{Correction: domain.ManualRankingCorrection{ID: 1, Status: "revoked"}, Before: domain.PlayerAggregate{TournamentCount: 2}, After: domain.PlayerAggregate{TournamentCount: 1}, Version: expected + 1}, nil
}

func TestManualCorrectionPreviewAndConfirmUseExistingAdminBoundary(t *testing.T) {
	logger := zerolog.Nop()
	directory := fakePlayerDirectory{profiles: map[uint]domain.PlayerProfile{1: {ID: 1, DisplayName: "Player One", CanonicalNameKey: "player one"}}}
	corrections := &fakeManualCorrectionRepository{preview: domain.ManualRankingCorrectionPreview{Player: directory.profiles[1], Correction: domain.ManualRankingCorrection{PlayerID: 1, EffectiveDate: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), EffectiveYear: 2026, Reason: "test", Status: "active"}, Before: domain.PlayerAggregate{TournamentCount: 1}, After: domain.PlayerAggregate{TournamentCount: 2}, ExpectedVersion: 0}}
	admin := NewAdminAPIHandler(&fakeTournamentAdminRepository{}, directory, &fakePlayerMerger{}, &logger, corrections)
	handler := AdminBasicAuth(admin, "example-admin", "example-password", &logger)
	session := httptest.NewRequest(http.MethodGet, "/api/admin/session", nil)
	session.SetBasicAuth("example-admin", "example-password")
	sessionResponse := httptest.NewRecorder()
	handler.ServeHTTP(sessionResponse, session)
	cookie := sessionResponse.Result().Cookies()[0]
	invalidDate := httptest.NewRequest(http.MethodPost, "/api/admin/players/1/corrections/preview", strings.NewReader(`{"effectiveDate":"2026-01-01T00:00:00Z","tournamentCountDelta":1,"reason":"test"}`))
	invalidDate.SetBasicAuth("example-admin", "example-password")
	invalidDate.Header.Set("Content-Type", "application/json")
	invalidDate.Header.Set("X-CSRF-Token", cookie.Value)
	invalidDate.AddCookie(cookie)
	invalidDateResponse := httptest.NewRecorder()
	handler.ServeHTTP(invalidDateResponse, invalidDate)
	if invalidDateResponse.Code != http.StatusBadRequest {
		t.Fatalf("non-date effectiveDate status=%d body=%s", invalidDateResponse.Code, invalidDateResponse.Body.String())
	}
	preview := httptest.NewRequest(http.MethodPost, "/api/admin/players/1/corrections/preview", strings.NewReader(`{"effectiveDate":"2026-01-01","tournamentCountDelta":1,"reason":"test"}`))
	preview.SetBasicAuth("example-admin", "example-password")
	preview.Header.Set("Content-Type", "application/json")
	preview.Header.Set("X-CSRF-Token", cookie.Value)
	preview.AddCookie(cookie)
	previewResponse := httptest.NewRecorder()
	handler.ServeHTTP(previewResponse, preview)
	if previewResponse.Code != http.StatusOK || !strings.Contains(previewResponse.Body.String(), `"token"`) {
		t.Fatalf("preview status=%d body=%s", previewResponse.Code, previewResponse.Body.String())
	}
	var payload struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(previewResponse.Body.Bytes(), &payload); err != nil || payload.Token == "" {
		t.Fatalf("preview payload=%s err=%v", previewResponse.Body.String(), err)
	}
	confirm := httptest.NewRequest(http.MethodPost, "/api/admin/players/corrections/confirm", strings.NewReader(`{"token":"`+payload.Token+`","expectedVersion":0,"confirmed":true}`))
	confirm.SetBasicAuth("example-admin", "example-password")
	confirm.Header.Set("Content-Type", "application/json")
	confirm.Header.Set("X-CSRF-Token", cookie.Value)
	confirm.AddCookie(cookie)
	confirmResponse := httptest.NewRecorder()
	handler.ServeHTTP(confirmResponse, confirm)
	if confirmResponse.Code != http.StatusOK || corrections.created != 1 {
		t.Fatalf("confirm status=%d created=%d body=%s", confirmResponse.Code, corrections.created, confirmResponse.Body.String())
	}
}

func TestManualCorrectionRevokeRequiresCSRFAndRejectsStaleVersion(t *testing.T) {
	logger := zerolog.Nop()
	directory := fakePlayerDirectory{profiles: map[uint]domain.PlayerProfile{1: {ID: 1, DisplayName: "Player One", CanonicalNameKey: "player one", RankingCorrectionVersion: 2}}}
	corrections := &fakeManualCorrectionRepository{revokeExpected: 2}
	admin := NewAdminAPIHandler(&fakeTournamentAdminRepository{}, directory, &fakePlayerMerger{}, &logger, corrections)
	handler := AdminBasicAuth(admin, "example-admin", "example-password", &logger)
	session := httptest.NewRequest(http.MethodGet, "/api/admin/session", nil)
	session.SetBasicAuth("example-admin", "example-password")
	sessionResponse := httptest.NewRecorder()
	handler.ServeHTTP(sessionResponse, session)
	cookie := sessionResponse.Result().Cookies()[0]
	makeRequest := func(token, body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/admin/players/1/corrections/1/revoke", strings.NewReader(body))
		request.SetBasicAuth("example-admin", "example-password")
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-CSRF-Token", token)
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	if response := makeRequest("wrong", `{"expectedVersion":2,"reason":"wrong","confirmed":true}`); response.Code != http.StatusForbidden {
		t.Fatalf("invalid csrf status=%d", response.Code)
	}
	if response := makeRequest(cookie.Value, `{"expectedVersion":1,"reason":"stale","confirmed":true}`); response.Code != http.StatusConflict {
		t.Fatalf("stale revoke status=%d body=%s", response.Code, response.Body.String())
	}
	if response := makeRequest(cookie.Value, `{"expectedVersion":2,"reason":"valid revoke","confirmed":true}`); response.Code != http.StatusOK || corrections.revoked != 1 {
		t.Fatalf("valid revoke status=%d revoked=%d body=%s", response.Code, corrections.revoked, response.Body.String())
	}
}
