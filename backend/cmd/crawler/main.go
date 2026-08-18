package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata"

	"kickertool-ranking/internal/adapters"
	"kickertool-ranking/internal/adapters/gormrepo"
	"kickertool-ranking/internal/adapters/httpapi"
	"kickertool-ranking/internal/adapters/httpclient"
	"kickertool-ranking/internal/adapters/kickertoolhtml"
	"kickertool-ranking/internal/adapters/tournamentapi"
	"kickertool-ranking/internal/app"
	"kickertool-ranking/internal/config"
	"kickertool-ranking/internal/ports"

	"github.com/joho/godotenv"
	"github.com/rs/zerolog"
)

func main() {
	// Load repository-root or backend-local development configuration without
	// overriding real process environment variables.
	_ = godotenv.Load("../.env")
	_ = godotenv.Load()
	console := zerolog.ConsoleWriter{
		Out:        os.Stdout,
		TimeFormat: "2006-01-02 15:04:05",
		NoColor:    false,
	}
	logger := zerolog.New(console).With().Timestamp().Str("service", "kickertool-crawler").Logger()
	configureLogLevel(os.Getenv("LOG_LEVEL"))
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		logger.Error().Err(err).Msg("invalid crawler configuration; refusing to start")
		os.Exit(1)
	}

	if dir := filepath.Dir(cfg.DBPath); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			logger.Fatal().Err(err).Str("db_path", cfg.DBPath).Msg("create database directory")
		}
	}
	clock := adapters.SystemClock{}
	repo, db, err := gormrepo.OpenSQLite(cfg.DBPath, clock)
	if err != nil {
		logger.Fatal().Err(err).Msg("initialize repository")
	}
	sqlDB, err := db.DB()
	if err != nil {
		logger.Fatal().Err(err).Msg("access sqlite connection")
	}
	defer sqlDB.Close()

	client := httpclient.New(&http.Client{}, cfg.HTTPTimeout, cfg.MaxRetries, cfg.RetryBackoff, "kickertool-ranking/1.0.2", &logger)
	var source ports.TournamentSource
	var standingSource ports.TournamentStandingSource
	var sourceLabel string
	var sourceURL string
	switch cfg.Source {
	case config.SourceAPI:
		apiSource, sourceErr := tournamentapi.NewSource(cfg.APIBaseURL, client, cfg.APIToken, cfg.PageLimit, &logger)
		if sourceErr != nil {
			logger.Error().Err(sourceErr).Msg("initialize Tournament.app API source")
			os.Exit(1)
		}
		if smokeErr := apiSource.SmokeHello(context.Background()); smokeErr != nil {
			logger.Error().Err(smokeErr).Msg("Tournament.app API smoke request failed")
			os.Exit(1)
		}
		source, standingSource, sourceLabel, sourceURL = apiSource, apiSource, string(config.SourceAPI), cfg.APIBaseURL
	case config.SourceHTML:
		htmlSource, sourceErr := kickertoolhtml.NewSource(cfg.HTMLURL, client, &logger)
		if sourceErr != nil {
			logger.Error().Err(sourceErr).Msg("initialize Kickertool HTML source")
			os.Exit(1)
		}
		source, standingSource, sourceLabel, sourceURL = htmlSource, htmlSource, string(config.SourceHTML), cfg.HTMLURL
	default:
		logger.Error().Str("source", string(cfg.Source)).Msg("unsupported crawler source")
		os.Exit(1)
	}
	crawler := app.NewCrawler(source, repo, clock, &logger, app.WithStandings(standingSource, repo))
	scheduler := app.NewScheduler(crawler, clock, cfg.Interval, &logger)
	publicRankingAPI := httpapi.NewPublicRankingAPIHandler(repo)
	legacyRankingAPI := httpapi.NewRankingHandler(repo, &logger)
	mux := http.NewServeMux()
	mux.Handle("/api/v1/public/rankings", httpapi.StripV1Prefix(publicRankingAPI))
	mux.Handle("/api/public/rankings", publicRankingAPI)
	mux.Handle("/api/standings", legacyRankingAPI)
	mux.Handle("/healthz", httpapi.HealthHandler())
	httpServer := &http.Server{Addr: ":8080", Handler: mux}
	if cfg.AdminUIEnabled {
		adminAPI := httpapi.NewAdminAPIHandler(repo, repo, repo, &logger)
		protectedAPI := httpapi.AdminBasicAuth(httpapi.StripV1Prefix(adminAPI), cfg.AdminUsername, cfg.AdminPassword, &logger)
		mux.Handle("/api/v1/admin/", protectedAPI)
	} else {
		mux.Handle("/api/v1/admin/", http.NotFoundHandler())
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	logger.Info().Str("source", sourceLabel).Str("source_url", sourceURL).Str("db_path", cfg.DBPath).Dur("crawl_interval", cfg.Interval).Msg("crawler started")
	serverErr := make(chan error, 1)
	go func() {
		logger.Info().Str("address", httpServer.Addr).Str("endpoint", "/api/v1/public/rankings").Msg("backend HTTP endpoint started")
		if listenErr := httpServer.ListenAndServe(); listenErr != nil && listenErr != http.ErrServerClosed {
			serverErr <- listenErr
			cancel()
		}
	}()
	schedulerErr := scheduler.Run(ctx)
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = httpServer.Shutdown(shutdownCtx)
	var listenErr error
	select {
	case listenErr = <-serverErr:
	default:
	}
	if listenErr != nil {
		logger.Error().Err(listenErr).Msg("ranking HTTP endpoint stopped with error")
		os.Exit(1)
	}
	if schedulerErr != nil && ctx.Err() == nil {
		logger.Error().Err(schedulerErr).Msg("crawler stopped with error")
		os.Exit(1)
	}
	logger.Info().Msg("crawler stopped")
}

func configureLogLevel(value string) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	case "warn", "warning":
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
	case "error":
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	default:
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}
}
