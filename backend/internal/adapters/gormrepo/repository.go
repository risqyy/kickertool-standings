package gormrepo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
	"kickertool-ranking/internal/domain"
	"kickertool-ranking/internal/ports"
)

type TournamentModel struct {
	ID                                    uint    `gorm:"primaryKey"`
	Source                                string  `gorm:"not null;uniqueIndex:ux_tournament_source_id,priority:1;uniqueIndex:ux_tournament_source_key,priority:1"`
	SourceID                              *string `gorm:"uniqueIndex:ux_tournament_source_id,priority:2"`
	SourceKey                             string  `gorm:"not null;uniqueIndex:ux_tournament_source_key,priority:2"`
	Name                                  string  `gorm:"not null"`
	Date                                  *time.Time
	StartTime                             *time.Time
	EndTime                               *time.Time
	Venue                                 string
	City                                  string
	Country                               string
	Organizer                             string
	Status                                string
	EntryType                             string
	IsLive                                bool
	PreviousStatus                        string
	PreviousIsLive                        bool
	StatusTransition                      string
	StatusTransitionAt                    *time.Time
	Participants                          *int
	URL                                   string    `gorm:"not null"`
	LastSeenAt                            time.Time `gorm:"not null"`
	CreatedAt                             time.Time
	UpdatedAt                             time.Time
	StandingsSyncedAt                     *time.Time
	StandingsHash                         string
	StandingsSyncComplete                 bool
	LastStandingsSyncFailed               bool
	FinalizedAt                           *time.Time
	ConsecutiveIdenticalCompleteSnapshots int
	IncludedInRanking                     bool `gorm:"not null;default:true"`
	InclusionUpdatedAt                    *time.Time
	InclusionVersion                      int64 `gorm:"not null;default:1"`
	InclusionReason                       string
}

type PlayerModel struct {
	ID                       uint   `gorm:"primaryKey"`
	CanonicalNameKey         string `gorm:"not null;uniqueIndex:ux_player_canonical_name_key"`
	DisplayName              string `gorm:"not null"`
	MergedIntoPlayerID       *uint  `gorm:"index"`
	MergedAt                 *time.Time
	LastSeenAt               time.Time `gorm:"not null"`
	CreatedAt                time.Time
	UpdatedAt                time.Time
	RankingCorrectionVersion int64 `gorm:"not null;default:0"`
}

type PlayerNameAliasModel struct {
	ID          uint        `gorm:"primaryKey"`
	NameKey     string      `gorm:"not null;uniqueIndex:ux_player_alias_name_key"`
	DisplayName string      `gorm:"not null"`
	PlayerID    uint        `gorm:"not null;index"`
	Player      PlayerModel `gorm:"foreignKey:PlayerID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// SourcePlayerIdentity preserves source IDs as provenance only. NameKey and
// the Player row remain the canonical identity.
type SourcePlayerIdentityModel struct {
	ID         uint        `gorm:"primaryKey"`
	Source     string      `gorm:"not null;uniqueIndex:ux_source_player_identity,priority:1"`
	ExternalID string      `gorm:"not null;uniqueIndex:ux_source_player_identity,priority:2"`
	NameKey    string      `gorm:"not null;uniqueIndex:ux_source_player_identity,priority:3"`
	PlayerRef  uint        `gorm:"not null;index"`
	Player     PlayerModel `gorm:"foreignKey:PlayerRef;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type DisciplineModel struct {
	ID           uint   `gorm:"primaryKey"`
	Source       string `gorm:"not null;uniqueIndex:ux_discipline_source_id,priority:1"`
	SourceID     string `gorm:"not null;uniqueIndex:ux_discipline_source_id,priority:2"`
	TournamentID string `gorm:"not null;index"`
	Name         string
	ShortName    string
	EntryType    string
}
type StageModel struct {
	ID           uint   `gorm:"primaryKey"`
	Source       string `gorm:"not null;uniqueIndex:ux_stage_source_id,priority:1"`
	SourceID     string `gorm:"not null;uniqueIndex:ux_stage_source_id,priority:2"`
	DisciplineID string `gorm:"not null;index"`
	TournamentID string `gorm:"not null;index"`
	State        string
}
type GroupModel struct {
	ID           uint   `gorm:"primaryKey"`
	Source       string `gorm:"not null;uniqueIndex:ux_group_source_id,priority:1"`
	SourceID     string `gorm:"not null;uniqueIndex:ux_group_source_id,priority:2"`
	StageID      string `gorm:"not null;index"`
	DisciplineID string `gorm:"not null;index"`
	TournamentID string `gorm:"not null;index"`
	Name         string
	State        string
}
type EntryModel struct {
	ID           uint   `gorm:"primaryKey"`
	Source       string `gorm:"not null;uniqueIndex:ux_entry_source_id,priority:1"`
	SourceID     string `gorm:"not null;uniqueIndex:ux_entry_source_id,priority:2"`
	TournamentID string `gorm:"not null;index"`
	Name         string
	EntryType    string
}
type EntryMembershipModel struct {
	ID           uint   `gorm:"primaryKey"`
	Source       string `gorm:"not null;uniqueIndex:ux_membership_source_entry_player,priority:1"`
	EntryID      string `gorm:"not null;uniqueIndex:ux_membership_source_entry_player,priority:2"`
	PlayerID     string `gorm:"not null;uniqueIndex:ux_membership_source_entry_player,priority:3"`
	TournamentID string `gorm:"not null;index"`
	PlayerName   string
}
type GroupStandingModel struct {
	ID                           uint   `gorm:"primaryKey"`
	Source                       string `gorm:"not null;uniqueIndex:ux_groupstanding_source_id,priority:1"`
	SourceID                     string `gorm:"not null;uniqueIndex:ux_groupstanding_source_id,priority:2"`
	TournamentID                 string `gorm:"not null;index"`
	GroupID                      string `gorm:"not null;index"`
	EntryID                      string `gorm:"not null;index"`
	EntryName                    string
	Rank                         *int
	Result                       *int
	Preliminary                  *int
	FinalResult                  *int
	PointsCents                  *int64
	PointsPerMatchCents          *int64
	CorrectedPointsPerMatchCents *int64
	HasCorrectedValue            *bool
	GamesPlayed                  *int
	GoalDifference               *int
	URL                          string
}
type AllocationModel struct {
	ID          uint   `gorm:"primaryKey"`
	Source      string `gorm:"not null;uniqueIndex:ux_allocation_source_standing_player,priority:1"`
	StandingID  string `gorm:"not null;uniqueIndex:ux_allocation_source_standing_player,priority:2"`
	PlayerRef   uint   `gorm:"not null;uniqueIndex:ux_allocation_source_standing_player,priority:3;index"`
	PlayerID    string `gorm:"not null;index"`
	EntryID     string `gorm:"not null;index"`
	PointsCents int64
}

