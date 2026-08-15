package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"kickertool-ranking/internal/adapters"
	"kickertool-ranking/internal/adapters/gormrepo"
	"kickertool-ranking/internal/adapters/httpclient"
	"kickertool-ranking/internal/adapters/tournamentapi"
	"kickertool-ranking/internal/app"
)

func TestHTTPSQLiteCrawlerIntegration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/tournaments":
			_, _ = w.Write([]byte(`[{"id":"t1","name":"Tournament One","state":"finished","date":"2026-08-13T19:00:00+02:00"}]`))
		case "/tournaments/t1":
			_, _ = w.Write([]byte(`{"id":"t1","name":"Tournament One","state":"finished","disciplines":[{"id":"d1","name":"Monster DYP","entryType":"monster_dyp","stages":[{"id":"s1","state":"finished","groups":[{"id":"g1","name":"Final","state":"finished","tournamentMode":"monster_dyp"}]}]}]}`))
		case "/tournaments/t1/group/g1/entries":
			_, _ = w.Write([]byte(`[{"id":"p1","name":"Player One"}]`))
		case "/tournaments/t1/groups/g1/standings":
			_, _ = w.Write([]byte(`[{"id":"r1","entryId":"p1","entryName":"Player One","playerId":"p1","playerName":"Player One","rank":1,"points":15,"matches":3,"goalDifference":2}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	dbPath := filepath.Join(t.TempDir(), "nested", "tournaments.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatal(err)
	}
	clock := adapters.SystemClock{}
	repo, db, err := gormrepo.OpenSQLite(dbPath, clock)
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	client := httpclient.New(server.Client(), time.Second, 0, time.Millisecond, "integration-test", nil)
	source, err := tournamentapi.NewSource(server.URL, client, "example-api-token", 25, nil)
	if err != nil {
		t.Fatal(err)
	}
	logger := zerolog.Nop()
	crawler := app.NewCrawler(source, repo, clock, &logger, app.WithStandings(source, repo))
	result, err := crawler.Crawl(context.Background())
	if err != nil || result.Inserted != 1 || result.StandingsInserted != 1 || result.PlayersInserted != 1 {
		t.Fatalf("first crawl=%+v err=%v", result, err)
	}
	result, err = crawler.Crawl(context.Background())
	if err != nil || result.Unchanged != 1 || result.StandingsUnchanged != 1 || result.StandingsFound != 1 {
		t.Fatalf("second identical crawl=%+v err=%v", result, err)
	}
	result, err = crawler.Crawl(context.Background())
	if err != nil || result.TournamentsSkipped != 1 || result.StandingsInserted != 0 {
		t.Fatalf("third crawl should skip finalized tournament=%+v err=%v", result, err)
	}
}
