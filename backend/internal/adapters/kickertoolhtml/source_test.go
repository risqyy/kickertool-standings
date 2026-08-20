package kickertoolhtml

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"
	"kickertool-ranking/internal/domain"
)

func TestHTMLDateComponentUsesBerlinCalendarDay(t *testing.T) {
	root, err := html.Parse(strings.NewReader(`<a href="/community/tournaments/t1"><span class="name">Cup</span><time datetime="2025-05-03T22:17:32Z">03.05.2025</time></a>`))
	if err != nil {
		t.Fatal(err)
	}
	date, start := parseHTMLDateComponent(root)
	if date == nil || date.Format("02.01.2006") != "04.05.2025" {
		t.Fatalf("date=%v, want 04.05.2025", date)
	}
	if start == nil || start.Location().String() != "Europe/Berlin" {
		t.Fatalf("start=%v, want Europe/Berlin", start)
	}
	if got := start.Format("02.01.2006 15:04:05"); got != "04.05.2025 00:17:32" {
		t.Fatalf("start=%s, want 04.05.2025 00:17:32", got)
	}
	if got := date.In(mustBerlinLocation()).Format("2006-01-02"); got != "2025-05-04" {
		t.Fatalf("Berlin date=%s", got)
	}
}

func TestTournamentAnchorUsesDedicatedNameAndDateComponents(t *testing.T) {
	root, err := html.Parse(strings.NewReader(`<a href="/community/tournaments/t1"><span class="name">Cup</span><span class="date"><time datetime="2025-05-03T22:17:32Z">03.05.2025</time></span><span class="nameType">Monster DYP</span></a>`))
	if err != nil {
		t.Fatal(err)
	}
	var anchor *html.Node
	walk(root, func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "a" {
			anchor = node
		}
	})
	tournament := tournamentFromAnchor(anchor, "https://example.test/community/tournaments/t1")
	if tournament.Name != "Cup" || tournament.EntryType != "monster_dyp" || tournament.Date == nil || tournament.Date.Format("02.01.2006") != "04.05.2025" {
		t.Fatalf("tournament=%+v", tournament)
	}
}

func TestTournamentAnchorReadsDateAndCategoryFromCardContainer(t *testing.T) {
	root, err := html.Parse(strings.NewReader(`<li class="tournament-card"><a href="/community/tournaments/t2"><span class="name">Card Cup</span></a><time datetime="2025-05-03T22:17:32Z">03.05.2025</time><span class="nameType">Monster DYP</span></li>`))
	if err != nil {
		t.Fatal(err)
	}
	var anchor *html.Node
	walk(root, func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "a" {
			anchor = node
		}
	})
	tournament := tournamentFromAnchor(anchor, "https://example.test/community/tournaments/t2")
	if tournament.Date == nil || tournament.Date.Format("02.01.2006") != "04.05.2025" || tournament.EntryType != "monster_dyp" {
		t.Fatalf("tournament=%+v", tournament)
	}
}

func TestTournamentTitleDateIsNotUsedWithoutDateComponent(t *testing.T) {
	root, err := html.Parse(strings.NewReader(`<a data-name-type="Monster DYP" href="/community/tournaments/t3"><span class="name">Cup 09.10.2025</span><span class="status">finished</span></a>`))
	if err != nil {
		t.Fatal(err)
	}
	var anchor *html.Node
	walk(root, func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "a" {
			anchor = node
		}
	})
	tournament := tournamentFromAnchor(anchor, "https://example.test/community/tournaments/t3")
	if tournament.Date != nil {
		t.Fatalf("title date must not become structured date: %v", tournament.Date)
	}
}

func TestHTMLTimestampWithoutZoneStaysBerlinLocalTime(t *testing.T) {
	date, hasClock := parseHTMLDateValue("2025-05-03T22:17:32")
	start, ok := parseHTMLTimeInstant("2025-05-03T22:17:32")
	if date == nil || !hasClock || !ok || start == nil {
		t.Fatalf("date=%v hasClock=%v start=%v", date, hasClock, start)
	}
	if got := date.Format("02.01.2006"); got != "03.05.2025" {
		t.Fatalf("date=%s, want local 03.05.2025", got)
	}
	if got := start.Format("02.01.2006 15:04:05"); got != "03.05.2025 22:17:32" {
		t.Fatalf("start=%s, want local 03.05.2025 22:17:32", got)
	}
}

func mustBerlinLocation() *time.Location {
	location, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		panic(err)
	}
	return location
}

