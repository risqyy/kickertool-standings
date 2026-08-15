package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"kickertool-ranking/internal/adapters/tournamentapi"
)

type SourceKind string

const (
	SourceAPI  SourceKind = "api"
	SourceHTML SourceKind = "html"
)

type Config struct {
	Source         SourceKind
	APIBaseURL     string
	APIToken       string
	HTMLURL        string
	DBPath         string
	Interval       time.Duration
	HTTPTimeout    time.Duration
	PageLimit      int
	MaxRetries     int
	RetryBackoff   time.Duration
	LogLevel       string
	AdminUIEnabled bool
	AdminBindAddr  string
	AdminUsername  string
	AdminPassword  string
}

func Load(getenv func(string) string) (Config, error) {
	if getenv == nil {
		return Config{}, errors.New("environment reader is required")
	}
	adminUIValue := strings.TrimSpace(getenv("ADMIN_UI_ENABLED"))
	result := Config{
		APIBaseURL:     valueOr(getenv("TOURNAMENT_API_BASE_URL"), tournamentapi.DefaultBaseURL),
		DBPath:         valueOr(getenv("DB_PATH"), "./data/tournaments.db"),
		Interval:       durationOr(getenv("CRAWL_INTERVAL"), 15*time.Minute),
		HTTPTimeout:    durationOr(getenv("HTTP_TIMEOUT"), 30*time.Second),
		PageLimit:      intOr(getenv("PAGE_LIMIT"), 25),
		MaxRetries:     intOr(getenv("MAX_RETRIES"), 3),
		RetryBackoff:   durationOr(getenv("RETRY_BACKOFF"), time.Second),
		LogLevel:       valueOr(getenv("LOG_LEVEL"), "info"),
		AdminUIEnabled: strings.EqualFold(adminUIValue, "true"),
		AdminBindAddr:  valueOr(getenv("ADMIN_BIND_ADDR"), "127.0.0.1:8081"),
		AdminUsername:  strings.TrimSpace(getenv("ADMIN_USERNAME")),
		AdminPassword:  getenv("ADMIN_PASSWORD"),
		APIToken:       strings.TrimSpace(getenv("TOURNAMENT_API_TOKEN")),
		HTMLURL:        strings.TrimSpace(getenv("TOURNAMENT_HTML_URL")),
	}
	switch getenv("TOURNAMENT_SOURCE") {
	case string(SourceAPI):
		result.Source = SourceAPI
		if result.APIToken == "" {
			return Config{}, errors.New("TOURNAMENT_API_TOKEN is required when TOURNAMENT_SOURCE=api")
		}
	case string(SourceHTML):
		result.Source = SourceHTML
		if result.HTMLURL == "" {
			return Config{}, errors.New("TOURNAMENT_HTML_URL is required when TOURNAMENT_SOURCE=html")
		}
		parsed, err := url.Parse(result.HTMLURL)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return Config{}, errors.New("TOURNAMENT_HTML_URL must be a valid http or https URL")
		}
	default:
		return Config{}, fmt.Errorf("TOURNAMENT_SOURCE must be exactly api or html, got %q", getenv("TOURNAMENT_SOURCE"))
	}
	if result.AdminUIEnabled {
		if result.AdminUsername == "" || strings.TrimSpace(result.AdminPassword) == "" {
			return Config{}, errors.New("ADMIN_UI_ENABLED requires non-empty ADMIN_USERNAME and ADMIN_PASSWORD")
		}
		if _, _, err := net.SplitHostPort(result.AdminBindAddr); err != nil {
			return Config{}, errors.New("ADMIN_BIND_ADDR must be host:port")
		}
	}
	return result, nil
}

func valueOr(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}

func durationOr(value string, fallback time.Duration) time.Duration {
	parsed, err := time.ParseDuration(strings.TrimSpace(value))
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func intOr(value string, fallback int) int {
	var parsed int
	if _, err := fmt.Sscan(strings.TrimSpace(value), &parsed); err != nil || parsed < 0 {
		return fallback
	}
	return parsed
}
