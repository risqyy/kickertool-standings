import type { Dashboard, ManualRankingCorrectionChange, ManualRankingCorrectionListResponse, ManualRankingCorrectionPreview, ManualRankingCorrectionRevocationResponse, MergeResult, Player, PlayerMergeAudit, PlayerMergeUndoPreview, PlayerMergeUndoResult, RankingsResponse, TournamentPage } from './types'

export class ApiError extends Error {
  status: number
  payload: unknown
  constructor(status: number, message: string, payload: unknown = null) {
    super(message)
    this.status = status
    this.payload = payload
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, { credentials: 'include', ...init })
  const payload = await response.json().catch(() => null)
  if (!response.ok) {
    const message = typeof payload === 'object' && payload && 'error' in payload ? String(payload.error) : 'HTTP ' + response.status
    throw new ApiError(response.status, message, payload)
  }
  return payload as T
}

function withCsrf(init: RequestInit, token: string): RequestInit {
  const headers = new Headers(init.headers)
  headers.set('X-CSRF-Token', token)
  return { ...init, headers }
}

async function adminMutation<T>(path: string, csrf: string, init: RequestInit): Promise<T> {
  try {
    return await request<T>(path, withCsrf(init, csrf))
  } catch (error) {
    // A browser may retain the page while the CSRF cookie expires or is
    // replaced by a proxy. Bootstrap the session once and retry only that
    // specific failure; origin and authentication failures remain terminal.
    if (!(error instanceof ApiError) || error.status !== 403 || error.message !== 'invalid csrf token') throw error
    const session = await getAdminSession()
    return request<T>(path, withCsrf(init, session.csrf_token))
  }
}

export async function getRankings(year?: number | null) {
  const search = year === undefined || year === null ? '' : '?' + new URLSearchParams({ year: String(year) })
  return request<RankingsResponse>('/api/v1/public/rankings' + search)
}
export async function getAdminSession() { return request<{ authenticated: boolean; csrf_token: string }>('/api/v1/admin/session') }
export async function getDashboard() { return (await request<{ data: Dashboard }>('/api/v1/admin/dashboard')).data }

export interface TournamentQuery { q?: string; included?: string; state?: string; source?: string; page?: number; limit?: number; sort?: string; direction?: 'asc' | 'desc' }
export async function getTournaments(query: TournamentQuery) {
  const search = new URLSearchParams()
  for (const [key, value] of Object.entries(query)) if (value !== undefined && value !== '') search.set(key, String(value))
  return request<TournamentPage>('/api/v1/admin/tournaments?' + search)
}

export async function setTournamentInclusion(csrf: string, id: number, included: boolean, expectedVersion: number, reason: string) {
  return adminMutation<{ changed: boolean; tournament: TournamentPage['items'][number] }>('/api/v1/admin/tournaments/' + id + '/inclusion', csrf, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ included, expectedVersion, reason })
  })
}

export async function searchPlayers(query: string) {
  const value = await request<{ items: Player[]; message?: string }>('/api/v1/admin/players/search?' + new URLSearchParams({ q: query }))
  return value.items
}
export async function previewMerge(csrf: string, sourcePlayerId: number, targetPlayerId: number) {
  return adminMutation<{ token: string; result: MergeResult }>('/api/v1/admin/players/merge/preview', csrf, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ sourcePlayerId, targetPlayerId }) })
}
export async function confirmMerge(csrf: string, token: string, targetDisplayName: string) {
  return adminMutation<{ alreadyMerged: boolean; result: MergeResult }>('/api/v1/admin/players/merge/confirm', csrf, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ token, targetDisplayName, confirmed: true }) })
}
export async function listPlayerMerges() {
  return (await request<{ items: PlayerMergeAudit[] }>('/api/v1/admin/players/merges')).items
}
export async function previewPlayerMergeUndo(csrf: string, mergeId: number) {
  return adminMutation<{ token: string; preview: PlayerMergeUndoPreview }>('/api/v1/admin/players/merges/' + mergeId + '/undo/preview', csrf, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' })
}
export async function confirmPlayerMergeUndo(csrf: string, mergeId: number, token: string, reason: string) {
  return adminMutation<PlayerMergeUndoResult>('/api/v1/admin/players/merges/' + mergeId + '/undo/confirm', csrf, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ token, reason, confirmed: true }) })
}

export interface ManualCorrectionInput {
	effectiveDate: string
	effectiveYear: number
	tournamentCountDelta: number
	gamesPlayedDelta: number
	pointsCentsDelta: number
	goalDifferenceDelta: number
	reason: string
	replaceCorrectionId?: number
}

export async function listManualCorrections(playerId: number) {
	return request<ManualRankingCorrectionListResponse>('/api/v1/admin/players/' + playerId + '/corrections')
}
export async function previewManualCorrection(csrf: string, playerId: number, input: ManualCorrectionInput) {
	return adminMutation<ManualRankingCorrectionPreview>('/api/v1/admin/players/' + playerId + '/corrections/preview', csrf, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(input) })
}
export async function confirmManualCorrection(csrf: string, token: string, expectedVersion: number) {
	return adminMutation<ManualRankingCorrectionChange>('/api/v1/admin/players/corrections/confirm', csrf, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ token, expectedVersion, confirmed: true }) })
}
export async function revokeManualCorrection(csrf: string, playerId: number, correctionId: number, expectedVersion: number, reason: string) {
	return adminMutation<ManualRankingCorrectionRevocationResponse>('/api/v1/admin/players/' + playerId + '/corrections/' + correctionId + '/revoke', csrf, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ expectedVersion, reason, confirmed: true }) })
}
