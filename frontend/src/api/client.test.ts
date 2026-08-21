import { describe, expect, it, vi } from 'vitest'
import { ApiError, confirmManualCorrection, confirmPlayerMergeUndo, getAdminSession, getRankings, listPlayerMerges, previewManualCorrection, previewMerge, previewPlayerMergeUndo, revokeManualCorrection } from './client'

describe('public rankings API', () => {
  it('adds the selected year as a query parameter and keeps the overall URL unchanged', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ items: [], lastSyncAt: null, availableYears: [2025], selectedYear: 2025 }), { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)

    await getRankings(2025)
    await getRankings()

    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/public/rankings?year=2025')
    expect(fetchMock.mock.calls[1][0]).toBe('/api/v1/public/rankings')
  })
})

describe('admin CSRF session flow', () => {
  it('uses the session token and includes the browser cookie on preview', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({ authenticated: true, csrf_token: 'session-token' }), { status: 200 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ token: 'plan-token', result: {} }), { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)

    const session = await getAdminSession()
    await previewMerge(session.csrf_token, 1, 2)

    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(fetchMock.mock.calls[0][1]).toMatchObject({ credentials: 'include' })
    const previewInit = fetchMock.mock.calls[1][1] as RequestInit
    expect(previewInit.credentials).toBe('include')
    expect(new Headers(previewInit.headers).get('X-CSRF-Token')).toBe('session-token')
  })

  it('refreshes the session once for an expired or replaced CSRF cookie', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({ error: 'invalid csrf token' }), { status: 403, headers: { 'Content-Type': 'application/json' } }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ authenticated: true, csrf_token: 'fresh-token' }), { status: 200 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ token: 'plan-token', result: {} }), { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)

    await previewMerge('stale-token', 1, 2)

    expect(fetchMock).toHaveBeenCalledTimes(3)
    const retryInit = fetchMock.mock.calls[2][1] as RequestInit
    expect(retryInit.credentials).toBe('include')
    expect(new Headers(retryInit.headers).get('X-CSRF-Token')).toBe('fresh-token')
  })

  it('does not retry an origin failure', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ error: 'invalid origin' }), { status: 403, headers: { 'Content-Type': 'application/json' } }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(previewMerge('token', 1, 2)).rejects.toMatchObject({ status: 403, message: 'invalid origin' } satisfies Partial<ApiError>)
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })
})

describe('manual ranking corrections API', () => {
  it('keeps the exact effective date and exposes preview/confirm/revoke contracts', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({ token: 'correction-token', player: {}, correction: {}, before: {}, after: {}, expectedVersion: 4 }), { status: 200 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ correction: {}, before: {}, after: {}, version: 5 }), { status: 200 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ correction: {}, before: {}, after: {}, version: 6 }), { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)

    const input = { effectiveDate: '2026-08-20', effectiveYear: 2026, tournamentCountDelta: 1, gamesPlayedDelta: -2, pointsCentsDelta: 125, goalDifferenceDelta: 3, reason: 'Protokollnachtrag', replaceCorrectionId: 9 }
    await previewManualCorrection('csrf', 7, input)
    await confirmManualCorrection('csrf', 'correction-token', 4)
    await revokeManualCorrection('csrf', 7, 9, 5, 'Aufhebung dokumentiert')

    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/admin/players/7/corrections/preview')
    expect(JSON.parse((fetchMock.mock.calls[0][1] as RequestInit).body as string)).toEqual(input)
    expect(fetchMock.mock.calls[1][0]).toBe('/api/v1/admin/players/corrections/confirm')
    expect(fetchMock.mock.calls[2][0]).toBe('/api/v1/admin/players/7/corrections/9/revoke')
    expect(JSON.parse((fetchMock.mock.calls[2][1] as RequestInit).body as string).reason).toBe('Aufhebung dokumentiert')
  })
})

describe('player merge undo API', () => {
  it('uses the history, preview and explicit confirmation contracts', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({ items: [] }), { status: 200 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ token: 'undo-token', preview: {} }), { status: 200 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ merge: {}, sourceAfter: {}, targetAfter: {}, undoneAt: '2026-08-21T09:00:00Z' }), { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)

    await listPlayerMerges()
    await previewPlayerMergeUndo('csrf-token', 12)
    await confirmPlayerMergeUndo('csrf-token', 12, 'undo-token', 'Versehentlich zusammengeführt')

    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/admin/players/merges')
    expect(fetchMock.mock.calls[1][0]).toBe('/api/v1/admin/players/merges/12/undo/preview')
    expect(fetchMock.mock.calls[2][0]).toBe('/api/v1/admin/players/merges/12/undo/confirm')
    const init = fetchMock.mock.calls[2][1] as RequestInit
    expect(new Headers(init.headers).get('X-CSRF-Token')).toBe('csrf-token')
    expect(JSON.parse(init.body as string)).toEqual({ token: 'undo-token', reason: 'Versehentlich zusammengeführt', confirmed: true })
  })
})
