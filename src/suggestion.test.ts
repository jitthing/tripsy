import { describe, expect, it } from 'vitest'
import { deriveTripSuggestion } from './App'
import type { ReservationDraft } from './lib/api'

function draft(overrides: Partial<ReservationDraft>): ReservationDraft {
  return {
    id: 'draft-1', importId: 'import-1', kind: 'flight', title: '', supplier: '', confirmationCode: '',
    timeZone: 'UTC', location: '', notes: '', confidence: 0.9, status: 'pending', ...overrides,
  }
}

describe('deriveTripSuggestion', () => {
  it('returns nothing for a keyword fallback, which has no dates or place', () => {
    expect(deriveTripSuggestion([draft({ title: 'Your flight to Lisbon', confidence: 0.2 })])).toBeNull()
  })

  it('returns nothing when there are dates but nowhere to go', () => {
    expect(deriveTripSuggestion([draft({ startsAt: '2026-09-10T08:30:00Z', title: 'A booking' })])).toBeNull()
  })

  it('prefers a stay location over a flight departure airport', () => {
    const suggestion = deriveTripSuggestion([
      draft({ id: 'a', kind: 'flight', location: 'Gatwick North', startsAt: '2026-09-10T08:30:00Z' }),
      draft({ id: 'b', kind: 'stay', location: 'Rua Garrett 12, Lisbon', startsAt: '2026-09-10T14:00:00Z' }),
    ])
    // The flight's location is where you leave from, not where the trip is.
    expect(suggestion?.destination).toBe('Lisbon')
  })

  it('falls back to the arrival side of a routed flight title', () => {
    const suggestion = deriveTripSuggestion([
      draft({ kind: 'flight', title: 'BA487 London → Barcelona', startsAt: '2026-09-10T08:30:00Z' }),
    ])
    expect(suggestion?.destination).toBe('Barcelona')
  })

  it('spans the earliest start to the latest end across every draft', () => {
    const suggestion = deriveTripSuggestion([
      draft({ id: 'a', kind: 'stay', location: 'Lisbon', startsAt: '2026-09-10T14:00:00Z', endsAt: '2026-09-13T10:00:00Z' }),
      draft({ id: 'b', kind: 'flight', startsAt: '2026-09-09T08:30:00Z', endsAt: '2026-09-09T11:00:00Z' }),
    ])
    expect(suggestion?.startDate).toBe('2026-09-09')
    expect(suggestion?.endDate).toBe('2026-09-13')
  })

  it('reports the weakest draft as the suggestion confidence', () => {
    const suggestion = deriveTripSuggestion([
      draft({ id: 'a', kind: 'stay', location: 'Lisbon', startsAt: '2026-09-10T14:00:00Z', confidence: 0.95 }),
      draft({ id: 'b', kind: 'flight', startsAt: '2026-09-09T08:30:00Z', confidence: 0.35 }),
    ])
    expect(suggestion?.confidence).toBe(0.35)
  })

  it('ignores discarded drafts', () => {
    const suggestion = deriveTripSuggestion([
      draft({ id: 'a', kind: 'stay', location: 'Lisbon', startsAt: '2026-09-10T14:00:00Z' }),
      draft({ id: 'b', kind: 'stay', location: 'Reykjavik', startsAt: '2027-01-04T14:00:00Z', status: 'discarded' }),
    ])
    expect(suggestion?.destination).toBe('Lisbon')
    expect(suggestion?.endDate).toBe('2026-09-10')
  })
})