type StandingModel struct {
	ID                           uint    `gorm:"primaryKey"`
	Source                       string  `gorm:"not null;uniqueIndex:ux_standing_source_tournament_player,priority:1;uniqueIndex:ux_standing_source_key,priority:1;uniqueIndex:ux_standing_source_external,priority:1"`
	TournamentID                 string  `gorm:"not null;uniqueIndex:ux_standing_source_tournament_player,priority:2"`
	TournamentRef                uint    `gorm:"not null;index"`
	SourceStandingID             *string `gorm:"uniqueIndex:ux_standing_source_external,priority:2"`
	StandingKey                  string  `gorm:"not null;uniqueIndex:ux_standing_source_key,priority:2"`
	DisciplineID                 string  `gorm:"index"`
	StageID                      string  `gorm:"index"`
	Group                        string
	PlayerID                     string
	PlayerKey                    string `gorm:"not null;uniqueIndex:ux_standing_source_tournament_player,priority:3"`
	PlayerRef                    uint   `gorm:"not null;index"`
	PlayerName                   string `gorm:"not null"`
	EntryID                      string `gorm:"not null;index"`
	EntryName                    string
	Team                         string
	Partner                      string
	Rank                         *int
	Result                       *int
	Preliminary                  *int
	FinalResult                  *int
	PointsCents                  *int64
	PointsPerMatchCents          *int64
	CorrectedPointsPerMatchCents *int64
	HasCorrectedValue            *bool
	GamesPlayed                  *int
	GoalDifference               *int
	Status                       string
	StatsJSON                    string    `gorm:"not null"`
	URL                          string    `gorm:"not null"`
	LastSeenAt                   time.Time `gorm:"not null"`
	CreatedAt                    time.Time
	UpdatedAt                    time.Time
	Tournament                   TournamentModel `gorm:"foreignKey:TournamentRef;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT"`
	Player                       PlayerModel     `gorm:"foreignKey:PlayerRef;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT"`
}

type PlayerAggregateModel struct {
	ID                 uint   `gorm:"primaryKey"`
	Source             string `gorm:"not null;uniqueIndex:ux_aggregate_source_player,priority:1"`
	PlayerKey          string `gorm:"not null;uniqueIndex:ux_aggregate_source_player,priority:2"`
	PlayerRef          uint   `gorm:"not null;index"`
	PlayerName         string `gorm:"not null"`
	TournamentCount    int    `gorm:"not null"`
	TotalPointsCents   *int64
	GamesPlayed        *int
	GoalDifference     *int
	PointsPerGameCents *int64
	PointsAvailable    bool        `gorm:"not null"`
	GamesAvailable     bool        `gorm:"not null"`
	GoalsAvailable     bool        `gorm:"not null"`
	RecalculatedAt     time.Time   `gorm:"not null"`
	Player             PlayerModel `gorm:"foreignKey:PlayerRef;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT"`
}

type PlayerMergeAuditModel struct {
	ID                          uint      `gorm:"primaryKey"`
	SourcePlayerID              uint      `gorm:"not null;index"`
	TargetPlayerID              uint      `gorm:"not null;index"`
	SourceDisplayName           string    `gorm:"not null"`
	TargetDisplayName           string    `gorm:"not null"`
	MergedAt                    time.Time `gorm:"not null;index"`
	TransferredAliases          int       `gorm:"not null"`
	TransferredSourceIdentities int       `gorm:"not null"`
	TransferredAllocations      int       `gorm:"not null"`
	DeduplicatedAllocations     int       `gorm:"not null"`
	Actor                       string
	Reason                      string
	CreatedAt                   time.Time
}

type TournamentInclusionAuditModel struct {
	ID               uint  `gorm:"primaryKey"`
	TournamentID     uint  `gorm:"not null;index"`
	Included         bool  `gorm:"not null"`
	PreviousIncluded bool  `gorm:"not null"`
	ExpectedVersion  int64 `gorm:"not null"`
	NewVersion       int64 `gorm:"not null"`
	Reason           string
	ChangedAt        time.Time `gorm:"not null;index"`
}

// ManualRankingCorrectionModel is a source-independent additive adjustment.
// Rows are retained forever; revocation only changes the current status and
// appends a signed revision row below.
type ManualRankingCorrectionModel struct {
	ID                     uint      `gorm:"primaryKey"`
	PlayerRef              uint      `gorm:"not null;index"`
	PlayerKey              string    `gorm:"not null;index"`
	EffectiveDate          time.Time `gorm:"not null;index"`
	EffectiveYear          int       `gorm:"not null;index"`
	TournamentCountDelta   int       `gorm:"not null"`
	GamesPlayedDelta       int       `gorm:"not null"`
	PointsCentsDelta       int64     `gorm:"not null"`
	GoalDifferenceDelta    int       `gorm:"not null"`
	Reason                 string    `gorm:"not null"`
	Administrator          string    `gorm:"not null"`
	CreatedAt              time.Time `gorm:"not null;index"`
	Status                 string    `gorm:"not null;index"`
	RevokedAt              *time.Time
	RevokedBy              string
	RevocationReason       string
	Revision               int64       `gorm:"not null;default:1"`
	Version                int64       `gorm:"not null;default:1"`
	SupersedesCorrectionID *uint       `gorm:"index"`
	ReplacedByCorrectionID *uint       `gorm:"index"`
	Player                 PlayerModel `gorm:"foreignKey:PlayerRef;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT"`
}

// ManualRankingCorrectionRevisionModel is append-only. Hash and PreviousHash
// make accidental edits/deletions detectable when the audit trail is exported.
type ManualRankingCorrectionRevisionModel struct {
	ID                   uint      `gorm:"primaryKey"`
	CorrectionID         uint      `gorm:"not null;index"`
	Revision             int64     `gorm:"not null"`
	Action               string    `gorm:"not null"`
	EffectiveDate        time.Time `gorm:"not null"`
	TournamentCountDelta int       `gorm:"not null"`
	GamesPlayedDelta     int       `gorm:"not null"`
	PointsCentsDelta     int64     `gorm:"not null"`
	GoalDifferenceDelta  int       `gorm:"not null"`
	Reason               string    `gorm:"not null"`
	Administrator        string    `gorm:"not null"`
	OccurredAt           time.Time `gorm:"not null;index"`
	PreviousHash         string
	Hash                 string `gorm:"not null;uniqueIndex"`
}

type Repository struct {
	db    *gorm.DB
	clock ports.Clock
}

