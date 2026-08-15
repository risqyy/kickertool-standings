package tournamentapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"kickertool-ranking/internal/adapters/httpclient"
	"kickertool-ranking/internal/domain"
)

func testSource(t *testing.T, handler http.HandlerFunc) (*Source, func()) {
	t.Helper()
	server := httptest.NewServer(handler)
	client := httpclient.New(server.Client(), time.Second, 0, time.Millisecond, "test", nil)
	source, err := NewSource(server.URL, client, "example-api-token", 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	return source, server.Close
}

func TestSourceSmokeAndListingUseExactAuthorization(t *testing.T) {
	var requests []string
	source, closeServer := testSource(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "example-api-token" || strings.Contains(got, "Bearer") {
			t.Fatalf("unexpected authorization header: %q", got)
		}
		requests = append(requests, r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/hello":
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/tournaments":
			if r.URL.Query().Get("offset") == "0" {
				_, _ = w.Write([]byte(`[{"id":"t1","name":"One","state":"finished","date":"2026-08-01T10:00:00+02:00","disciplines":[{"entryType":"monster_dyp"}]},{"id":"t2","name":"Two","state":"running","disciplines":[{"entryType":"monster_dyp"}]}]`))
			} else {
				_, _ = w.Write([]byte(`[{"id":"t3","name":"Three","state":"planned","disciplines":[{"entryType":"monster_dyp"}]}]`))
			}
		case "/tournaments/t1", "/tournaments/t2", "/tournaments/t3":
			_, _ = w.Write([]byte(`{"disciplines":[{"entryType":"monster_dyp","stages":[{"groups":[{"tournamentMode":"monster_dyp"}]}]}]}`))
		default:
			http.NotFound(w, r)
		}
	})
	defer closeServer()

	if err := source.SmokeHello(context.Background()); err != nil {
		t.Fatalf("smoke hello: %v", err)
	}
	tournaments, err := source.FetchTournaments(context.Background())
	if err != nil {
		t.Fatalf("fetch tournaments: %v", err)
	}
	if len(tournaments) != 3 || tournaments[0].SourceID != "t1" || !tournaments[1].IsLive || tournaments[2].Status != "planned" {
		t.Fatalf("unexpected tournaments: %#v", tournaments)
	}
	if tournaments[0].URL != source.baseURL+"/tournaments/t1" {
		t.Fatalf("custom base URL not used: %s", tournaments[0].URL)
	}
	if len(requests) != 6 || requests[0] != "/hello" || !strings.Contains(requests[1], "limit=2") || !strings.Contains(requests[4], "offset=2") {
		t.Fatalf("unexpected requests: %#v", requests)
	}
}

func TestSourceAuthErrorsAreDistinctAndPaginationGuardsDuplicates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/hello" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"example-token-must-not-appear"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/tournaments/same" {
			_, _ = w.Write([]byte(`{"disciplines":[{"entryType":"monster_dyp","stages":[{"groups":[{"tournamentMode":"monster_dyp"}]}]}]}`))
			return
		}
		_, _ = w.Write([]byte(`[{"id":"same","name":"One","state":"finished","disciplines":[{"entryType":"monster_dyp"}]},{"id":"same","name":"Again","state":"finished","disciplines":[{"entryType":"monster_dyp"}]}]`))
	}))
	defer server.Close()
	source, err := NewSource(server.URL, httpclient.New(server.Client(), time.Second, 0, time.Millisecond, "test", nil), "example-api-token", 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = source.SmokeHello(context.Background())
	var authErr *AuthError
	if !errors.As(err, &authErr) || authErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected auth error, got %v", err)
	}
	if strings.Contains(err.Error(), "example-api-token") {
		t.Fatalf("token leaked in error: %v", err)
	}
	if _, err := source.FetchTournaments(context.Background()); err == nil || !strings.Contains(err.Error(), "repeated id") {
		t.Fatalf("expected duplicate pagination error, got %v", err)
	}
}

