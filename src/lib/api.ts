import { supabase } from './supabase'

const baseURL = import.meta.env.VITE_API_URL?.replace(/\/$/, '') || 'http://localhost:8080'

export type Trip = { id: string; ownerId: string; title: string; destination: string; startDate: string; endDate: string; coverColor: string; createdAt: string }
export type Plan = { id: string; tripId: string; createdBy: string; kind: PlanKind; title: string; startsAt: string; endsAt?: string; location: string; confirmationCode: string; notes: string }
export type PlanKind = 'flight' | 'stay' | 'activity' | 'transport' | 'food' | 'other'
export type ChecklistItem = { id: string; tripId: string; createdBy: string; title: string; isComplete: boolean; sortOrder: number }
export type Document = { id: string; tripId: string; uploadedBy: string; name: string; storagePath: string; contentType: string; sizeBytes: number; createdAt: string }
export type RouteOptionType = 'direct_flight' | 'flight_train' | 'train' | 'bus' | 'other'
export type RouteOptionStatus = 'considering' | 'shortlisted' | 'booked' | 'dismissed'
export type RouteOption = { id: string; tripId: string; createdBy: string; title: string; routeType: RouteOptionType; origin: string; destination: string; departsAt?: string; arrivesAt?: string; durationMinutes?: number; transfers: number; priceAmount?: number; currency?: string; bookingUrl: string; notes: string; status: RouteOptionStatus; createdAt: string }
export type Member = { id: string; email: string; displayName: string; avatarUrl: string; role: 'owner' | 'member' }
export type ReservationImport = { id: string; tripId?: string; ownerId: string; sender: string; subject: string; receivedAt?: string; status: 'queued' | 'processing' | 'review' | 'approved' | 'discarded' | 'failed'; errorMessage: string; duplicateOfImportId?: string; usedLlm: boolean; createdAt: string; extractionError: string }
export type ReservationDraft = { id: string; importId: string; kind: PlanKind; title: string; supplier: string; confirmationCode: string; startsAt?: string; endsAt?: string; timeZone: string; location: string; notes: string; confidence: number; status: 'pending' | 'approved' | 'discarded' }
export type ImportDetail = { import: ReservationImport; drafts: ReservationDraft[]; attachments: Array<{ id: string; filename: string; contentType: string; sizeBytes: number }> }
export type CalendarStatus = { connected: boolean; email?: string; calendarId?: string; status?: string; lastError?: string; lastSyncedAt?: string }
export type TripDetail = { trip: Trip; plans: Plan[]; checklist: ChecklistItem[]; documents: Document[]; routeOptions: RouteOption[]; members: Member[] }

// Update replaces the whole resource, so create and update take the same shape.
export type TripInput = Omit<Trip, 'id' | 'ownerId' | 'createdAt'>
export type PlanInput = Omit<Plan, 'id' | 'tripId' | 'createdBy'>
export type RouteOptionInput = Omit<RouteOption, 'id' | 'tripId' | 'createdBy' | 'createdAt'>

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  if (!supabase) throw new Error('Supabase is not configured')
  const { data: { session } } = await supabase.auth.getSession()
  if (!session) throw new Error('Your session has expired. Please sign in again.')
  const response = await fetch(`${baseURL}${path}`, {
    ...init,
    headers: { Authorization: `Bearer ${session.access_token}`, ...(init?.body ? { 'Content-Type': 'application/json' } : {}), ...init?.headers },
  })
  if (response.status === 204) return undefined as T
  const body = await response.json().catch(() => ({}))
  if (!response.ok) throw new Error(body.error || 'Something went wrong')
  return body as T
}