func OpenSQLite(path string, clock ports.Clock) (*Repository, *gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		return nil, nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := db.AutoMigrate(&TournamentModel{}, &DisciplineModel{}, &StageModel{}, &GroupModel{}, &EntryModel{}, &PlayerModel{}, &PlayerNameAliasModel{}, &SourcePlayerIdentityModel{}, &EntryMembershipModel{}, &GroupStandingModel{}, &AllocationModel{}, &StandingModel{}, &PlayerAggregateModel{}, &PlayerMergeAuditModel{}, &TournamentInclusionAuditModel{}, &ManualRankingCorrectionModel{}, &ManualRankingCorrectionRevisionModel{}); err != nil {
		return nil, db, fmt.Errorf("auto migrate tournaments: %w", err)
	}
	if err := ensurePlayerAliases(db); err != nil {
		return nil, db, fmt.Errorf("ensure player aliases: %w", err)
	}
	db = db.Session(&gorm.Session{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	return &Repository{db: db, clock: clock}, db, nil
}

func New(db *gorm.DB, clock ports.Clock) (*Repository, error) {
	if db == nil {
		return nil, errors.New("gorm db is required")
	}
	if err := db.AutoMigrate(&TournamentModel{}, &DisciplineModel{}, &StageModel{}, &GroupModel{}, &EntryModel{}, &PlayerModel{}, &PlayerNameAliasModel{}, &SourcePlayerIdentityModel{}, &EntryMembershipModel{}, &GroupStandingModel{}, &AllocationModel{}, &StandingModel{}, &PlayerAggregateModel{}, &PlayerMergeAuditModel{}, &TournamentInclusionAuditModel{}, &ManualRankingCorrectionModel{}, &ManualRankingCorrectionRevisionModel{}); err != nil {
		return nil, fmt.Errorf("auto migrate tournaments: %w", err)
	}
	if err := ensurePlayerAliases(db); err != nil {
		return nil, fmt.Errorf("ensure player aliases: %w", err)
	}
	db = db.Session(&gorm.Session{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	return &Repository{db: db, clock: clock}, nil
}

func ensurePlayerAliases(db *gorm.DB) error {
	var players []PlayerModel
	if err := db.Where("merged_into_player_id IS NULL").Find(&players).Error; err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		for _, player := range players {
			var alias PlayerNameAliasModel
			err := tx.Where("name_key = ?", player.CanonicalNameKey).First(&alias).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				if err := tx.Create(&PlayerNameAliasModel{NameKey: player.CanonicalNameKey, DisplayName: player.DisplayName, PlayerID: player.ID}).Error; err != nil {
					return fmt.Errorf("create alias for player %d: %w", player.ID, err)
				}
				continue
			}
			if err != nil {
				return err
			}
			if alias.PlayerID != player.ID {
				return fmt.Errorf("canonical alias conflict for %q", player.CanonicalNameKey)
			}
		}
		return nil
	})
}

func (r *Repository) UpsertMany(ctx context.Context, tournaments []domain.Tournament) (result domain.SyncResult, err error) {
	tournaments = normalizeTournamentBatch(tournaments)
	result.Found = len(tournaments)
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, tournament := range tournaments {
			if tournament.Source == "" || (tournament.SourceID == "" && tournament.SourceKey == "") {
				return fmt.Errorf("invalid tournament identity for %q", tournament.Name)
			}
			if tournament.LastSeenAt.IsZero() {
				tournament.LastSeenAt = r.clock.Now()
			}
			var existing TournamentModel
			query := tx.Where("source = ? AND source_key = ?", tournament.Source, tournament.SourceKey)
			findErr := query.First(&existing).Error
			if errors.Is(findErr, gorm.ErrRecordNotFound) && tournament.SourceID != "" {
				findErr = tx.Where("source = ? AND source_id = ?", tournament.Source, tournament.SourceID).First(&existing).Error
			}
			if errors.Is(findErr, gorm.ErrRecordNotFound) {
				if createErr := tx.Create(toModel(tournament)).Error; createErr != nil {
					return fmt.Errorf("insert %s: %w", tournament.Identity(), createErr)
				}
				result.Inserted++
				continue
			}
			if findErr != nil {
				return fmt.Errorf("find %s: %w", tournament.Identity(), findErr)
			}
			if same(existing, tournament) {
				if updateErr := tx.Model(&existing).Update("last_seen_at", tournament.LastSeenAt).Error; updateErr != nil {
					return fmt.Errorf("touch %s: %w", tournament.Identity(), updateErr)
				}
				result.Unchanged++
				continue
			}
			updates := toModel(tournament)
			if existing.Status != tournament.Status || existing.IsLive != tournament.IsLive {
				updates.PreviousStatus = existing.Status
				updates.PreviousIsLive = existing.IsLive
				updates.StatusTransition = statusTransition(existing.IsLive, tournament.IsLive)
				now := r.clock.Now()
				updates.StatusTransitionAt = &now
			}
			if updateErr := tx.Model(&existing).Updates(map[string]any{
				"source_id": updates.SourceID, "source_key": updates.SourceKey, "name": updates.Name,
				"date": updates.Date, "start_time": updates.StartTime, "end_time": updates.EndTime,
				"venue": updates.Venue, "city": updates.City, "country": updates.Country,
				"organizer": updates.Organizer, "status": updates.Status, "entry_type": updates.EntryType, "is_live": updates.IsLive,
				"previous_status": updates.PreviousStatus, "previous_is_live": updates.PreviousIsLive,
				"status_transition": updates.StatusTransition, "status_transition_at": updates.StatusTransitionAt,
				"participants": updates.Participants,
				"url":          updates.URL, "last_seen_at": updates.LastSeenAt,
			}).Error; updateErr != nil {
				return fmt.Errorf("update %s: %w", tournament.Identity(), updateErr)
			}
			result.Updated++
		}
		return nil
	})
	return result, err
}

// normalizeTournamentBatch makes the source key the primary identity for a
// batch. A source can repeat a tournament in a listing response; only the
// last normalized representation is persisted, deterministically.
func normalizeTournamentBatch(tournaments []domain.Tournament) []domain.Tournament {
	byIdentity := make(map[string]domain.Tournament, len(tournaments))
	order := make([]string, 0, len(tournaments))
	for _, tournament := range tournaments {
		tournament.Source = strings.TrimSpace(tournament.Source)
		tournament.SourceID = strings.TrimSpace(tournament.SourceID)
		tournament.SourceKey = strings.TrimSpace(tournament.SourceKey)
		if tournament.SourceKey == "" && tournament.SourceID != "" {
			tournament.SourceKey = tournament.SourceID
		}
		key := tournament.Source + "\x00" + tournament.SourceKey
		if _, exists := byIdentity[key]; !exists {
			order = append(order, key)
		}
		byIdentity[key] = tournament
	}
	result := make([]domain.Tournament, 0, len(order))
	for _, key := range order {
		result = append(result, byIdentity[key])
	}
	return result
}

func (r *Repository) FindBySourceID(ctx context.Context, source, sourceID string) (domain.Tournament, error) {
	var model TournamentModel
	if err := r.db.WithContext(ctx).Where("source = ? AND source_id = ?", source, sourceID).First(&model).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.Tournament{}, fmt.Errorf("find tournament %s: %w", sourceID, ports.ErrNotFound)
		}
		return domain.Tournament{}, err
	}
	return fromModel(model), nil
}

func (r *Repository) ListPlayerRanking(ctx context.Context) ([]domain.PlayerAggregate, error) {
	// Read the current source rows and active corrections together. The
	// materialized aggregate remains a cache, while this reader makes a future
	// effective date visible without waiting for another crawl/recalculation.
	return r.listCurrentRanking(ctx, r.clock.Now())
}

// ListAvailableRankingYears returns calendar years that have at least one
// included, completed tournament with a complete, non-failed standings
// snapshot containing usable standing rows. Years are returned newest first.
func (r *Repository) ListAvailableRankingYears(ctx context.Context) ([]int, error) {
	tournaments, err := r.qualifiedRankingTournaments(ctx, nil)
	if err != nil {
		return nil, err
	}
	location, err := time.LoadLocation(domain.RankingLocation)
	if err != nil {
		return nil, fmt.Errorf("load ranking timezone: %w", err)
	}
	seen := make(map[int]struct{}, len(tournaments))
	for _, tournament := range tournaments {
		if tournament.Date != nil {
			seen[tournament.Date.In(location).Year()] = struct{}{}
		}
	}
	now := r.clock.Now()
	var corrections []ManualRankingCorrectionModel
	if err := r.db.WithContext(ctx).Where("status = ? AND effective_date <= ?", manualCorrectionActive, now).Find(&corrections).Error; err != nil {
		return nil, fmt.Errorf("list correction years: %w", err)
	}
	for _, correction := range corrections {
		seen[correction.EffectiveDate.In(location).Year()] = struct{}{}
	}
	years := make([]int, 0, len(seen))
	for year := range seen {
		years = append(years, year)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(years)))
	return years, nil
}

