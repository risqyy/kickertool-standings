export type Nullable<T> = T | null

export type RankingTrend = 'up' | 'down' | 'same' | 'new'

export interface RankingRow {
  rank: number
  trend: RankingTrend
  name: string
  includedTournamentCount: number
  gamesPlayed: Nullable<number>
  totalPoints: Nullable<string>
  pointsPerGame: Nullable<string>
  goalDifference: Nullable<number>
}

export interface RankingsResponse {
  items: RankingRow[]
  lastSyncAt: Nullable<string>
  availableYears: number[]
  selectedYear: Nullable<number>
}

export interface Tournament {
  id: number
  source: string
  sourceId: string
  sourceKey: string
  name: string
  date: Nullable<string>
  startTime: Nullable<string>
  endTime: Nullable<string>
  status: string
  isLive: boolean
  entryType: string
  includedInRanking: boolean
  inclusionUpdatedAt: Nullable<string>
  inclusionVersion: number
  inclusionReason: string
  url: string
  participants: Nullable<number>
  standingCount: number
  playerCount: number
  standingsComplete: boolean
  lastSyncError: boolean
  standingsSyncedAt: Nullable<string>
  lastSeenAt: string
}

export interface TournamentPage {
  items: Tournament[]
  page: number
  limit: number
  total: number
  last_sync_at: Nullable<string>
}

export interface Dashboard {
  tournamentCount: number
  includedTournamentCount: number
  excludedTournamentCount: number
  playerCount: number
  lastSyncAt: Nullable<string>
}

export interface Player {
  id: number
  displayName: string
  canonicalNameKey: string
  aliases: string[]
  matchedAlias?: string
  active: boolean
  mergedIntoPlayerId?: number
  tournamentCount: number
  gamesPlayed: Nullable<number>
  totalPointsCents: Nullable<number>
  pointsPerGameCents: Nullable<number>
	goalDifference: Nullable<number>
	rankingCorrectionVersion?: number
}

export interface RankingAggregate {
	tournamentCount: number
	gamesPlayed: Nullable<number>
	totalPointsCents: Nullable<number>
	pointsPerGameCents: Nullable<number>
	goalDifference: Nullable<number>
}

export interface ManualRankingCorrection {
	id: number
	playerId: number
	playerKey: string
	effectiveDate: string
	effectiveYear: number
	tournamentCountDelta: number
	gamesPlayedDelta: number
	pointsCentsDelta: number
	goalDifferenceDelta: number
	reason: string
	administrator: string
	createdAt: string
	status: 'active' | 'revoked' | 'replaced' | string
	revokedAt: Nullable<string>
	revokedBy?: string
	revocationReason?: string
	revision: number
	version: number
	supersedesCorrectionId?: number | null
	replacedByCorrectionId?: number | null
}

export interface ManualRankingCorrectionPreviewDetails {
	player: Player
	correction: ManualRankingCorrection
	before: RankingAggregate
	after: RankingAggregate
	expectedVersion: number
}

export interface ManualRankingCorrectionPreview extends ManualRankingCorrectionPreviewDetails {
  token: string
  preview?: ManualRankingCorrectionPreviewDetails
  result?: Pick<ManualRankingCorrectionChange, 'before' | 'after'>
	superseded?: ManualRankingCorrection
}

export interface ManualRankingCorrectionChange {
	correction: ManualRankingCorrection
	before: RankingAggregate
	after: RankingAggregate
  version: number
	superseded?: ManualRankingCorrection
}

export interface ManualRankingCorrectionListResponse {
  items: ManualRankingCorrection[]
  version: number
}

export interface ManualRankingCorrectionRevocationResponse extends ManualRankingCorrectionChange {}

export interface MergeAggregate {
  tournamentCount: number
  gamesPlayed: Nullable<number>
  totalPointsCents: Nullable<number>
  pointsPerGameCents: Nullable<number>
  goalDifference: Nullable<number>
}

export interface MergeResult {
  sourcePlayerId: number
  targetPlayerId: number
  sourceDisplayName: string
  targetDisplayName: string
  alreadyMerged: boolean
  transferredAliases: number
  transferredSourceIdentities: number
  transferredAllocations: number
  deduplicatedAllocations: number
  sourceBefore: Nullable<MergeAggregate>
  targetBefore: Nullable<MergeAggregate>
  targetAfter: Nullable<MergeAggregate>
}