func TestSourceDiscoversRelativePaginationAndStandingsWithoutAuthorization(t *testing.T) {
	var authorization []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = append(authorization, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.URL.Query().Get("page") == "2" {
			_, _ = w.Write([]byte(`<a data-name-type="Monster DYP" href="/community/tournaments/t2">Example Cup Two finished</a>`))
			return
		}
		switch r.URL.Path {
		case "/community":
			_, _ = w.Write([]byte(`<a data-name-type="Monster DYP" href="/community/tournaments/t1">Example Cup One finished</a><a href="https://foreign.test/tournaments/no">foreign</a><script>const nextUrl = "/community?page=2";</script>`))
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

func TestSourceFetchesAllPaginationBranchesAndNewResultsOnNextCrawl(t *testing.T) {
	var crawl int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		switch r.URL.Path {
		case "/community":
			if r.URL.Query().Get("page") == "2" {
				_, _ = w.Write([]byte(`<a data-name-type="Monster DYP" href="/community/tournaments/t2">Cup Two finished</a>`))
				return
			}
			if r.URL.Query().Get("page") == "3" {
				_, _ = w.Write([]byte(`<a data-name-type="Monster DYP" href="/community/tournaments/t3">Cup Three finished</a>`))
				return
			}
			crawl++
			if crawl == 1 {
				_, _ = w.Write([]byte(`<a data-name-type="Monster DYP" href="/community/tournaments/t1">Cup One finished</a><a data-name-type="Whist" href="/community/tournaments/whist">Whist Cup finished</a><a class="next" href="/community?page=2">2</a><a class="next" href="/community?page=3">3</a>`))
			} else {
				_, _ = w.Write([]byte(`<a data-name-type="Monster DYP" href="/community/tournaments/t1">Cup One finished</a><a data-name-type="Monster DYP" href="/community/tournaments/t4">Cup Four finished</a><a data-name-type="Whist" href="/community/tournaments/whist">Whist Cup finished</a>`))
			}
		case "/community/tournaments/t1":
			_, _ = w.Write([]byte(`<h1>Cup One</h1>`))
		case "/community/tournaments/t2":
			_, _ = w.Write([]byte(`<h1>Cup Two</h1>`))
		case "/community/tournaments/t3":
			_, _ = w.Write([]byte(`<h1>Cup Three</h1>`))
		case "/community/tournaments/t4":
			_, _ = w.Write([]byte(`<h1>Cup Four</h1>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	source, err := NewSource(server.URL+"/community", server.Client(), nil)
	if err != nil {
		t.Fatal(err)
	}
	first, err := source.FetchTournaments(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 3 {
		t.Fatalf("first crawl=%d, want all pagination branches: %+v", len(first), first)
	}
	for _, tournament := range first {
		if tournament.SourceID == "whist" {
			t.Fatalf("pure Whist was queued for generic standings sync: %+v", first)
		}
	}
	second, err := source.FetchTournaments(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 2 || second[1].SourceID != "t4" {
		t.Fatalf("second crawl=%+v, want newly published t4", second)
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

func TestSourceSeparatesMonsterDYPCategoryFromWhistMode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		switch r.URL.Path {
		case "/community/tournaments/monster":
			_, _ = w.Write([]byte(`<div data-name-type="Monster DYP" data-modes="monster_dyp,whist"></div><table><tr><th>Rank</th><th>Player</th></tr><tr data-standing-id="r1" data-player-id="p1"><td>1</td><td>Player One</td></tr></table>`))
		case "/community/tournaments/whist":
			_, _ = w.Write([]byte(`<div data-name-type="Whist" data-modes="whist"></div><table><tr><th>Rank</th><th>Player</th></tr><tr data-standing-id="r2" data-player-id="p2"><td>1</td><td>Whist Player</td></tr></table>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	source, err := NewSource(server.URL+"/community", server.Client(), nil)
	if err != nil {
		t.Fatal(err)
	}
	monster, err := source.FetchStandings(context.Background(), domain.Tournament{Source: SourceName, SourceID: "monster", Status: "finished", URL: server.URL + "/community/tournaments/monster"})
	if err != nil {
		t.Fatal(err)
	}
	if !monster.Complete || len(monster.Standings) != 1 {
		t.Fatalf("Monster-DYP+Whist should be eligible: %+v", monster)
	}
	whist, err := source.FetchStandings(context.Background(), domain.Tournament{Source: SourceName, SourceID: "whist", Status: "finished", URL: server.URL + "/community/tournaments/whist"})
	if err != nil {
		t.Fatal(err)
	}
	if whist.Complete || len(whist.Standings) != 0 {
		t.Fatalf("pure Whist should not be eligible: %+v", whist)
	}
}

func TestSourceKeepsIndependentOctoberSectionsAndDeduplicatesByTournamentID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.URL.Path != "/community" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`<section class="october-monster"><a data-name-type="Monster DYP" data-modes="monster_dyp,whist" href="/community/tournaments/october-monster"><span class="name">Monster + Whist</span><time datetime="2025-10-09">09.10.2025</time></a></section><section class="october-whist"><a data-name-type="Whist" data-modes="whist" href="/community/tournaments/october-whist"><span class="name">Whist only</span><time datetime="2025-10-09">09.10.2025</time></a></section><section class="duplicate"><a data-name-type="Monster DYP" href="/community/tournaments/october-monster"><span class="name">Monster + Whist</span><time datetime="2025-10-09">09.10.2025</time></a></section>`))
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
	if len(tournaments) != 1 || tournaments[0].SourceID != "october-monster" || tournaments[0].EntryType != "monster_dyp" {
		t.Fatalf("independent sections=%+v, want one eligible Monster-DYP tournament", tournaments)
	}
	if tournaments[0].Date == nil || tournaments[0].Date.Format("02.01.2006") != "09.10.2025" {
		t.Fatalf("date=%v, want 09.10.2025", tournaments[0].Date)
	}
}