// ListPlayerRankingForYear calculates a period ranking from the shared
// source/correction aggregation. Effective dates after the current clock are
// excluded, so a ranking changes at the effective instant without a crawl.
func (r *Repository) ListPlayerRankingForYear(ctx context.Context, year int) ([]domain.PlayerAggregate, error) {
	rows, err := r.listRankingRowsForYear(ctx, year)
	if err != nil {
		return nil, err
	}
	now := r.clock.Now()
	corrections, err := r.activeCorrections(ctx, now, &year, nil)
	if err != nil {
		return nil, err
	}
	return r.aggregateRankingRows(ctx, rows, corrections)
}

// qualifiedRankingTournaments centralizes the inclusion/completion/standing
// qualification shared by the available-year list and yearly values.
func (r *Repository) qualifiedRankingTournaments(ctx context.Context, year *int) ([]TournamentModel, error) {
	var models []TournamentModel
	query := r.db.WithContext(ctx).
		Where("included_in_ranking = ?", true).
		Where("standings_sync_complete = ?", true).
		Where("last_standings_sync_failed = ?", false).
		Where("date IS NOT NULL")
	if err := query.Find(&models).Error; err != nil {
		return nil, fmt.Errorf("list ranking tournaments: %w", err)
	}
	if len(models) == 0 {
		return []TournamentModel{}, nil
	}
	now := r.clock.Now()
	location, err := time.LoadLocation(domain.RankingLocation)
	if err != nil {
		return nil, fmt.Errorf("load ranking timezone: %w", err)
	}
	candidates := make([]TournamentModel, 0, len(models))
	for _, model := range models {
		tournament := fromModel(model)
		if !domain.IsTournamentCompleted(tournament, now) || model.Date == nil {
			continue
		}
		if year != nil && model.Date.In(location).Year() != *year {
			continue
		}
		candidates = append(candidates, model)
	}
	if len(candidates) == 0 {
		return []TournamentModel{}, nil
	}
	refs := make([]uint, 0, len(candidates))
	for _, model := range candidates {
		refs = append(refs, model.ID)
	}
	var standingRefs []uint
	if err := r.db.WithContext(ctx).Model(&StandingModel{}).
		Where("tournament_ref IN ?", refs).
		Group("tournament_ref").Pluck("tournament_ref", &standingRefs).Error; err != nil {
		return nil, fmt.Errorf("find complete ranking standings: %w", err)
	}
	withStandings := make(map[uint]struct{}, len(standingRefs))
	for _, ref := range standingRefs {
		withStandings[ref] = struct{}{}
	}
	qualified := make([]TournamentModel, 0, len(candidates))
	for _, model := range candidates {
		if _, ok := withStandings[model.ID]; ok {
			qualified = append(qualified, model)
		}
	}
	return qualified, nil
}

func sortPlayerRanking(ranking []domain.PlayerAggregate) {
	sort.SliceStable(ranking, func(i, j int) bool {
		left, right := ranking[i], ranking[j]
		if left.PointsAvailable != right.PointsAvailable {
			return left.PointsAvailable
		}
		if compareOptionalInt64(left.TotalPointsCents, right.TotalPointsCents) != 0 {
			return compareOptionalInt64(left.TotalPointsCents, right.TotalPointsCents) > 0
		}
		if compareOptionalInt64(left.PointsPerGameCents, right.PointsPerGameCents) != 0 {
			return compareOptionalInt64(left.PointsPerGameCents, right.PointsPerGameCents) > 0
		}
		if compareOptionalInt(left.GoalDifference, right.GoalDifference) != 0 {
			return compareOptionalInt(left.GoalDifference, right.GoalDifference) > 0
		}
		if left.TournamentCount != right.TournamentCount {
			return left.TournamentCount > right.TournamentCount
		}
		if left.PlayerName != right.PlayerName {
			return left.PlayerName < right.PlayerName
		}
		return left.PlayerKey < right.PlayerKey
	})
}

func compareOptionalInt64(left, right *int64) int {
	if left == nil && right == nil {
		return 0
	}
	if left == nil {
		return -1
	}
	if right == nil {
		return 1
	}
	if *left < *right {
		return -1
	}
	if *left > *right {
		return 1
	}
	return 0
}

func compareOptionalInt(left, right *int) int {
	if left == nil && right == nil {
		return 0
	}
	if left == nil {
		return -1
	}
	if right == nil {
		return 1
	}
	if *left < *right {
		return -1
	}
	if *left > *right {
		return 1
	}
	return 0
}

func (r *Repository) UpsertStandingSnapshot(ctx context.Context, snapshot domain.StandingSnapshot) (result domain.StandingSyncResult, err error) {
	if !snapshot.Complete {
		return result, fmt.Errorf("refuse incomplete standings snapshot for tournament %s", snapshot.TournamentID)
	}
	result.Found = len(snapshot.Standings)
	hash := domain.HashStandingSnapshot(snapshot)
	standings := append([]domain.TournamentStanding(nil), snapshot.Standings...)
	sort.SliceStable(standings, func(i, j int) bool { return standings[i].StandingKey < standings[j].StandingKey })
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		tournament, findTournamentErr := r.findTournament(tx, snapshot.Source, snapshot.TournamentID)
		if findTournamentErr != nil {
			return findTournamentErr
		}
		if err := upsertHierarchy(tx, snapshot); err != nil {
			return err
		}
		touched := make(map[string]struct{})
		for _, standing := range standings {
			standing.PlayerKey = domain.PlayerKey(standing.PlayerName)
			if err := validateStanding(standing, snapshot); err != nil {
				return err
			}
			now := r.clock.Now()
			player, playerResult, playerErr := r.upsertPlayer(tx, standing, now)
			if playerErr != nil {
				return playerErr
			}
			result.PlayersInserted += playerResult.inserted
			result.PlayersUpdated += playerResult.updated
			standing.PlayerKey = player.CanonicalNameKey
			touched[standing.PlayerKey] = struct{}{}
			standing.LastSeenAt = now
			standingModel := toStandingModel(standing, tournament.ID, player.ID)
			if err := bindAllocationsToPlayer(tx, snapshot.Source, standing, player.ID); err != nil {
				return err
			}
			existing, findErr := findStanding(tx, standing)
			if errors.Is(findErr, gorm.ErrRecordNotFound) {
				if createErr := tx.Create(standingModel).Error; createErr != nil {
					return fmt.Errorf("insert standing %s: %w", standing.StandingKey, createErr)
				}
				result.StandingsInserted++
				continue
			}
			if findErr != nil {
				return fmt.Errorf("find standing %s: %w", standing.StandingKey, findErr)
			}
			if sameStanding(existing, standing, tournament.ID, player.ID) {
				if updateErr := tx.Model(&existing).Update("last_seen_at", now).Error; updateErr != nil {
					return fmt.Errorf("touch standing %s: %w", standing.StandingKey, updateErr)
				}
				result.StandingsUnchanged++
				continue
			}
			if updateErr := tx.Model(&existing).Updates(standingUpdates(standingModel)).Error; updateErr != nil {
				return fmt.Errorf("update standing %s: %w", standing.StandingKey, updateErr)
			}
			result.StandingsUpdated++
		}
		for playerKey := range touched {
			if err := r.recalculateAggregate(tx, snapshot.Source, playerKey, r.clock.Now()); err != nil {
				return err
			}
			result.AggregatesRecalculated++
		}
		if err := r.recordCompleteStandingSync(tx, &tournament, hash, r.clock.Now()); err != nil {
			return err
		}
		return nil
	})
	return result, err
}

