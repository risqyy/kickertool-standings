package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"kickertool-ranking/internal/domain"
)

type fakePlayerCreator struct {
	result domain.PlayerCreationResult
	input  domain.PlayerCreationInput
	called int
}

type fakePlayerCreationDirectory struct {
	fakePlayerDirectory
	creator *fakePlayerCreator
}

func (f fakePlayerCreationDirectory) CreateManualPlayer(ctx context.Context, input domain.PlayerCreationInput) (domain.PlayerCreationResult, error) {
	return f.creator.CreateManualPlayer(ctx, input)
}

func (f *fakePlayerCreator) CreateManualPlayer(_ context.Context, input domain.PlayerCreationInput) (domain.PlayerCreationResult, error) {
	f.called++
	f.input = input
	return f.result, nil
}

func TestManualPlayerCreateUsesAdminAuthCSRFValidationAndActor(t *testing.T) {
	logger := zerolog.Nop()
	creator := &fakePlayerCreator{result: domain.PlayerCreationResult{
		Created: true, CreatedAt: time.Date(2026, time.August, 23, 11, 0, 0, 0, time.UTC), Administrator: "operator", Origin: "manual",
		Player: domain.PlayerProfile{ID: 9, DisplayName: "New Player", CanonicalNameKey: "new player", Active: true},
	}}
	directory := fakePlayerCreationDirectory{fakePlayerDirectory: fakePlayerDirectory{profiles: map[uint]domain.PlayerProfile{}}, creator: creator}
	handler := AdminBasicAuth(NewAdminAPIHandler(&fakeTournamentAdminRepository{}, directory, &fakePlayerMerger{}, &logger), "example-admin", "example-password", &logger)

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, "/api/admin/players", strings.NewReader(`{"displayName":"New Player","confirmed":true}`)))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d", unauthorized.Code)
	}

	cookie := adminSessionCookie(t, handler)
	missingCSRF := createPlayerRequest(cookie, "", `{"displayName":"New Player","confirmed":true}`)
	missingCSRF.SetBasicAuth("example-admin", "example-password")
	missingResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingResponse, missingCSRF)
	if missingResponse.Code != http.StatusForbidden || creator.called != 0 {
		t.Fatalf("missing csrf status=%d calls=%d body=%s", missingResponse.Code, creator.called, missingResponse.Body.String())
	}

	badOrigin := createPlayerRequest(cookie, cookie.Value, `{"displayName":"New Player","confirmed":true}`)
	badOrigin.SetBasicAuth("example-admin", "example-password")
	badOrigin.Header.Set("Origin", "https://attacker.example")
	badOrigin.Host = "public.example"
	badOriginResponse := httptest.NewRecorder()
	handler.ServeHTTP(badOriginResponse, badOrigin)
	if badOriginResponse.Code != http.StatusForbidden || creator.called != 0 {
		t.Fatalf("bad origin status=%d calls=%d body=%s", badOriginResponse.Code, creator.called, badOriginResponse.Body.String())
	}

	for _, body := range []string{
		`{"displayName":"   ","confirmed":true}`,
		`{"displayName":"x","confirmed":true}`,
		`{"displayName":"New Player","confirmed":false}`,
		`{"displayName":"A` + strings.Repeat(" ", domain.MaxPlayerDisplayNameLength) + `B","confirmed":true}`,
	} {
		request := createPlayerRequest(cookie, cookie.Value, body)
		request.SetBasicAuth("example-admin", "example-password")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest || creator.called != 0 {
			t.Fatalf("validation status=%d calls=%d body=%s", response.Code, creator.called, response.Body.String())
		}
	}

	request := createPlayerRequest(cookie, cookie.Value, `{"displayName":"  New Player  ","confirmed":true}`)
	request.SetBasicAuth("example-admin", "example-password")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || creator.called != 1 || creator.input.DisplayName != "New Player" || creator.input.Administrator != "example-admin" {
		t.Fatalf("create status=%d calls=%d input=%+v body=%s", response.Code, creator.called, creator.input, response.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil || payload["created"] != true {
		t.Fatalf("create payload=%s err=%v", response.Body.String(), err)
	}
}

func TestManualPlayerCreateReturnsActivePlayerOnConflict(t *testing.T) {
	logger := zerolog.Nop()
	creator := &fakePlayerCreator{result: domain.PlayerCreationResult{Player: domain.PlayerProfile{ID: 14, DisplayName: "Merged Root", CanonicalNameKey: "merged root", Active: true}, Created: false}}
	directory := fakePlayerCreationDirectory{fakePlayerDirectory: fakePlayerDirectory{profiles: map[uint]domain.PlayerProfile{}}, creator: creator}
	handler := AdminBasicAuth(NewAdminAPIHandler(&fakeTournamentAdminRepository{}, directory, &fakePlayerMerger{}, &logger), "example-admin", "example-password", &logger)
	cookie := adminSessionCookie(t, handler)
	request := createPlayerRequest(cookie, cookie.Value, `{"displayName":"Alias Name","confirmed":true}`)
	request.SetBasicAuth("example-admin", "example-password")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"code":"player_exists"`) || !strings.Contains(response.Body.String(), `"id":14`) {
		t.Fatalf("conflict status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestVersionedManualPlayerCreateRoute(t *testing.T) {
	logger := zerolog.Nop()
	creator := &fakePlayerCreator{result: domain.PlayerCreationResult{
		Created: true, CreatedAt: time.Date(2026, time.August, 23, 11, 0, 0, 0, time.UTC), Administrator: "example-admin", Origin: "manual",
		Player: domain.PlayerProfile{ID: 22, DisplayName: "Versioned Player", CanonicalNameKey: "versioned player", Active: true},
	}}
	directory := fakePlayerCreationDirectory{fakePlayerDirectory: fakePlayerDirectory{profiles: map[uint]domain.PlayerProfile{}}, creator: creator}
	admin := NewAdminAPIHandler(&fakeTournamentAdminRepository{}, directory, &fakePlayerMerger{}, &logger)
	handler := StripV1Prefix(AdminBasicAuth(admin, "example-admin", "example-password", &logger))

	sessionRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/session", nil)
	sessionRequest.SetBasicAuth("example-admin", "example-password")
	sessionResponse := httptest.NewRecorder()
	handler.ServeHTTP(sessionResponse, sessionRequest)
	if sessionResponse.Code != http.StatusOK || len(sessionResponse.Result().Cookies()) != 1 {
		t.Fatalf("versioned session status=%d body=%s", sessionResponse.Code, sessionResponse.Body.String())
	}
	cookie := sessionResponse.Result().Cookies()[0]
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/players", strings.NewReader(`{"displayName":"Versioned Player","confirmed":true}`))
	request.SetBasicAuth("example-admin", "example-password")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", cookie.Value)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || creator.called != 1 {
		t.Fatalf("versioned creation status=%d calls=%d body=%s", response.Code, creator.called, response.Body.String())
	}
}

func adminSessionCookie(t *testing.T, handler http.Handler) *http.Cookie {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/api/admin/session", nil)
	request.SetBasicAuth("example-admin", "example-password")
	// The caller may use different credentials; Basic Auth is replaced by the
	// request in tests that need a custom credential pair.
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("session status=%d body=%s", response.Code, response.Body.String())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("session cookies=%v", cookies)
	}
	return cookies[0]
}

func createPlayerRequest(cookie *http.Cookie, csrf, body string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/api/admin/players", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", csrf)
	request.AddCookie(cookie)
	return request
}
