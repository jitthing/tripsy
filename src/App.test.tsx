import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import type { Session } from '@supabase/supabase-js'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  api: {
    listTrips: vi.fn(),
    listInbox: vi.fn(),
    getTrip: vi.fn(),
    createPlan: vi.fn(),
    updatePlan: vi.fn(),
    deletePlan: vi.fn(),
    updateTrip: vi.fn(),
    deleteTrip: vi.fn(),
    updateChecklist: vi.fn(),
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

const plan = {
  id: 'plan-1', tripId: 'trip-1', createdBy: 'user-1', kind: 'flight' as const, title: 'Flight to Lisbon',
  startsAt: '2026-09-10T08:30:00Z', endsAt: '2026-09-10T11:00:00Z', location: 'Gatwick North', confirmationCode: 'XK92QP', notes: 'Gate closes 40 minutes early.',
}

const detail = { trip, plans: [], checklist: [], documents: [], routeOptions: [], members: [] }
const detailWithPlan = { ...detail, plans: [plan] }
const session = { user: { email: 'ari@example.com', user_metadata: { full_name: 'Ari' } } } as unknown as Session

// The app reads its route from the URL, so every test starts from a bare path.
beforeEach(() => { window.history.replaceState({}, '', '/') })

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

describe('URL routing', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    window.history.replaceState({}, '', '/')
    mocks.auth.getSession.mockResolvedValue({ data: { session } })
    mocks.auth.onAuthStateChange.mockReturnValue({ data: { subscription: { unsubscribe: vi.fn() } } })
    mocks.api.listTrips.mockResolvedValue([trip])
    mocks.api.getTrip.mockResolvedValue(detailWithPlan)
  })

  it('sends a bare path to the first trip without stacking history', async () => {
    render(<App />)

    expect(await screen.findByRole('heading', { name: 'Lisbon' })).toBeInTheDocument()
    expect(window.location.pathname).toBe('/trip/trip-1')
  })

  it('opens a deep-linked section directly', async () => {
    window.history.replaceState({}, '', '/trip/trip-1/documents')
    render(<App />)

    expect(await screen.findByRole('heading', { name: 'Travel wallet' })).toBeInTheDocument()
    expect(mocks.api.getTrip).toHaveBeenCalledWith('trip-1')
  })

  it('puts the section in the URL and restores it on back', async () => {
    render(<App />)
    await screen.findByRole('heading', { name: 'Lisbon' })

    const tabs = within(screen.getByRole('navigation', { name: 'Trip sections' }))
    fireEvent.click(tabs.getByRole('button', { name: 'Documents' }))
    expect(await screen.findByRole('heading', { name: 'Travel wallet' })).toBeInTheDocument()
    expect(window.location.pathname).toBe('/trip/trip-1/documents')

    window.history.back()
    await waitFor(() => expect(window.location.pathname).toBe('/trip/trip-1'))
    expect(await screen.findByRole('heading', { name: 'What’s next' })).toBeInTheDocument()
  })

  it('routes the inbox at its own path', async () => {
    mocks.api.listInbox.mockResolvedValue([])
    render(<App />)
    await screen.findByRole('heading', { name: 'Lisbon' })

    fireEvent.click(screen.getByRole('button', { name: 'Inbox' }))

    expect(await screen.findByRole('heading', { name: 'Incoming tasks' })).toBeInTheDocument()
    expect(window.location.pathname).toBe('/inbox')
  })

  it('switches trips from the picker sheet and updates the URL', async () => {
    const other = { ...trip, id: 'trip-2', title: 'Osaka autumn', destination: 'Osaka' }
    mocks.api.listTrips.mockResolvedValue([trip, other])
    render(<App />)
    await screen.findByRole('heading', { name: 'Lisbon' })

    fireEvent.click(screen.getByRole('button', { name: 'Switch trip' }))
    fireEvent.click(await screen.findByRole('button', { name: /Osaka autumn/ }))

    await waitFor(() => expect(window.location.pathname).toBe('/trip/trip-2'))
    expect(mocks.api.getTrip).toHaveBeenCalledWith('trip-2')
  })
})

describe('Trip search', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    window.history.replaceState({}, '', '/')
    mocks.auth.getSession.mockResolvedValue({ data: { session } })
    mocks.auth.onAuthStateChange.mockReturnValue({ data: { subscription: { unsubscribe: vi.fn() } } })
    mocks.api.listTrips.mockResolvedValue([trip])
    mocks.api.getTrip.mockResolvedValue(detailWithPlan)
  })

  it('finds a plan by its confirmation code', async () => {
    render(<App />)
    await screen.findByRole('heading', { name: 'Lisbon' })

    fireEvent.click(screen.getByRole('button', { name: 'Search' }))
    fireEvent.change(await screen.findByLabelText('Search this trip'), { target: { value: 'xk92' } })

    expect(await screen.findByText('PLANS · 1')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Flight to Lisbon/ })).toBeInTheDocument()
  })

  it('says so when nothing matches', async () => {
    render(<App />)
    await screen.findByRole('heading', { name: 'Lisbon' })

    fireEvent.click(screen.getByRole('button', { name: 'Search' }))
    fireEvent.change(await screen.findByLabelText('Search this trip'), { target: { value: 'zzzz' } })

    expect(await screen.findByText('Nothing matches “zzzz”')).toBeInTheDocument()
  })
})