func upsertHierarchy(tx *gorm.DB, snapshot domain.StandingSnapshot) error {
	for _, value := range snapshot.Disciplines {
		model := DisciplineModel{Source: snapshot.Source, SourceID: value.SourceID, TournamentID: value.TournamentID, Name: value.Name, ShortName: value.ShortName, EntryType: value.EntryType}
		if err := upsertSourceRecord(tx, &DisciplineModel{}, &model, snapshot.Source, value.SourceID, "source_id", map[string]any{"tournament_id": model.TournamentID, "name": model.Name, "short_name": model.ShortName, "entry_type": model.EntryType}); err != nil {
			return err
		}
	}
	for _, value := range snapshot.Stages {
		model := StageModel{Source: snapshot.Source, SourceID: value.SourceID, DisciplineID: value.DisciplineID, TournamentID: value.TournamentID, State: value.State}
		if err := upsertSourceRecord(tx, &StageModel{}, &model, snapshot.Source, value.SourceID, "source_id", map[string]any{"discipline_id": model.DisciplineID, "tournament_id": model.TournamentID, "state": model.State}); err != nil {
			return err
		}
	}
	for _, value := range snapshot.Groups {
		model := GroupModel{Source: snapshot.Source, SourceID: value.SourceID, StageID: value.StageID, DisciplineID: value.DisciplineID, TournamentID: value.TournamentID, Name: value.Name, State: value.State}
		if err := upsertSourceRecord(tx, &GroupModel{}, &model, snapshot.Source, value.SourceID, "source_id", map[string]any{"stage_id": model.StageID, "discipline_id": model.DisciplineID, "tournament_id": model.TournamentID, "name": model.Name, "state": model.State}); err != nil {
			return err
		}
	}
	for _, value := range snapshot.Entries {
		model := EntryModel{Source: snapshot.Source, SourceID: value.SourceID, TournamentID: value.TournamentID, Name: value.Name, EntryType: value.EntryType}
		if err := upsertSourceRecord(tx, &EntryModel{}, &model, snapshot.Source, value.SourceID, "source_id", map[string]any{"tournament_id": model.TournamentID, "name": model.Name, "entry_type": model.EntryType}); err != nil {
			return err
		}
	}
	for _, value := range snapshot.Memberships {
		model := EntryMembershipModel{Source: snapshot.Source, EntryID: value.EntryID, PlayerID: value.PlayerID, TournamentID: value.TournamentID, PlayerName: value.PlayerName}
		var existing EntryMembershipModel
		err := tx.Where("source = ? AND entry_id = ? AND player_id = ?", model.Source, model.EntryID, model.PlayerID).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if err := tx.Create(&model).Error; err != nil {
				return fmt.Errorf("insert entry membership: %w", err)
			}
		} else if err != nil {
			return err
		} else if err := tx.Model(&existing).Updates(map[string]any{"tournament_id": model.TournamentID, "player_name": model.PlayerName}).Error; err != nil {
			return fmt.Errorf("update entry membership: %w", err)
		}
	}
	for _, value := range snapshot.GroupStandings {
		model := GroupStandingModel{Source: snapshot.Source, SourceID: value.SourceID, TournamentID: value.TournamentID, GroupID: value.GroupID, EntryID: value.EntryID, EntryName: value.EntryName, Rank: value.Rank, Result: value.Result, Preliminary: value.Preliminary, FinalResult: value.FinalResult, PointsCents: value.PointsCents, PointsPerMatchCents: value.PointsPerMatchCents, CorrectedPointsPerMatchCents: value.CorrectedPointsPerMatchCents, HasCorrectedValue: value.HasCorrectedValue, GamesPlayed: value.GamesPlayed, GoalDifference: value.GoalDifference, URL: value.URL}
		if err := upsertSourceRecord(tx, &GroupStandingModel{}, &model, snapshot.Source, value.SourceID, "source_id", map[string]any{"tournament_id": model.TournamentID, "group_id": model.GroupID, "entry_id": model.EntryID, "entry_name": model.EntryName, "rank": model.Rank, "result": model.Result, "preliminary": model.Preliminary, "final_result": model.FinalResult, "points_cents": model.PointsCents, "points_per_match_cents": model.PointsPerMatchCents, "corrected_points_per_match_cents": model.CorrectedPointsPerMatchCents, "has_corrected_value": model.HasCorrectedValue, "games_played": model.GamesPlayed, "goal_difference": model.GoalDifference, "url": model.URL}); err != nil {
			return err
		}
	}
	for _, value := range snapshot.Allocations {
		model := AllocationModel{Source: snapshot.Source, StandingID: value.StandingID, EntryID: value.EntryID, PlayerID: value.PlayerID, PointsCents: value.PointsCents}
		var existing AllocationModel
		err := tx.Where("source = ? AND standing_id = ? AND player_id = ?", model.Source, model.StandingID, model.PlayerID).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if err := tx.Create(&model).Error; err != nil {
				return fmt.Errorf("insert standing allocation: %w", err)
			}
		} else if err != nil {
			return err
		} else if err := tx.Model(&existing).Updates(map[string]any{"entry_id": model.EntryID, "points_cents": model.PointsCents}).Error; err != nil {
			return fmt.Errorf("update standing allocation: %w", err)
		}
	}
	return nil
}

func upsertSourceRecord(tx *gorm.DB, model any, value any, source, sourceID, key string, updates map[string]any) error {
	var queryModel any
	switch model.(type) {
	case *DisciplineModel:
		queryModel = &DisciplineModel{}
	case *StageModel:
		queryModel = &StageModel{}
	case *GroupModel:
		queryModel = &GroupModel{}
	case *EntryModel:
		queryModel = &EntryModel{}
	case *GroupStandingModel:
		queryModel = &GroupStandingModel{}
	default:
		return fmt.Errorf("unsupported hierarchy model")
	}
	err := tx.Where("source = ? AND "+key+" = ?", source, sourceID).First(queryModel).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if err := tx.Create(value).Error; err != nil {
			return fmt.Errorf("insert hierarchy %s: %w", sourceID, err)
		}
		return nil
	}
	if err != nil {
		return err
	}
	if err := tx.Model(queryModel).Updates(updates).Error; err != nil {
		return fmt.Errorf("update hierarchy %s: %w", sourceID, err)
	}
	return nil
}

