package kickertoolhtml

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
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

func TestCurrentTournamentCardParsesEnglishDateAndModeClass(t *testing.T) {
	root, err := html.Parse(strings.NewReader(`<a class="tournament-card" href="/fvhkickern/tournaments/current"><span class="name">Summer Cup</span><span class="date">August 17, 2026</span><span class="mode monster_dyp">Monster DYP</span></a>`))
	if err != nil {
		t.Fatal(err)
	}
	var anchor *html.Node
	walk(root, func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "a" {
			anchor = node
		}
	})
	tournament := tournamentFromAnchor(anchor, "https://example.test/fvhkickern/tournaments/current")
	if tournament.Name != "Summer Cup" || tournament.EntryType != "monster_dyp" {
		t.Fatalf("tournament=%+v", tournament)
	}
	if tournament.Date == nil || tournament.Date.Format("2006-01-02") != "2026-08-17" {
		t.Fatalf("date=%v, want 2026-08-17", tournament.Date)
	}
}

func TestCompetitionEvidenceParsesUnquotedSvelteData(t *testing.T) {
	root, err := html.Parse(strings.NewReader(`<script>const tournament = {_id:"t1",date:new Date(1786956090000),name:"Summer Cup",modes:["monster_dyp"],nameType:"monster_dyp"};</script>`))
	if err != nil {
		t.Fatal(err)
	}
	entryType, evidence, modes := competitionEvidence(root, []byte(`const tournament = {_id:"t1",date:new Date(1786956090000),name:"Summer Cup",modes:["monster_dyp"],nameType:"monster_dyp"};`))
	if !evidence || entryType != "monster_dyp" || len(modes) != 1 || modes[0] != "monster_dyp" {
		t.Fatalf("entryType=%q evidence=%v modes=%v", entryType, evidence, modes)
	}
}

func TestListingSvelteNameTypeOverridesModeForMixedCompetition(t *testing.T) {
	body := []byte(`<a class="tournament-card" href="/fvhkickern/tournaments/mixed"><span class="name">Monster + Whist</span><span class="mode whist">Whist</span></a><script>const item = {_id:"mixed",modes:["whist"],nameType:"monster_dyp"};</script>`)
	tournaments, _, err := parseListing("https://example.test/fvhkickern", body, func(candidate *url.URL) bool {
		return candidate.Host == "example.test" && strings.HasPrefix(candidate.Path, "/fvhkickern")
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(tournaments) != 1 || tournaments[0].EntryType != "monster_dyp" {
		t.Fatalf("tournaments=%+v, want Monster-DYP category from Svelte data", tournaments)
	}
}

func TestCanonicalStandingRowsRejectsConflictingPlayerIDs(t *testing.T) {
	rows := []domain.TournamentStanding{
		{StandingID: "r1", StandingKey: "r1", PlayerID: "p1", PlayerName: "Same Name"},
		{StandingID: "r2", StandingKey: "r2", PlayerID: "p2", PlayerName: "same name"},
	}
	if _, err := canonicalStandingRows(rows); err == nil {
		t.Fatal("expected conflicting player IDs with one PlayerKey to be rejected")
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
			_, _ = w.Write([]byte(`<h1>Example Cup One</h1><div class="discipline-type">Monster DYP</div><a href="/community/tournaments/t1/standings">Standings</a>`))
		case "/community/tournaments/t1/standings":
			_, _ = w.Write([]byte(`<h1>Example Cup One standings</h1><table><tr><th>#</th><th>Player</th><th>Points</th><th>Matches</th><th>Tor-Diff</th></tr><tr data-standing-id="r1" data-player-id="p1"><td>1</td><td>Player One</td><td>1,50</td><td>2</td><td>3</td></tr></table>`))
		case "/community/tournaments/t2":
			_, _ = w.Write([]byte(`<h1>Example Cup Two</h1><a href="/community/tournaments/t2/standings">Standings</a>`))
		case "/community/tournaments/t2/standings":
			_, _ = w.Write([]byte(`<h1>Example Cup Two standings</h1><table><tr><th>#</th><th>Player</th></tr><tr data-standing-id="r2" data-player-id="p2"><td>1</td><td>Player Two</td></tr></table>`))
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
			_, _ = w.Write([]byte(`<h1>Cup One</h1><a href="/community/tournaments/t1/standings">Standings</a>`))
		case "/community/tournaments/t1/standings":
			_, _ = w.Write([]byte(`<h1>Cup One standings</h1><table><tr><th>#</th><th>Player</th></tr><tr data-standing-id="r1" data-player-id="p1"><td>1</td><td>Player One</td></tr></table>`))
		case "/community/tournaments/t2":
			_, _ = w.Write([]byte(`<h1>Cup Two</h1><a href="/community/tournaments/t2/standings">Standings</a>`))
		case "/community/tournaments/t2/standings":
			_, _ = w.Write([]byte(`<h1>Cup Two standings</h1><table><tr><th>#</th><th>Player</th></tr><tr data-standing-id="r2" data-player-id="p2"><td>1</td><td>Player Two</td></tr></table>`))
		case "/community/tournaments/t3":
			_, _ = w.Write([]byte(`<h1>Cup Three</h1><a href="/community/tournaments/t3/standings">Standings</a>`))
		case "/community/tournaments/t3/standings":
			_, _ = w.Write([]byte(`<h1>Cup Three standings</h1><table><tr><th>#</th><th>Player</th></tr><tr data-standing-id="r3" data-player-id="p3"><td>1</td><td>Player Three</td></tr></table>`))
		case "/community/tournaments/t4":
			_, _ = w.Write([]byte(`<h1>Cup Four</h1><a href="/community/tournaments/t4/standings">Standings</a>`))
		case "/community/tournaments/t4/standings":
			_, _ = w.Write([]byte(`<h1>Cup Four standings</h1><table><tr><th>#</th><th>Player</th></tr><tr data-standing-id="r4" data-player-id="p4"><td>1</td><td>Player Four</td></tr></table>`))
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

func TestSourceAllowsExactlyOneHundredCompletedListingPages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := 1
		if raw := r.URL.Query().Get("page"); raw != "" {
			page, _ = strconv.Atoi(raw)
		}
		if page < 100 {
			_, _ = w.Write([]byte(`<a class="next" href="/community?page=` + strconv.Itoa(page+1) + `">Next</a>`))
		}
	}))
	defer server.Close()

	source, err := NewSource(server.URL+"/community", server.Client(), nil)
	if err != nil {
		t.Fatal(err)
	}
	tournaments, err := source.FetchTournaments(context.Background())
	if err != nil {
		t.Fatalf("exactly 100 completed pages should be valid: %v", err)
	}
	if len(tournaments) != 0 {
		t.Fatalf("unexpected tournaments=%+v", tournaments)
	}
}

