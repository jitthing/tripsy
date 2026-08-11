import { beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  getSession: vi.fn(),
  fetch: vi.fn(),
}))

vi.mock('./supabase', () => ({
  supabase: { auth: { getSession: mocks.getSession } },
}))

import { api } from './api'

describe('API client', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.stubGlobal('fetch', mocks.fetch)
  })

  it('sends the current session token with API requests', async () => {
    mocks.getSession.mockResolvedValue({ data: { session: { access_token: 'session-token' } } })
    mocks.fetch.mockResolvedValue(new Response(JSON.stringify([]), { status: 200 }))

    await expect(api.listTrips()).resolves.toEqual([])

    expect(mocks.fetch).toHaveBeenCalledWith(expect.stringMatching(/\/v1\/trips$/), {
      headers: { Authorization: 'Bearer session-token' },
    })
  })

  it('does not make an unauthenticated request when the session has expired', async () => {
    mocks.getSession.mockResolvedValue({ data: { session: null } })

    await expect(api.listTrips()).rejects.toThrow('Your session has expired. Please sign in again.')

    expect(mocks.fetch).not.toHaveBeenCalled()
  })

  it('surfaces an API error message', async () => {
    mocks.getSession.mockResolvedValue({ data: { session: { access_token: 'session-token' } } })
    mocks.fetch.mockResolvedValue(new Response(JSON.stringify({ error: 'Unable to load trip details' }), { status: 500 }))

    await expect(api.getTrip('trip-1')).rejects.toThrow('Unable to load trip details')
  })
})