func (r *Repository) MarkStandingSyncFailed(ctx context.Context, source, tournamentID string) error {
	query := r.db.WithContext(ctx).Where("source = ? AND source_id = ?", source, tournamentID)
	result := query.Model(&TournamentModel{}).Updates(map[string]any{
		"standings_sync_complete":    false,
		"last_standings_sync_failed": true,
	})
	if result.Error != nil {
		return fmt.Errorf("mark standings sync failed for %s: %w", tournamentID, result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("mark standings sync failed for %s: %w", tournamentID, ports.ErrNotFound)
	}
	return nil
}

func (r *Repository) recordCompleteStandingSync(tx *gorm.DB, tournament *TournamentModel, hash string, now time.Time) error {
	identical := tournament.StandingsSyncComplete && tournament.StandingsHash != "" && tournament.StandingsHash == hash
	count := 1
	if identical {
		count = tournament.ConsecutiveIdenticalCompleteSnapshots + 1
	}
	updates := map[string]any{
		"standings_synced_at":                      now,
		"standings_hash":                           hash,
		"standings_sync_complete":                  true,
		"last_standings_sync_failed":               false,
		"consecutive_identical_complete_snapshots": count,
	}
	if tournament.IsLive {
		updates["finalized_at"] = nil
	} else if !identical {
		updates["finalized_at"] = nil
	} else if count >= 2 {
		updates["finalized_at"] = now
	}
	if err := tx.Model(tournament).Updates(updates).Error; err != nil {
		return fmt.Errorf("record complete standings sync for %d: %w", tournament.ID, err)
	}
	return nil
}

type playerUpsertResult struct{ inserted, updated int }

func (r *Repository) findTournament(tx *gorm.DB, source, tournamentID string) (TournamentModel, error) {
	var tournament TournamentModel
	query := tx.Where("source = ?", source)
	if tournamentID != "" {
		query = query.Where("source_id = ?", tournamentID)
	} else {
		query = query.Where("source_key = ?", tournamentID)
	}
	if err := query.First(&tournament).Error; err != nil {
		return tournament, fmt.Errorf("find tournament %s for standings: %w", tournamentID, err)
	}
	return tournament, nil
}

func (r *Repository) upsertPlayer(tx *gorm.DB, standing domain.TournamentStanding, now time.Time) (PlayerModel, playerUpsertResult, error) {
	result := playerUpsertResult{}
	nameKey := domain.PlayerKey(standing.PlayerName)
	var alias PlayerNameAliasModel
	findErr := tx.Where("name_key = ?", nameKey).First(&alias).Error
	externalID := standing.PlayerID
	if errors.Is(findErr, gorm.ErrRecordNotFound) {
		model := toPlayerModel(standing, now)
		if err := tx.Create(&model).Error; err != nil {
			return model, result, fmt.Errorf("insert canonical player %s: %w", nameKey, err)
		}
		result.inserted++
		if err := tx.Create(&PlayerNameAliasModel{NameKey: nameKey, DisplayName: strings.TrimSpace(standing.PlayerName), PlayerID: model.ID}).Error; err != nil {
			return model, result, fmt.Errorf("insert player alias %s: %w", nameKey, err)
		}
		if err := upsertSourcePlayerIdentity(tx, standing.Source, externalID, nameKey, model.ID); err != nil {
			return model, result, err
		}
		return model, result, nil
	}
	if findErr != nil {
		return PlayerModel{}, result, fmt.Errorf("find player alias %s: %w", nameKey, findErr)
	}
	player, err := resolvePlayerRoot(tx, alias.PlayerID)
	if err != nil {
		return PlayerModel{}, result, err
	}
	changed := nameKey == player.CanonicalNameKey && player.DisplayName != strings.TrimSpace(standing.PlayerName)
	updates := map[string]any{"last_seen_at": now}
	if changed {
		updates["display_name"] = strings.TrimSpace(standing.PlayerName)
	}
	if changed || len(updates) > 1 {
		if err := tx.Model(&player).Updates(updates).Error; err != nil {
			return player, result, fmt.Errorf("update player %s: %w", nameKey, err)
		}
	}
	if changed {
		player.DisplayName = strings.TrimSpace(standing.PlayerName)
	}
	player.LastSeenAt = now
	if err := upsertSourcePlayerIdentity(tx, standing.Source, externalID, nameKey, player.ID); err != nil {
		return player, result, err
	}
	if changed {
		result.updated++
	}
	return player, result, nil
}

func resolvePlayerRoot(tx *gorm.DB, playerID uint) (PlayerModel, error) {
	visited := make(map[uint]struct{})
	currentID := playerID
	for {
		if currentID == 0 {
			return PlayerModel{}, fmt.Errorf("player %d not found", playerID)
		}
		if _, seen := visited[currentID]; seen {
			return PlayerModel{}, fmt.Errorf("player merge cycle detected at %d", currentID)
		}
		visited[currentID] = struct{}{}
		var player PlayerModel
		if err := tx.First(&player, currentID).Error; err != nil {
			return PlayerModel{}, fmt.Errorf("load player %d: %w", currentID, err)
		}
		if player.MergedIntoPlayerID == nil {
			return player, nil
		}
		currentID = *player.MergedIntoPlayerID
	}
}

func bindAllocationsToPlayer(tx *gorm.DB, source string, standing domain.TournamentStanding, playerID uint) error {
	if standing.StandingID == "" || standing.PlayerID == "" {
		return nil
	}
	return tx.Model(&AllocationModel{}).
		Where("source = ? AND standing_id = ? AND player_id = ?", source, standing.StandingID, standing.PlayerID).
		Update("player_ref", playerID).Error
}

func upsertSourcePlayerIdentity(tx *gorm.DB, source, externalID, nameKey string, playerRef uint) error {
	if externalID == "" {
		return nil
	}
	var identity SourcePlayerIdentityModel
	query := tx.Where("source = ? AND external_id = ? AND name_key = ?", source, externalID, nameKey)
	err := query.First(&identity).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return tx.Create(&SourcePlayerIdentityModel{Source: source, ExternalID: externalID, NameKey: nameKey, PlayerRef: playerRef}).Error
	}
	if err != nil {
		return fmt.Errorf("find source player identity: %w", err)
	}
	if identity.PlayerRef != playerRef {
		return fmt.Errorf("ambiguous source player identity %s/%s", source, externalID)
	}
	return nil
}

func findStanding(tx *gorm.DB, standing domain.TournamentStanding) (StandingModel, error) {
	var existing StandingModel
	var err error
	if standing.StandingID != "" {
		err = tx.Where("source = ? AND source_standing_id = ?", standing.Source, standing.StandingID).First(&existing).Error
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return existing, err
		}
		return existing, gorm.ErrRecordNotFound
	}
	err = tx.Where("source = ? AND tournament_id = ? AND player_key = ?", standing.Source, standing.TournamentID, standing.PlayerKey).First(&existing).Error
	return existing, err
}