export const api = {
  listTrips: () => request<Trip[]>('/v1/trips'),
  listInbox: () => request<ReservationImport[]>('/v1/inbox'),
  getTrip: (tripId: string) => request<TripDetail>(`/v1/trips/${tripId}/`),
  createTrip: (input: TripInput) => request<Trip>('/v1/trips', { method: 'POST', body: JSON.stringify(input) }),
  updateTrip: (tripId: string, input: TripInput) => request<Trip>(`/v1/trips/${tripId}/`, { method: 'PATCH', body: JSON.stringify(input) }),
  deleteTrip: (tripId: string) => request<void>(`/v1/trips/${tripId}/`, { method: 'DELETE' }),
  createPlan: (tripId: string, input: PlanInput) => request<Plan>(`/v1/trips/${tripId}/plans`, { method: 'POST', body: JSON.stringify(input) }),
  updatePlan: (tripId: string, planId: string, input: PlanInput) => request<Plan>(`/v1/trips/${tripId}/plans/${planId}`, { method: 'PATCH', body: JSON.stringify(input) }),
  deletePlan: (tripId: string, planId: string) => request<void>(`/v1/trips/${tripId}/plans/${planId}`, { method: 'DELETE' }),
  updateChecklist: (tripId: string, item: ChecklistItem) => request<ChecklistItem>(`/v1/trips/${tripId}/checklist/${item.id}`, { method: 'PATCH', body: JSON.stringify(item) }),
  createChecklist: (tripId: string, input: Pick<ChecklistItem, 'title' | 'isComplete' | 'sortOrder'>) => request<ChecklistItem>(`/v1/trips/${tripId}/checklist`, { method: 'POST', body: JSON.stringify(input) }),
  deleteChecklist: (tripId: string, itemId: string) => request<void>(`/v1/trips/${tripId}/checklist/${itemId}`, { method: 'DELETE' }),
  addMember: (tripId: string, email: string) => request<Member>(`/v1/trips/${tripId}/members`, { method: 'POST', body: JSON.stringify({ email }) }),
  deleteMember: (tripId: string, userId: string) => request<void>(`/v1/trips/${tripId}/members/${userId}`, { method: 'DELETE' }),
  createDocument: (tripId: string, input: Omit<Document, 'id' | 'tripId' | 'uploadedBy' | 'createdAt'>) => request<Document>(`/v1/trips/${tripId}/documents`, { method: 'POST', body: JSON.stringify(input) }),
  deleteDocument: (tripId: string, documentId: string) => request<void>(`/v1/trips/${tripId}/documents/${documentId}`, { method: 'DELETE' }),
  createRouteOption: (tripId: string, input: RouteOptionInput) => request<RouteOption>(`/v1/trips/${tripId}/route-options`, { method: 'POST', body: JSON.stringify(input) }),
  updateRouteOption: (tripId: string, optionId: string, input: RouteOptionInput) => request<RouteOption>(`/v1/trips/${tripId}/route-options/${optionId}`, { method: 'PATCH', body: JSON.stringify(input) }),
  deleteRouteOption: (tripId: string, optionId: string) => request<void>(`/v1/trips/${tripId}/route-options/${optionId}`, { method: 'DELETE' }),
  getImportAddress: (tripId: string) => request<{ address: string }>(`/v1/trips/${tripId}/import-address`, { method: 'POST' }),
  getInboxAddress: () => request<{ address: string }>('/v1/inbox/address', { method: 'POST' }),
  listImports: (tripId: string) => request<ReservationImport[]>(`/v1/trips/${tripId}/imports`),
  getImport: (importId: string) => request<ImportDetail>(`/v1/imports/${importId}`),
  approveDraft: (importId: string, draft: ReservationDraft) => request<Plan>(`/v1/imports/${importId}/drafts/${draft.id}/approve`, { method: 'POST', body: JSON.stringify({ kind: draft.kind, title: draft.title, startsAt: draft.startsAt, endsAt: draft.endsAt, location: draft.location, confirmationCode: draft.confirmationCode, notes: draft.notes, timeZone: draft.timeZone }) }),
  discardDraft: (importId: string, draftId: string) => request<void>(`/v1/imports/${importId}/drafts/${draftId}/discard`, { method: 'POST' }),
  assignImport: (importId: string, tripId: string) => request<void>(`/v1/imports/${importId}/assign`, { method: 'POST', body: JSON.stringify({ tripId }) }),
  retryImport: (importId: string) => request<void>(`/v1/imports/${importId}/retry`, { method: 'POST' }),
  calendarStatus: () => request<CalendarStatus>('/v1/calendar/status'),
  calendarConnect: () => request<{ url: string }>('/v1/calendar/connect', { method: 'POST' }),
  calendarSync: () => request<{ queued: boolean }>('/v1/calendar/sync', { method: 'POST' }),
  calendarDisconnect: () => request<void>('/v1/calendar', { method: 'DELETE' }),
}
