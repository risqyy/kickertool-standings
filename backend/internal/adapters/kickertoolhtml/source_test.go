package kickertoolhtml

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"kickertool-ranking/internal/domain"
)

func TestSourceDiscoversRelativePaginationAndStandingsWithoutAuthorization(t *testing.T) {
	var authorization []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = append(authorization, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.URL.Query().Get("page") == "2" {
			_, _ = w.Write([]byte(`<a href="/community/tournaments/t2">Monster DYP Example Cup Two finished</a>`))
			return
		}
		switch r.URL.Path {
		case "/community":
			_, _ = w.Write([]byte(`<a href="/community/tournaments/t1">Monster DYP Example Cup One finished</a><a href="https://foreign.test/tournaments/no">foreign</a><script>const nextUrl = "/community?page=2";</script>`))
		case "/community/tournaments/t1":
			_, _ = w.Write([]byte(`<h1>Example Cup One</h1><div class="discipline-type">Monster DYP</div><table><tr><th>#</th><th>Player</th><th>Points</th><th>Matches</th><th>Tor-Diff</th></tr><tr data-standing-id="r1" data-player-id="p1"><td>1</td><td>Player One</td><td>1,50</td><td>2</td><td>3</td></tr></table>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	source, err := NewSource(server.URL+"/community", server.Client(), nil)
	if err != nil {
		t.Fatal(err)
	}
	tournaments, err := source.FetchTournaments(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tournaments) != 2 || tournaments[0].Source != domain.KickertoolHTMLSource {
		t.Fatalf("tournaments=%+v", tournaments)
	}
	snapshot, err := source.FetchStandings(context.Background(), tournaments[0])
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Complete || len(snapshot.Standings) != 1 {
		t.Fatalf("snapshot=%+v", snapshot)
	}
	standing := snapshot.Standings[0]
	if standing.PlayerKey != domain.PlayerKey("Player One") || standing.PointsCents == nil || *standing.PointsCents != 150 || standing.GamesPlayed == nil || *standing.GamesPlayed != 2 || standing.GoalDifference == nil || *standing.GoalDifference != 3 {
		t.Fatalf("standing=%+v", standing)
	}
	for _, value := range authorization {
		if strings.TrimSpace(value) != "" {
			t.Fatalf("HTML request carried authorization header %q", value)
		}
	}
}

func TestSourceDiscoversSeparateStandingsAndDynamicJSONEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		switch r.URL.Path {
		case "/community/tournaments/t3":
			_, _ = w.Write([]byte(`<h1>Example Cup</h1><section class="discipline"><span class="entry-type">Monster DYP</span><a href="/community/tournaments/t3/groups/g1/standings">Group standings</a></section><script>fetch("/community/xhr/t3/group/g1/standings.json")</script>`))
		case "/community/tournaments/t3/groups/g1/standings":
			_, _ = w.Write([]byte(`<div data-entry-type="monster_dyp"></div><table><tr><th>Rank</th><th>Player</th><th>Points</th><th>Matches</th></tr><tr data-standing-id="r1" data-entry-id="e1" data-player-id="p1"><td>1</td><td>Player One</td><td>2,00</td><td>4</td></tr></table>`))
		case "/community/xhr/t3/group/g1/standings.json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"entryType":"monster_dyp","standings":[{"id":"r2","rank":2,"entry":{"id":"e2","name":"Example Team B"},"player":{"id":"p2","name":"Player Two"},"points":1.5,"matches":3}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	source, err := NewSource(server.URL+"/community", server.Client(), nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := source.FetchStandings(context.Background(), domain.Tournament{Source: SourceName, SourceID: "t3", SourceKey: "t3", Status: "finished", URL: server.URL + "/community/tournaments/t3"})
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Complete || len(snapshot.Standings) != 2 || len(snapshot.GroupStandings) != 2 {
		t.Fatalf("discovered snapshot=%+v", snapshot)
	}
	if snapshot.Standings[0].PlayerID == "" || snapshot.Standings[1].PlayerID == "" {
		t.Fatalf("player IDs not allocated: %+v", snapshot.Standings)
	}
}

func TestNewSourceRejectsInvalidStartURL(t *testing.T) {
	if _, err := NewSource("not-a-url", http.DefaultClient, nil); err == nil {
		t.Fatal("expected invalid URL error")
	}
}