func (r *Repository) recalculateAggregate(tx *gorm.DB, source, playerKey string, now time.Time) error {
	var rows []StandingModel
	if err := tx.Joins("JOIN tournament_models ON tournament_models.id = standing_models.tournament_ref").Where("standing_models.source = ? AND standing_models.player_key = ? AND tournament_models.included_in_ranking = ?", source, playerKey, true).Find(&rows).Error; err != nil {
		return fmt.Errorf("load standings for aggregate %s: %w", playerKey, err)
	}
	var player PlayerModel
	if err := tx.Where("canonical_name_key = ?", playerKey).First(&player).Error; err != nil {
		return fmt.Errorf("find aggregate player %s: %w", playerKey, err)
	}
	var corrections []ManualRankingCorrectionModel
	if err := tx.Where("player_ref = ? AND status = ? AND effective_date <= ?", player.ID, manualCorrectionActive, now).Order("id ASC").Find(&corrections).Error; err != nil {
		return fmt.Errorf("load ranking corrections for aggregate %s: %w", playerKey, err)
	}
	tournaments := make(map[string]struct{})
	var totalCents int64
	pointsAvailable := len(rows) > 0
	gamesAvailable := true
	goalsAvailable := true
	gamesTotal := 0
	goalDifferenceTotal := 0
	for i := range rows {
		tournaments[rows[i].TournamentID] = struct{}{}
		if rows[i].PointsCents != nil {
			var err error
			totalCents, err = addInt64Checked(totalCents, *rows[i].PointsCents)
			if err != nil {
				return err
			}
		} else {
			pointsAvailable = false
		}
		if rows[i].GamesPlayed == nil {
			gamesAvailable = false
		} else {
			var err error
			gamesTotal, err = addIntChecked(gamesTotal, *rows[i].GamesPlayed)
			if err != nil {
				return err
			}
		}
		if rows[i].GoalDifference == nil {
			goalsAvailable = false
		} else {
			var err error
			goalDifferenceTotal, err = addIntChecked(goalDifferenceTotal, *rows[i].GoalDifference)
			if err != nil {
				return err
			}
		}
	}
	correctionsOnly := len(rows) == 0
	for _, correction := range corrections {
		if correctionsOnly || pointsAvailable {
			var err error
			totalCents, err = addInt64Checked(totalCents, correction.PointsCentsDelta)
			if err != nil {
				return err
			}
		}
		if correctionsOnly || gamesAvailable {
			var err error
			gamesTotal, err = addIntChecked(gamesTotal, correction.GamesPlayedDelta)
			if err != nil {
				return err
			}
		}
		if correctionsOnly || goalsAvailable {
			var err error
			goalDifferenceTotal, err = addIntChecked(goalDifferenceTotal, correction.GoalDifferenceDelta)
			if err != nil {
				return err
			}
		}
		// A correction is an explicit contribution to the count, while source
		// standings continue to define the de-duplicated tournament base.
		if correction.TournamentCountDelta != 0 {
			// Applied below as a scalar to keep the tournament set untouched.
		}
	}
	tournamentCount := len(tournaments)
	for _, correction := range corrections {
		var err error
		tournamentCount, err = addIntChecked(tournamentCount, correction.TournamentCountDelta)
		if err != nil {
			return err
		}
	}
	if correctionsOnly {
		pointsAvailable = len(corrections) > 0
	}
	var aggregate PlayerAggregateModel
	findErr := tx.Where("source = ? AND player_key = ?", source, playerKey).First(&aggregate).Error
	model := PlayerAggregateModel{Source: source, PlayerKey: playerKey, PlayerRef: player.ID, PlayerName: player.DisplayName,
		TournamentCount: tournamentCount, PointsAvailable: pointsAvailable,
		GamesAvailable: gamesAvailable, GoalsAvailable: goalsAvailable, RecalculatedAt: now}
	if pointsAvailable {
		model.TotalPointsCents = &totalCents
	}
	if gamesAvailable {
		model.GamesPlayed = &gamesTotal
	}
	if goalsAvailable {
		model.GoalDifference = &goalDifferenceTotal
	}
	if pointsAvailable && gamesAvailable && gamesTotal > 0 {
		pointsPerGameCents := roundCents(totalCents, int64(gamesTotal))
		model.PointsPerGameCents = &pointsPerGameCents
	}
	if errors.Is(findErr, gorm.ErrRecordNotFound) {
		if err := tx.Create(&model).Error; err != nil {
			return fmt.Errorf("insert aggregate %s: %w", playerKey, err)
		}
		return nil
	}
	if findErr != nil {
		return fmt.Errorf("find aggregate %s: %w", playerKey, findErr)
	}
	if err := tx.Model(&aggregate).Updates(map[string]any{
		"player_ref": model.PlayerRef, "player_name": model.PlayerName, "tournament_count": model.TournamentCount,
		"total_points_cents": model.TotalPointsCents, "games_played": model.GamesPlayed,
		"goal_difference": model.GoalDifference, "points_per_game_cents": model.PointsPerGameCents,
		"points_available": model.PointsAvailable, "games_available": model.GamesAvailable, "goals_available": model.GoalsAvailable,
		"recalculated_at": model.RecalculatedAt,
	}).Error; err != nil {
		return fmt.Errorf("update aggregate %s: %w", playerKey, err)
	}
	return nil
}

func validateStanding(standing domain.TournamentStanding, snapshot domain.StandingSnapshot) error {
	if standing.Source == "" || standing.Source != snapshot.Source || standing.TournamentID != snapshot.TournamentID || standing.PlayerKey == "" || standing.PlayerName == "" || standing.StandingKey == "" {
		return fmt.Errorf("invalid standing identity for tournament %s", snapshot.TournamentID)
	}
	return nil
}

func toPlayerModel(standing domain.TournamentStanding, now time.Time) PlayerModel {
	nameKey := domain.PlayerKey(standing.PlayerName)
	return PlayerModel{CanonicalNameKey: nameKey, DisplayName: strings.TrimSpace(standing.PlayerName), LastSeenAt: now}
}

func toStandingModel(standing domain.TournamentStanding, tournamentRef, playerRef uint) *StandingModel {
	var externalID *string
	if standing.StandingID != "" {
		value := standing.StandingID
		externalID = &value
	}
	stats, _ := json.Marshal(standing.Stats)
	return &StandingModel{Source: standing.Source, TournamentID: standing.TournamentID, TournamentRef: tournamentRef,
		SourceStandingID: externalID, StandingKey: standing.StandingKey, DisciplineID: standing.DisciplineID, StageID: standing.StageID, Group: standing.Group, PlayerID: standing.PlayerID,
		PlayerKey: standing.PlayerKey, PlayerRef: playerRef, PlayerName: standing.PlayerName, EntryID: standing.EntryID, EntryName: standing.EntryName, Team: standing.Team,
		Partner: standing.Partner, Rank: standing.Rank, Result: standing.Result, Preliminary: standing.Preliminary, FinalResult: standing.FinalResult,
		PointsCents: standing.PointsCents, PointsPerMatchCents: standing.PointsPerMatchCents, CorrectedPointsPerMatchCents: standing.CorrectedPointsPerMatchCents, HasCorrectedValue: standing.HasCorrectedValue,
		GamesPlayed: standing.GamesPlayed, GoalDifference: standing.GoalDifference,
		Status: standing.Status, StatsJSON: string(stats), URL: standing.URL, LastSeenAt: standing.LastSeenAt}
}

func standingUpdates(model *StandingModel) map[string]any {
	return map[string]any{"tournament_ref": model.TournamentRef, "source_standing_id": model.SourceStandingID, "standing_key": model.StandingKey,
		"group": model.Group, "player_id": model.PlayerID, "player_key": model.PlayerKey, "player_ref": model.PlayerRef,
		"discipline_id": model.DisciplineID, "stage_id": model.StageID, "player_name": model.PlayerName, "entry_id": model.EntryID, "entry_name": model.EntryName, "team": model.Team, "partner": model.Partner, "rank": model.Rank, "result": model.Result, "preliminary": model.Preliminary, "final_result": model.FinalResult, "points_cents": model.PointsCents, "points_per_match_cents": model.PointsPerMatchCents, "corrected_points_per_match_cents": model.CorrectedPointsPerMatchCents, "has_corrected_value": model.HasCorrectedValue,
		"games_played": model.GamesPlayed, "goal_difference": model.GoalDifference,
		"status": model.Status, "stats_json": model.StatsJSON, "url": model.URL, "last_seen_at": model.LastSeenAt}
}