func TestSourceDiscoversSeparateStandingsAndDynamicJSONEndpoint(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.RequestURI())
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		switch r.URL.Path {
		case "/community/tournaments/t3":
			_, _ = w.Write([]byte(`<h1>Example Cup</h1><section class="discipline"><span class="entry-type">Monster DYP</span><a href="/community/tournaments/t3/groups/g1/standings">Group standings</a><a href="/community/tournaments/t3/groups/missing/standings">Missing optional group</a><a href="/community/tournaments/foreign/groups/g1/standings">Foreign group</a><a href="/community/tournaments/foreign/groups/t3/standings">Foreign group ID collision</a></section><script>fetch("/community/xhr/t3/group/g1/standings.json");fetch("/community/xhr/foreign/group/g1/standings.json");fetch("/community/xhr/foreign/group/t3/standings.json");fetch("/community/tournaments/foreign/standings?group=t3")</script>`))
		case "/community/tournaments/t3/groups/g1/standings":
			_, _ = w.Write([]byte(`<div data-entry-type="monster_dyp"></div><table><tr><th>Rank</th><th>Player</th><th>Points</th><th>Matches</th></tr><tr data-standing-id="r1" data-entry-id="e1" data-player-id="p1"><td>1</td><td>Player One</td><td>2,00</td><td>4</td></tr></table>`))
		case "/community/xhr/t3/group/g1/standings.json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"entryType":"monster_dyp","standings":[{"id":"r2","rank":2,"entry":{"id":"e2","name":"Example Team B"},"player":{"id":"p2","name":"Player Two"},"points":1.5,"matches":3},{"id":"r3","rank":1,"entry":{"id":"e3","name":"Example Team A"},"player":{"id":"p1","name":"Player One"},"points":2.0,"matches":4}]}`))
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
	if !snapshot.Complete || len(snapshot.Standings) != 2 || len(snapshot.GroupStandings) != 3 {
		t.Fatalf("discovered snapshot=%+v", snapshot)
	}
	playerOneRows := 0
	for _, row := range snapshot.Standings {
		if row.PlayerKey == domain.PlayerKey("Player One") {
			playerOneRows++
		}
	}
	if playerOneRows != 1 {
		t.Fatalf("duplicate player-level rows were not normalized: %+v", snapshot.Standings)
	}
	if snapshot.Standings[0].PlayerID == "" || snapshot.Standings[1].PlayerID == "" {
		t.Fatalf("player IDs not allocated: %+v", snapshot.Standings)
	}
	for _, path := range requests {
		if strings.Contains(path, "/foreign/") {
			t.Fatalf("foreign tournament endpoint was fetched: %v", requests)
		}
	}
	for _, forbidden := range []string{
		"/community/tournaments/foreign/groups/t3/standings",
		"/community/xhr/foreign/group/t3/standings.json",
		"/community/tournaments/foreign/standings?group=t3",
	} {
		for _, path := range requests {
			if path == forbidden {
				t.Fatalf("candidate with colliding tournament/group ID was fetched: %v", requests)
			}
		}
	}
}

