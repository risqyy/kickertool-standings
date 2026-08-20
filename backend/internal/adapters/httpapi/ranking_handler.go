package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"kickertool-ranking/internal/domain"
	"kickertool-ranking/internal/ports"
)

type RankingHandler struct {
	reader ports.PlayerRankingReader
	logger *zerolog.Logger
}

type PublicRankingAPIHandler struct {
	reader ports.PlayerRankingReader
}

func NewPublicRankingAPIHandler(reader ports.PlayerRankingReader) *PublicRankingAPIHandler {
	return &PublicRankingAPIHandler{reader: reader}
}

func (h *PublicRankingAPIHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || r.URL.Path != "/api/public/rankings" {
		w.Header().Set("Allow", http.MethodGet)
		http.NotFound(w, r)
		return
	}
	year, err := requestedRankingYear(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	periodReader, supportsPeriods := h.reader.(ports.PeriodRankingReader)
	availableYears := []int{}
	if supportsPeriods {
		availableYears, err = periodReader.ListAvailableRankingYears(r.Context())
		if err != nil {
			http.Error(w, "could not load ranking years", http.StatusInternalServerError)
			return
		}
		if availableYears == nil {
			availableYears = []int{}
		}
		availableYears = append([]int{}, availableYears...)
		sort.Slice(availableYears, func(i, j int) bool { return availableYears[i] > availableYears[j] })
	}
	var aggregates []domain.PlayerAggregate
	if year != nil {
		if !supportsPeriods {
			http.Error(w, "year rankings are unavailable", http.StatusInternalServerError)
			return
		}
		aggregates, err = periodReader.ListPlayerRankingForYear(r.Context(), *year)
	} else {
		aggregates, err = h.reader.ListPlayerRanking(r.Context())
	}
	if err != nil {
		http.Error(w, "could not load rankings", http.StatusInternalServerError)
		return
	}
	rows := make([]publicRankingRow, 0, len(aggregates))
	for index, aggregate := range aggregates {
		rows = append(rows, publicRankingRow{Rank: index + 1, Trend: publicTrend(aggregate.Trend), Name: aggregate.PlayerName, IncludedTournamentCount: aggregate.TournamentCount, GamesPlayed: aggregate.GamesPlayed, TotalPoints: centsString(aggregate.TotalPointsCents), PointsPerGame: centsString(aggregate.PointsPerGameCents), GoalDifference: aggregate.GoalDifference})
	}
	var lastSync *time.Time
	if source, ok := h.reader.(interface {
		LastSyncAt(context.Context) (*time.Time, error)
	}); ok {
		lastSync, _ = source.LastSyncAt(r.Context())
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{"items": rows, "lastSyncAt": lastSync, "availableYears": availableYears, "selectedYear": year})
}

func requestedRankingYear(r *http.Request) (*int, error) {
	values, ok := r.URL.Query()["year"]
	if !ok {
		return nil, nil
	}
	if len(values) != 1 || len(values[0]) != 4 {
		return nil, fmt.Errorf("year must be a valid four-digit calendar year")
	}
	for _, character := range values[0] {
		if character < '0' || character > '9' {
			return nil, fmt.Errorf("year must be a valid four-digit calendar year")
		}
	}
	parsed, err := strconv.Atoi(values[0])
	if err != nil || parsed < 1000 || parsed > 9999 {
		return nil, fmt.Errorf("year must be a valid four-digit calendar year")
	}
	return &parsed, nil
}

type publicRankingRow struct {
	Rank                    int     `json:"rank"`
	Trend                   string  `json:"trend"`
	Name                    string  `json:"name"`
	IncludedTournamentCount int     `json:"includedTournamentCount"`
	GamesPlayed             *int    `json:"gamesPlayed"`
	TotalPoints             *string `json:"totalPoints"`
	PointsPerGame           *string `json:"pointsPerGame"`
	GoalDifference          *int    `json:"goalDifference"`
}

func publicTrend(value domain.RankingTrend) string {
	if value == "" {
		// Keep adapters implementing the pre-trend reader interface usable while
		// the concrete repository supplies all four states explicitly.
		return string(domain.RankingTrendSame)
	}
	return string(value)
}

type rankingRow struct {
	Rank            int       `json:"rank"`
	PlayerName      string    `json:"player_name"`
	TournamentCount int       `json:"tournament_count"`
	TotalPoints     *decimal2 `json:"total_points"`
	GamesPlayed     *int      `json:"games_played"`
	GoalDifference  *int      `json:"goal_difference"`
	PointsPerGame   *decimal2 `json:"points_per_game"`
	PointsAvailable bool      `json:"points_available"`
	GamesAvailable  bool      `json:"games_available"`
	GoalsAvailable  bool      `json:"goals_available"`
}

func NewRankingHandler(reader ports.PlayerRankingReader, logger *zerolog.Logger, _ ...string) *RankingHandler {
	return &RankingHandler{reader: reader, logger: logger}
}

func (h *RankingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.URL.Path != "/api/standings" {
		http.NotFound(w, r)
		return
	}
	aggregates, err := h.reader.ListPlayerRanking(r.Context())
	if err != nil {
		if h.logger != nil {
			h.logger.Error().Err(err).Str("path", r.URL.Path).Msg("load player ranking")
		}
		http.Error(w, "could not load standings", http.StatusInternalServerError)
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(toRows(aggregates)); err != nil && h.logger != nil {
		h.logger.Error().Err(err).Msg("write player ranking JSON")
	}
}

func toRows(aggregates []domain.PlayerAggregate) []rankingRow {
	rows := make([]rankingRow, 0, len(aggregates))
	for index, aggregate := range aggregates {
		rows = append(rows, rankingRow{Rank: index + 1, PlayerName: aggregate.PlayerName, TournamentCount: aggregate.TournamentCount, TotalPoints: newDecimal2(aggregate.TotalPointsCents), GamesPlayed: aggregate.GamesPlayed, GoalDifference: aggregate.GoalDifference, PointsPerGame: newDecimal2(aggregate.PointsPerGameCents), PointsAvailable: aggregate.PointsAvailable, GamesAvailable: aggregate.GamesAvailable, GoalsAvailable: aggregate.GoalsAvailable})
	}
	return rows
}

type decimal2 int64

func centsString(value *int64) *string {
	if value == nil {
		return nil
	}
	formatted := decimal2(*value).String()
	return &formatted
}

func newDecimal2(value *int64) *decimal2 {
	if value == nil {
		return nil
	}
	converted := decimal2(*value)
	return &converted
}

func (value decimal2) String() string {
	amount := int64(value)
	sign := ""
	if amount < 0 {
		sign = "-"
		amount = -amount
	}
	return fmt.Sprintf("%s%d.%02d", sign, amount/100, amount%100)
}

func (value decimal2) MarshalJSON() ([]byte, error) {
	return []byte(value.String()), nil
}

func (value *decimal2) UnmarshalJSON(data []byte) error {
	text := strings.Trim(strings.TrimSpace(string(data)), `"`)
	parsed, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return err
	}
	*value = decimal2(math.Round(parsed * 100))
	return nil
}