func sameStanding(model StandingModel, standing domain.TournamentStanding, tournamentRef, playerRef uint) bool {
	stats, _ := json.Marshal(standing.Stats)
	return model.Source == standing.Source && model.TournamentID == standing.TournamentID && model.TournamentRef == tournamentRef && model.StandingKey == standing.StandingKey && model.DisciplineID == standing.DisciplineID && model.StageID == standing.StageID && model.Group == standing.Group && model.PlayerID == standing.PlayerID && model.PlayerKey == standing.PlayerKey && model.PlayerRef == playerRef && model.PlayerName == standing.PlayerName && model.EntryID == standing.EntryID && model.EntryName == standing.EntryName && model.Team == standing.Team && model.Partner == standing.Partner && sameInt(model.Rank, standing.Rank) && sameInt(model.Result, standing.Result) && sameInt(model.Preliminary, standing.Preliminary) && sameInt(model.FinalResult, standing.FinalResult) && sameInt64(model.PointsCents, standing.PointsCents) && sameInt64(model.PointsPerMatchCents, standing.PointsPerMatchCents) && sameInt64(model.CorrectedPointsPerMatchCents, standing.CorrectedPointsPerMatchCents) && sameBool(model.HasCorrectedValue, standing.HasCorrectedValue) && sameInt(model.GamesPlayed, standing.GamesPlayed) && sameInt(model.GoalDifference, standing.GoalDifference) && model.Status == standing.Status && model.StatsJSON == string(stats) && model.URL == standing.URL
}

func sameBool(a, b *bool) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func sameInt64(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func roundCents(totalCents, games int64) int64 {
	if games <= 0 {
		return 0
	}
	quotient, remainder := totalCents/games, totalCents%games
	if remainder < 0 {
		return -roundCents(-totalCents, games)
	}
	if remainder >= games-remainder {
		quotient++
	}
	return quotient
}

var errAggregateOverflow = errors.New("ranking aggregate arithmetic overflow")

func addIntChecked(left, right int) (int, error) {
	max := int(^uint(0) >> 1)
	min := -max - 1
	if (right > 0 && left > max-right) || (right < 0 && left < min-right) {
		return 0, errAggregateOverflow
	}
	return left + right, nil
}

func addInt64Checked(left, right int64) (int64, error) {
	max := int64(^uint64(0) >> 1)
	min := -max - 1
	if (right > 0 && left > max-right) || (right < 0 && left < min-right) {
		return 0, errAggregateOverflow
	}
	return left + right, nil
}

func toModel(t domain.Tournament) *TournamentModel {
	var sourceID *string
	if t.SourceID != "" {
		value := t.SourceID
		sourceID = &value
	}
	return &TournamentModel{ID: t.ID, Source: t.Source, SourceID: sourceID, SourceKey: t.SourceKey, Name: t.Name,
		Date: t.Date, StartTime: t.StartTime, EndTime: t.EndTime, Venue: t.Venue, City: t.City,
		Country: t.Country, Organizer: t.Organizer, Status: t.Status, EntryType: t.EntryType, IsLive: t.IsLive, PreviousStatus: t.PreviousStatus,
		PreviousIsLive: t.PreviousIsLive, StatusTransition: t.StatusTransition, StatusTransitionAt: t.StatusTransitionAt,
		Participants: t.Participants, URL: t.URL, LastSeenAt: t.LastSeenAt, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
		StandingsSyncedAt: t.StandingsSyncedAt, StandingsHash: t.StandingsHash, StandingsSyncComplete: t.StandingsSyncComplete,
		LastStandingsSyncFailed: t.LastStandingsSyncFailed, FinalizedAt: t.FinalizedAt,
		ConsecutiveIdenticalCompleteSnapshots: t.ConsecutiveIdenticalCompleteSnapshots, IncludedInRanking: t.IncludedInRanking,
		InclusionUpdatedAt: t.InclusionUpdatedAt, InclusionVersion: t.InclusionVersion, InclusionReason: t.InclusionReason}
}

func fromModel(m TournamentModel) domain.Tournament {
	var sourceID string
	if m.SourceID != nil {
		sourceID = *m.SourceID
	}
	return domain.Tournament{ID: m.ID, Source: m.Source, SourceID: sourceID, SourceKey: m.SourceKey, Name: m.Name,
		Date: m.Date, StartTime: m.StartTime, EndTime: m.EndTime, Venue: m.Venue, City: m.City, Country: m.Country,
		Organizer: m.Organizer, Status: m.Status, EntryType: m.EntryType, IsLive: m.IsLive, PreviousStatus: m.PreviousStatus,
		PreviousIsLive: m.PreviousIsLive, StatusTransition: m.StatusTransition, StatusTransitionAt: m.StatusTransitionAt,
		Participants: m.Participants, URL: m.URL, LastSeenAt: m.LastSeenAt, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
		StandingsSyncedAt: m.StandingsSyncedAt, StandingsHash: m.StandingsHash, StandingsSyncComplete: m.StandingsSyncComplete,
		LastStandingsSyncFailed: m.LastStandingsSyncFailed, FinalizedAt: m.FinalizedAt,
		ConsecutiveIdenticalCompleteSnapshots: m.ConsecutiveIdenticalCompleteSnapshots, IncludedInRanking: m.IncludedInRanking,
		InclusionUpdatedAt: m.InclusionUpdatedAt, InclusionVersion: m.InclusionVersion, InclusionReason: m.InclusionReason}
}

func fromAggregateModel(m PlayerAggregateModel) domain.PlayerAggregate {
	return domain.PlayerAggregate{
		Source:             m.Source,
		PlayerKey:          m.PlayerKey,
		PlayerName:         m.PlayerName,
		TournamentCount:    m.TournamentCount,
		TotalPointsCents:   m.TotalPointsCents,
		GamesPlayed:        m.GamesPlayed,
		GoalDifference:     m.GoalDifference,
		PointsPerGameCents: m.PointsPerGameCents,
		PointsAvailable:    m.PointsAvailable,
		GamesAvailable:     m.GamesAvailable,
		GoalsAvailable:     m.GoalsAvailable,
		RecalculatedAt:     m.RecalculatedAt,
	}
}

func same(m TournamentModel, t domain.Tournament) bool {
	if m.Source != t.Source || m.SourceKey != t.SourceKey || m.Name != t.Name || m.Venue != t.Venue || m.City != t.City || m.Country != t.Country || m.Organizer != t.Organizer || m.Status != t.Status || m.EntryType != t.EntryType || m.IsLive != t.IsLive || m.URL != t.URL {
		return false
	}
	if (m.SourceID == nil) != (t.SourceID == "") || (m.SourceID != nil && *m.SourceID != t.SourceID) || !sameTime(m.Date, t.Date) || !sameTime(m.StartTime, t.StartTime) || !sameTime(m.EndTime, t.EndTime) || !sameInt(m.Participants, t.Participants) {
		return false
	}
	return true
}

func statusTransition(previousLive, currentLive bool) string {
	if previousLive && !currentLive {
		return "live_to_not_live"
	}
	if !previousLive && currentLive {
		return "not_live_to_live"
	}
	return "status_changed"
}

func sameTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}
func sameInt(a, b *int) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