describe('Plan detail, edit, and delete', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    mocks.auth.getSession.mockResolvedValue({ data: { session } })
    mocks.auth.onAuthStateChange.mockReturnValue({ data: { subscription: { unsubscribe: vi.fn() } } })
    mocks.api.listTrips.mockResolvedValue([trip])
    mocks.api.getTrip.mockResolvedValue(detailWithPlan)
  })

  async function openPlanDetail() {
    render(<App />)
    fireEvent.click(await screen.findByRole('button', { name: /Flight to Lisbon/ }))
  }

  it('shows notes, confirmation code, and end time that the list view hides', async () => {
    await openPlanDetail()

    expect(await screen.findByText('Gate closes 40 minutes early.')).toBeInTheDocument()
    expect(screen.getByText('XK92QP')).toBeInTheDocument()
    expect(screen.getByText('Confirmation code')).toBeInTheDocument()
    expect(screen.getAllByText('Gatwick North').length).toBeGreaterThan(0)
    expect(screen.getByText('Ends')).toBeInTheDocument()
  })

  it('saves an edited plan through updatePlan', async () => {
    mocks.api.updatePlan.mockResolvedValue(plan)
    await openPlanDetail()

    fireEvent.click(await screen.findByRole('button', { name: /Edit/ }))
    fireEvent.change(screen.getByDisplayValue('Flight to Lisbon'), { target: { value: 'Flight to Porto' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save changes' }))

    await screen.findByText('Plan updated')
    expect(mocks.api.updatePlan).toHaveBeenCalledWith('trip-1', 'plan-1', expect.objectContaining({ title: 'Flight to Porto', notes: 'Gate closes 40 minutes early.' }))
  })

  it('deletes a plan straight away and offers an undo', async () => {
    mocks.api.deletePlan.mockResolvedValue(undefined)
    mocks.api.createPlan.mockResolvedValue(plan)
    await openPlanDetail()

    fireEvent.click(await screen.findByRole('button', { name: /Delete/ }))

    await screen.findByText('Plan deleted')
    expect(mocks.api.deletePlan).toHaveBeenCalledWith('trip-1', 'plan-1')
    expect(screen.queryByRole('button', { name: /Flight to Lisbon/ })).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Undo' }))

    await screen.findByText('Plan restored')
    expect(mocks.api.createPlan).toHaveBeenCalledWith('trip-1', expect.objectContaining({ title: 'Flight to Lisbon', confirmationCode: 'XK92QP' }))
  })

  it('puts the plan back on screen when the delete fails', async () => {
    mocks.api.deletePlan.mockRejectedValue(new Error('Plan is locked by another member'))
    await openPlanDetail()

    fireEvent.click(await screen.findByRole('button', { name: /Delete/ }))

    expect(await screen.findByText('Plan is locked by another member')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Flight to Lisbon/ })).toBeInTheDocument()
  })

  it('still asks before deleting a trip, and backs out cleanly', async () => {
    render(<App />)

    fireEvent.click(await screen.findByRole('button', { name: 'Trip settings' }))
    fireEvent.click(screen.getByRole('button', { name: /Delete this trip/ }))
    expect(await screen.findByRole('heading', { name: 'Delete this trip?' })).toBeInTheDocument()
    expect(mocks.api.deleteTrip).not.toHaveBeenCalled()

    fireEvent.click(screen.getByRole('button', { name: 'Keep it' }))
    expect(screen.queryByRole('heading', { name: 'Delete this trip?' })).not.toBeInTheDocument()
    expect(mocks.api.deleteTrip).not.toHaveBeenCalled()
  })

  it('ticks a checklist item without refetching the trip', async () => {
    const item = { id: 'item-1', tripId: 'trip-1', createdBy: 'user-1', title: 'Renew passport', isComplete: false, sortOrder: 0 }
    mocks.api.getTrip.mockResolvedValue({ ...detailWithPlan, checklist: [item] })
    mocks.api.updateChecklist.mockResolvedValue({ ...item, isComplete: true })
    render(<App />)

    fireEvent.click(await screen.findByRole('button', { name: 'Renew passport' }))

    await screen.findByText('Ticked off')
    expect(mocks.api.updateChecklist).toHaveBeenCalledWith('trip-1', expect.objectContaining({ id: 'item-1', isComplete: true }))
    expect(mocks.api.getTrip).toHaveBeenCalledTimes(1)
  })

  it('sends the chosen cover colour when trip settings are saved', async () => {
    mocks.api.updateTrip.mockResolvedValue(trip)
    render(<App />)

    fireEvent.click(await screen.findByRole('button', { name: 'Trip settings' }))
    fireEvent.click(screen.getByRole('button', { name: 'Use #d5634d' }))
    fireEvent.click(screen.getByRole('button', { name: 'Save changes' }))

    await screen.findByText('Trip updated')
    expect(mocks.api.updateTrip).toHaveBeenCalledWith('trip-1', expect.objectContaining({ title: 'Lisbon weekend', coverColor: '#d5634d' }))
  })
})