func TestSourceMapsHierarchyTeamAllocationAndOptionalPoints(t *testing.T) {
	source, closeServer := testSource(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/tournaments/t1":
			_, _ = w.Write([]byte(`{"id":"t1","name":"Example Cup","state":"finished","disciplines":[{"id":"d1","name":"Monster DYP","entryType":"monster_dyp","stages":[{"id":"s1","state":"finished","groups":[{"id":"g1","name":"Example Final","state":"finished","tournamentMode":"monster_dyp"}]}]}]}`))
		case "/tournaments/t1/group/g1/entries":
			_, _ = w.Write([]byte(`[{"id":"team1","name":"Example Team A/B","entries":[{"id":"p1","name":"Player One"},{"id":"p2","name":"Player Two"}]},{"id":"team2","name":"No players","entries":[]}]`))
		case "/tournaments/t1/groups/g1/standings":
			_, _ = w.Write([]byte(`[{"id":"gs1","entry":{"id":"team1","name":"A/B"},"rank":1,"result":1,"points":1.5,"pointsPerMatch":0.75,"correctedPointsPerMatch":0.8,"hasCorrectedValue":true,"matches":2,"goalDifference":3},{"id":"gs2","entryId":"team2","entryName":"No players","rank":2,"points":0}]`))
		default:
			http.NotFound(w, r)
		}
	})
	defer closeServer()

	snapshot, err := source.FetchStandings(context.Background(), domain.Tournament{Source: domain.KickertoolAPISource, SourceID: "t1", Name: "Example Cup", Status: "finished"})
	if err != nil {
		t.Fatalf("fetch standings: %v", err)
	}
	if !snapshot.Complete || len(snapshot.Disciplines) != 1 || len(snapshot.Stages) != 1 || len(snapshot.Groups) != 1 || len(snapshot.Entries) != 2 {
		t.Fatalf("unexpected hierarchy: %#v", snapshot)
	}
	if len(snapshot.GroupStandings) != 2 || len(snapshot.Standings) != 2 || len(snapshot.Allocations) != 2 {
		t.Fatalf("unexpected standings/allocation counts: groups=%d standings=%d allocations=%d", len(snapshot.GroupStandings), len(snapshot.Standings), len(snapshot.Allocations))
	}
	for _, standing := range snapshot.Standings {
		if standing.PointsCents == nil || *standing.PointsCents != 150 || standing.GamesPlayed == nil || *standing.GamesPlayed != 2 {
			t.Fatalf("unexpected allocated standing: %#v", standing)
		}
		if standing.PointsPerMatchCents == nil || *standing.PointsPerMatchCents != 75 || standing.CorrectedPointsPerMatchCents == nil || *standing.CorrectedPointsPerMatchCents != 80 || standing.HasCorrectedValue == nil || !*standing.HasCorrectedValue {
			t.Fatalf("missing points audit fields: %#v", standing)
		}
	}
	if snapshot.GroupStandings[1].PlayerID != "" {
		t.Fatalf("team without members invented a player: %#v", snapshot.GroupStandings[1])
	}
}

func TestMapStandingUnwrapsEntryArrayWithoutUsingStandingID(t *testing.T) {
	standing := mapStanding(map[string]any{
		"id":    "standing-1",
		"entry": []any{map[string]any{"id": "entry-1", "name": "Team"}},
	}, "t1", "d1", "s1", "g1", "https://example.test/standings")
	if standing.EntryID != "entry-1" || standing.EntryID == standing.StandingID || standing.EntryName != "Team" {
		t.Fatalf("entry array was not mapped safely: %#v", standing)
	}
}

func TestSourceRequiresTokenAndHandlesForbidden(t *testing.T) {
	if _, err := NewSource("", nil, "", 0, nil); !errors.Is(err, ErrMissingToken) {
		t.Fatalf("expected missing token error, got %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusForbidden) }))
	defer server.Close()
	source, err := NewSource(server.URL, httpclient.New(server.Client(), time.Second, 0, time.Millisecond, "test", nil), "example-api-token", 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = source.SmokeHello(context.Background())
	var authErr *AuthError
	if !errors.As(err, &authErr) || authErr.StatusCode != http.StatusForbidden {
		t.Fatalf("expected forbidden auth error, got %v", err)
	}
}