func TestSourceImportsOnlyTournamentsWithReachableStandingsPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		switch r.URL.Path {
		case "/fvhkickern":
			_, _ = w.Write([]byte(`<a class="tournament-card" href="/fvhkickern/tournaments/with"><span class="name">With Standings</span><span class="date">August 17, 2026</span><span class="mode whist">Whist</span></a><a class="tournament-card" href="/fvhkickern/tournaments/with/standings"><span class="name">With Standings</span><span class="date">August 17, 2026</span><span class="mode whist">Whist</span></a><a class="tournament-card" href="/fvhkickern/tournaments/without"><span class="name">Without Standings</span><span class="date">August 18, 2026</span><span class="mode whist">Whist</span></a><a class="tournament-card" href="/fvhkickern/tournaments/whist"><span class="name">Whist Standings</span><span class="date">August 19, 2026</span><span class="mode whist">Whist</span></a><a class="tournament-card" href="/fvhkickern/tournaments/empty"><span class="name">Empty Standings</span><span class="date">August 20, 2026</span><span class="mode monster_dyp">Monster DYP</span></a><a class="tournament-card" href="/fvhkickern/tournaments/noisy"><span class="name">Noisy Detail</span><span class="date">August 21, 2026</span><span class="mode monster_dyp">Monster DYP</span></a><a class="tournament-card" href="/fvhkickern/tournaments/redirect"><span class="name">Redirect Standings</span><span class="date">August 22, 2026</span><span class="mode monster_dyp">Monster DYP</span></a><a class="tournament-card" href="/fvhkickern/tournaments/redirect-foreign"><span class="name">Foreign Redirect</span><span class="date">August 23, 2026</span><span class="mode monster_dyp">Monster DYP</span></a><script>const listing = {_id:"with",modes:["whist"],nameType:"monster_dyp"};</script>`))
		case "/fvhkickern/tournaments/with":
			_, _ = w.Write([]byte(`<h1>With Standings</h1><nav><a href="/fvhkickern/tournaments/with/standings">Standings</a></nav>`))
		case "/fvhkickern/tournaments/with/standings":
			_, _ = w.Write([]byte(`<h1>With Standings</h1><div data-name-type="Whist"></div><table><tr><th>#</th><th>Player</th></tr><tr data-standing-id="r1" data-player-id="p1"><td>1</td><td>Player One</td></tr></table>`))
		case "/fvhkickern/tournaments/without":
			_, _ = w.Write([]byte(`<h1>Without Standings</h1><table><tr><th>#</th><th>Player</th></tr><tr><td>1</td><td>Not a standings page</td></tr></table>`))
		case "/fvhkickern/tournaments/whist":
			_, _ = w.Write([]byte(`<h1>Whist Standings</h1><nav><a href="/fvhkickern/tournaments/whist/standings">Standings</a></nav>`))
		case "/fvhkickern/tournaments/whist/standings":
			_, _ = w.Write([]byte(`<h1>Whist Standings</h1><table><tr><th>#</th><th>Player</th></tr><tr data-standing-id="r2" data-player-id="p2"><td>1</td><td>Whist Player</td></tr></table>`))
		case "/fvhkickern/tournaments/empty":
			_, _ = w.Write([]byte(`<h1>Empty Standings</h1><nav><a href="/fvhkickern/tournaments/empty/standings">Standings</a></nav>`))
		case "/fvhkickern/tournaments/empty/standings":
			_, _ = w.Write([]byte(`<h1>Empty Standings</h1>`))
		case "/fvhkickern/tournaments/noisy":
			_, _ = w.Write([]byte(`<h1>Noisy Detail</h1><a href="/fvhkickern/tournaments/noisy/groups/final">Group</a><script>fetch("/fvhkickern/tournaments/noisy/xhr/results.json")</script>`))
		case "/fvhkickern/tournaments/noisy/groups/final":
			_, _ = w.Write([]byte(`<table><tr><th>#</th><th>Player</th></tr><tr><td>1</td><td>Group result</td></tr></table>`))
		case "/fvhkickern/tournaments/noisy/xhr/results.json":
			_, _ = w.Write([]byte(`<table><tr><th>#</th><th>Player</th></tr><tr><td>1</td><td>XHR result</td></tr></table>`))
		case "/fvhkickern/tournaments/redirect":
			http.Redirect(w, r, "/fvhkickern/tournaments/redirect/standings", http.StatusFound)
		case "/fvhkickern/tournaments/redirect/standings":
			_, _ = w.Write([]byte(`<h1>Redirect Standings</h1>`))
		case "/fvhkickern/tournaments/redirect-foreign":
			http.Redirect(w, r, "/fvhkickern/tournaments/foreign/standings", http.StatusFound)
		case "/fvhkickern/tournaments/foreign/standings":
			_, _ = w.Write([]byte(`<h1>Foreign Standings</h1><table><tr><th>#</th><th>Player</th></tr><tr data-standing-id="foreign-r1" data-player-id="foreign-p1"><td>1</td><td>Foreign Player</td></tr></table>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	source, err := NewSource(server.URL+"/fvhkickern", server.Client(), nil)
	if err != nil {
		t.Fatal(err)
	}
	tournaments, err := source.FetchTournaments(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tournaments) != 4 {
		t.Fatalf("tournaments=%+v, want Monster-DYP, pure Whist, empty, and redirect standings", tournaments)
	}
	seen := make(map[string]domain.Tournament)
	for _, tournament := range tournaments {
		seen[tournament.SourceID] = tournament
	}
	if _, ok := seen["without"]; ok {
		t.Fatalf("detail page without standings was imported: %+v", tournaments)
	}
	if _, ok := seen["with"]; !ok {
		t.Fatalf("detail page with standings was not imported: %+v", tournaments)
	}
	if !strings.HasSuffix(seen["with"].URL, "/standings") {
		t.Fatalf("duplicate tournament did not prefer direct standings URL: %+v", seen["with"])
	}
	if seen["with"].EntryType != "monster_dyp" {
		t.Fatalf("listing nameType category was overwritten by mode: %+v", seen["with"])
	}
	snapshot, err := source.FetchStandings(context.Background(), seen["with"])
	if err != nil || len(snapshot.Disciplines) != 1 || snapshot.Disciplines[0].EntryType != "monster_dyp" {
		t.Fatalf("listing category was not preserved through standings snapshot: err=%v snapshot=%+v", err, snapshot)
	}
	for _, id := range []string{"empty", "redirect"} {
		if _, ok := seen[id]; !ok {
			t.Fatalf("%s standings route was not considered reachable: %+v", id, tournaments)
		}
	}
	if _, ok := seen["noisy"]; ok {
		t.Fatalf("group/results/xhr-only detail was imported: %+v", tournaments)
	}
	if _, ok := seen["redirect-foreign"]; ok {
		t.Fatalf("cross-tournament redirect was imported: %+v", tournaments)
	}
	if whist, ok := seen["whist"]; !ok || whist.EntryType != "whist" {
		t.Fatalf("pure Whist with standings was not preserved: %+v", tournaments)
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
	if !whist.Complete || len(whist.Standings) != 1 {
		t.Fatalf("pure Whist with a reachable standings page should be eligible: %+v", whist)
	}
}

func TestSourceKeepsIndependentOctoberSectionsAndDeduplicatesByTournamentID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.URL.Path != "/community" {
			if r.URL.Path == "/community/tournaments/october-monster" {
				_, _ = w.Write([]byte(`<h1>Monster + Whist</h1><a href="/community/tournaments/october-monster/standings">Standings</a>`))
				return
			}
			if r.URL.Path == "/community/tournaments/october-monster/standings" {
				_, _ = w.Write([]byte(`<h1>Monster + Whist standings</h1><table><tr><th>#</th><th>Player</th></tr><tr data-standing-id="r1" data-player-id="p1"><td>1</td><td>Player One</td></tr></table>`))
				return
			}
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
