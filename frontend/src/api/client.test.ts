import { describe, expect, it, vi } from 'vitest'
import { ApiError, getAdminSession, previewMerge } from './client'

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
