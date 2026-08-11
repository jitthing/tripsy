import { fireEvent, render, screen } from '@testing-library/react'
import type { Session } from '@supabase/supabase-js'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  api: {
    listTrips: vi.fn(),
    getTrip: vi.fn(),
  },
  auth: {
    getSession: vi.fn(),
    onAuthStateChange: vi.fn(),
    signOut: vi.fn(),
  },
}))

vi.mock('./lib/api', () => ({ api: mocks.api }))
vi.mock('./lib/supabase', () => ({
  supabase: { auth: mocks.auth },
  isSupabaseConfigured: () => true,
}))

import App from './App'

const trip = {
  id: 'trip-1', ownerId: 'user-1', title: 'Lisbon weekend', destination: 'Lisbon',
  startDate: '2026-09-10T00:00:00Z', endDate: '2026-09-13T00:00:00Z', coverColor: '#1d4c46', createdAt: '2026-08-01T00:00:00Z',
}

const detail = { trip, plans: [], checklist: [], documents: [], routeOptions: [], members: [] }
const session = { user: { email: 'ari@example.com', user_metadata: { full_name: 'Ari' } } } as unknown as Session

describe('Workspace trip loading', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    mocks.auth.getSession.mockResolvedValue({ data: { session } })
    mocks.auth.onAuthStateChange.mockReturnValue({ data: { subscription: { unsubscribe: vi.fn() } } })
    mocks.api.listTrips.mockResolvedValue([trip])
  })

  it('renders a loaded trip detail', async () => {
    mocks.api.getTrip.mockResolvedValue(detail)

    render(<App />)

    expect(await screen.findByRole('heading', { name: 'Lisbon' })).toBeInTheDocument()
    expect(screen.getByText('Lisbon weekend')).toBeInTheDocument()
  })

  it('shows a trip-detail error instead of the no-trips empty state', async () => {
    mocks.api.getTrip.mockRejectedValueOnce(new Error('Unable to load trip details')).mockResolvedValueOnce(detail)

    render(<App />)

    expect(await screen.findByRole('heading', { name: 'We couldn’t load this trip.' })).toBeInTheDocument()
    expect(screen.getAllByText('Unable to load trip details')).toHaveLength(2)
    expect(screen.queryByText('Start your first shared trip.')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }))
    expect(await screen.findByRole('heading', { name: 'Lisbon' })).toBeInTheDocument()
    expect(mocks.api.getTrip).toHaveBeenCalledTimes(2)
  })

  it('shows a list error instead of claiming there are no trips', async () => {
    mocks.api.listTrips.mockRejectedValue(new Error('Unable to load trips'))

    render(<App />)

    expect(await screen.findByRole('heading', { name: 'We couldn’t load your trips.' })).toBeInTheDocument()
    expect(screen.getAllByText('Unable to load trips')).toHaveLength(2)
    expect(screen.queryByText('Start your first shared trip.')).not.toBeInTheDocument()
  })
})
