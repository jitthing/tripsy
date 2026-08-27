import { useEffect, useRef, useState, type FormEvent, type ReactNode } from 'react'
import type { Session } from '@supabase/supabase-js'
import {
  CalendarDays, Check, ChevronDown, ChevronLeft, ChevronRight, FileText, FolderOpen, Globe2, Hotel, LoaderCircle,
  Clock3, LogOut, MapPin, MoreHorizontal, PackageCheck, Pencil, Plane, Plus, Route, Search, Settings2,
  ShieldCheck, Ticket, TrainFront, Trash2, TriangleAlert, Upload, UsersRound, X,
} from 'lucide-react'
import { api, type ChecklistItem, type Document, type ImportDetail, type Plan, type PlanInput, type PlanKind, type ReservationDraft, type ReservationImport, type RouteOption, type RouteOptionInput, type RouteOptionStatus, type RouteOptionType, type Trip, type TripDetail, type TripInput } from './lib/api'
import { isSupabaseConfigured, supabase } from './lib/supabase'
import { sectionLabels, sections, useRoute, type Section } from './lib/router'

type TripSuggestion = { title: string; destination: string; startDate: string; endDate: string; confidence: number }

type Sheet =
  | { kind: 'trip'; suggestion?: TripSuggestion; importID?: string }
  | { kind: 'tripSettings' }
  | { kind: 'plan'; plan?: Plan }
  | { kind: 'planDetail'; plan: Plan }
  | { kind: 'route'; option?: RouteOption }
  | { kind: 'switcher' }
  | { kind: 'confirm'; title: string; body: string; confirmLabel: string; run: () => Promise<void> }

const coverColors = ['#1d4c46', '#d5634d', '#3f6f9a', '#8a5fa8', '#b8862c', '#2f7a54']

const kinds: Record<PlanKind, { label: string; icon: typeof Plane; color: string }> = {
  flight: { label: 'Flight', icon: Plane, color: 'orange' },
  stay: { label: 'Stay', icon: Hotel, color: 'rose' },
  activity: { label: 'Activity', icon: Ticket, color: 'blue' },
  transport: { label: 'Transport', icon: TrainFront, color: 'sea' },
  food: { label: 'Food', icon: Ticket, color: 'gold' },
  other: { label: 'Other', icon: CalendarDays, color: 'lilac' },
}

export default function App() {
  const [session, setSession] = useState<Session | null>(null)

  useEffect(() => {
    if (!supabase) return
    supabase.auth.getSession().then(({ data }) => setSession(data.session))
    const { data: listener } = supabase.auth.onAuthStateChange((_event, nextSession) => setSession(nextSession))
    return () => listener.subscription.unsubscribe()
  }, [])

  if (!isSupabaseConfigured()) return <ConfigurationScreen />
  if (!session) return <SignInScreen />
  return <Workspace session={session} />
}

function ConfigurationScreen() {
  return <main className="configuration-screen"><div className="brand-mark">W</div><p className="eyebrow">CONFIGURATION REQUIRED</p><h1>Connect Waypoint to your Supabase project.</h1><p>Add `VITE_SUPABASE_URL`, `VITE_SUPABASE_ANON_KEY`, and `VITE_API_URL` to `.env.local`, then restart the frontend.</p></main>
}

function SignInScreen() {
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  async function signIn() {
    if (!supabase) return
    setLoading(true); setError('')
    const { error } = await supabase.auth.signInWithOAuth({ provider: 'google', options: { redirectTo: window.location.origin } })
    if (error) { setError(error.message); setLoading(false) }
  }
  return <main className="auth-screen"><div className="auth-orbit orbit-one" /><div className="auth-orbit orbit-two" /><section className="auth-card"><div className="brand-lockup"><span className="brand-mark">W</span><span>waypoint</span></div><p className="eyebrow">PRIVATE TRAVEL ORGANISER</p><h1>Travel prepared.<br /><em>Together.</em></h1><p className="auth-copy">A shared home for your plans, confirmations, and everything you need before you go.</p><button className="google-button" onClick={signIn} disabled={loading}>{loading ? <LoaderCircle className="spin" size={19} /> : <span className="google-g">G</span>} Continue with Google</button>{error && <p className="form-error">{error}</p>}<p className="auth-note"><ShieldCheck size={14} /> Private trips, private documents.</p></section></main>
}

function Workspace({ session }: { session: Session }) {
  const [route, navigate] = useRoute()
  const [trips, setTrips] = useState<Trip[]>([])
  const [detail, setDetail] = useState<TripDetail | null>(null)
  const [loading, setLoading] = useState(true)
  const [tripListFailed, setTripListFailed] = useState(false)
  const [error, setError] = useState('')
  const [toast, setToast] = useState<{ message: string; undo?: () => Promise<void> } | null>(null)
  const [sheet, setSheet] = useState<Sheet | null>(null)
  const toastTimer = useRef(0)

  function notify(message: string, undo?: () => Promise<void>) {
    window.clearTimeout(toastTimer.current); setToast({ message, undo })
    toastTimer.current = window.setTimeout(() => setToast(null), undo ? 7000 : 3000)
  }
  useEffect(() => () => window.clearTimeout(toastTimer.current), [])
  async function runUndo() {
    const action = toast?.undo
    window.clearTimeout(toastTimer.current); setToast(null)
    if (!action) return
    try { await action() } catch (err) { setError(message(err)) }
  }
  // The URL owns which trip and section are showing. Inbox and Search sit outside a trip
  // but still act on the one you came from, so the last trip id survives those routes.
  const [lastTripID, setLastTripID] = useState('')
  useEffect(() => { if (route.name === 'trip') setLastTripID(route.tripId) }, [route])
  const activeTripID = route.name === 'trip' ? route.tripId : lastTripID
  const section = route.name === 'trip' ? route.section : 'overview'
  function openTrip(tripId: string, next: Section = 'overview', replace = false) { navigate({ name: 'trip', tripId, section: next }, replace) }
  function openSection(next: Section) { if (activeTripID) openTrip(activeTripID, next) }

  async function loadTrips() {
    setLoading(true); setTripListFailed(false); setError('')
    try {
      const nextTrips = await api.listTrips(); setTrips(nextTrips)
      return nextTrips
    } catch (err) { setError(message(err)); setTripListFailed(true); return [] } finally { setLoading(false) }
  }
  // A quiet reload refreshes in place, so an undo doesn't blank the screen back to skeletons.
  async function loadDetail(tripID: string, quiet = false) {
    if (!tripID) { setDetail(null); return }
    if (!quiet) setDetail(null)
    setError('')
    try { setDetail(await api.getTrip(tripID)) } catch (err) { setError(message(err)) }
  }
  useEffect(() => {
    loadTrips().then((nextTrips) => {
      // A bare "/" — a fresh visit or an OAuth return — lands on the first trip without adding history.
      if (route.name === 'home' && nextTrips[0]) openTrip(nextTrips[0].id, 'overview', true)
    })
  }, [])
  useEffect(() => { loadDetail(activeTripID) }, [activeTripID])

  // importID is set when the trip was suggested from a forwarded email, so the
  // email is filed against the trip it just created.
  async function createTrip(input: TripInput, importID?: string) {
    const trip = await api.createTrip(input)
    if (importID) await api.assignImport(importID, trip.id)
    setSheet(null); await loadTrips(); openTrip(trip.id)
    notify(importID ? 'Trip created and the email filed against it' : 'Trip created')
  }
  async function saveTrip(input: TripInput) {
    if (!activeTripID) return
    await api.updateTrip(activeTripID, input); setSheet(null); await loadTrips(); await loadDetail(activeTripID, true); notify('Trip updated')
  }
  // Apply the change locally first, then reconcile with the server; restore the snapshot if it rejects.
  async function mutate(update: (current: TripDetail) => TripDetail, action: () => Promise<unknown>, done: string, undo?: () => Promise<void>) {
    const snapshot = detail
    setDetail((current) => current ? update(current) : current)
    try { await action(); notify(done, undo) } catch (err) { setDetail(snapshot); setError(message(err)) }
  }

  async function savePlan(input: PlanInput, planID?: string) {
    if (!activeTripID) return
    const saved = planID ? await api.updatePlan(activeTripID, planID, input) : await api.createPlan(activeTripID, input)
    setSheet(null)
    setDetail((current) => current ? { ...current, plans: sortPlans(planID ? current.plans.map((entry) => entry.id === planID ? saved : entry) : [...current.plans, saved]) } : current)
    notify(planID ? 'Plan updated' : 'Plan added')
  }
  async function saveRouteOption(input: RouteOptionInput, optionID?: string) {
    if (!activeTripID) return
    const saved = optionID ? await api.updateRouteOption(activeTripID, optionID, input) : await api.createRouteOption(activeTripID, input)
    setSheet(null)
    setDetail((current) => current ? { ...current, routeOptions: sortRouteOptions(optionID ? current.routeOptions.map((entry) => entry.id === optionID ? saved : entry) : [...current.routeOptions, saved]) } : current)
    notify(optionID ? 'Route option updated' : 'Route option saved')
  }
  async function toggleChecklist(item: ChecklistItem) {
    if (!activeTripID) return
    const next = { ...item, isComplete: !item.isComplete }
    await mutate((current) => ({ ...current, checklist: current.checklist.map((entry) => entry.id === item.id ? next : entry) }),
      () => api.updateChecklist(activeTripID, next), next.isComplete ? 'Ticked off' : 'Unticked')
  }
  async function addChecklist(title: string) {
    if (!activeTripID || !detail) return
    const saved = await api.createChecklist(activeTripID, { title, isComplete: false, sortOrder: detail.checklist.length })
    setDetail((current) => current ? { ...current, checklist: [...current.checklist, saved] } : current)
    notify('Reminder added')
  }

  // Deletes that can be rebuilt from data we still hold offer undo; the rest ask first.
  function confirmThen(title: string, body: string, confirmLabel: string, run: () => Promise<void>) {
    setSheet({ kind: 'confirm', title, body, confirmLabel, run })
  }
  const removePlan = (plan: Plan) => { setSheet(null); return mutate(
    (current) => ({ ...current, plans: current.plans.filter((entry) => entry.id !== plan.id) }),
    () => api.deletePlan(activeTripID, plan.id), 'Plan deleted',
    async () => { await api.createPlan(activeTripID, planInput(plan)); await loadDetail(activeTripID, true); notify('Plan restored') }) }
  const removeRouteOption = (option: RouteOption) => mutate(
    (current) => ({ ...current, routeOptions: current.routeOptions.filter((entry) => entry.id !== option.id) }),
    () => api.deleteRouteOption(activeTripID, option.id), 'Route option deleted',
    async () => { await api.createRouteOption(activeTripID, routeOptionInput(option)); await loadDetail(activeTripID, true); notify('Route option restored') })
  const removeChecklist = (item: ChecklistItem) => mutate(
    (current) => ({ ...current, checklist: current.checklist.filter((entry) => entry.id !== item.id) }),
    () => api.deleteChecklist(activeTripID, item.id), 'Reminder removed',
    async () => { await api.createChecklist(activeTripID, { title: item.title, isComplete: item.isComplete, sortOrder: item.sortOrder }); await loadDetail(activeTripID, true); notify('Reminder restored') })
  // The storage object goes with it, so there is nothing left to rebuild from.
  const removeDocument = (doc: Document) => confirmThen('Delete this document?', `“${doc.name}” will be permanently removed from your travel wallet. This can’t be undone.`, 'Delete document', async () => {
    setSheet(null)
    await mutate((current) => ({ ...current, documents: current.documents.filter((entry) => entry.id !== doc.id) }), async () => {
      await api.deleteDocument(activeTripID, doc.id)
      await supabase?.storage.from('trip-documents').remove([doc.storagePath])
    }, 'Document deleted')
  })
  const removeMember = (member: TripDetail['members'][number]) => confirmThen('Remove this travel mate?', `${member.displayName || member.email} will lose access to this trip.`, 'Remove', async () => {
    setSheet(null)
    await mutate((current) => ({ ...current, members: current.members.filter((entry) => entry.id !== member.id) }),
      () => api.deleteMember(activeTripID, member.id), 'Travel mate removed')
  })
  const removeTrip = (trip: Trip) => confirmThen('Delete this trip?', `“${trip.title}” and everything in it — plans, documents, and route options — will be permanently deleted. This can’t be undone.`, 'Delete trip', async () => {
    setSheet(null)
    try {
      await api.deleteTrip(trip.id); setDetail(null)
      const remaining = await loadTrips()
      if (remaining[0]) openTrip(remaining[0].id, 'overview', true); else navigate({ name: 'home' }, true)
      notify('Trip deleted')
    } catch (err) { setError(message(err)) }
  })

  const profileName = session.user.user_metadata.full_name || session.user.email?.split('@')[0] || 'Traveller'
  if (loading && !detail && !tripListFailed) return <WorkspaceSkeleton />
  return <main className="app-shell production-shell">
    <header className="topbar"><div className="avatar">{initials(profileName)}</div><button className="trip-picker" onClick={() => setSheet({ kind: 'switcher' })} aria-label="Switch trip"><Globe2 size={15} /><span>{detail?.trip.title || 'Waypoint'}</span><ChevronDown size={15} /></button><div className="topbar-actions">{detail && <button className="icon-button" onClick={() => setSheet({ kind: 'tripSettings' })} aria-label="Trip settings"><Settings2 size={18} /></button>}<button className="icon-button" onClick={() => supabase?.auth.signOut()} aria-label="Sign out"><LogOut size={18} /></button></div></header>
    {error && <div className="error-banner">{error}<button onClick={() => setError('')} aria-label="Dismiss error"><X size={15} /></button></div>}
    {route.name === 'inbox' ? <section className="area-view"><Inbox trips={trips} onOpenTrip={(tripId) => openTrip(tripId, 'imports')} onSuggestTrip={(suggestion, importID) => setSheet({ kind: 'trip', suggestion, importID })} onReload={() => loadDetail(activeTripID, true)} notify={notify} /></section>
      : route.name === 'search' ? <section className="area-view"><SearchView detail={detail} onSelectPlan={(plan) => setSheet({ kind: 'planDetail', plan })} onOpenSection={openSection} /></section>
      : tripListFailed ? <TripListState error={error} onRetry={() => loadTrips()} /> : !activeTripID ? <EmptyTrips onCreate={() => setSheet({ kind: 'trip' })} /> : !detail ? (error ? <TripDetailState error={error} onRetry={() => loadDetail(activeTripID)} /> : <TripSkeleton />) : <>
      <TripHeader trip={detail.trip} checklist={detail.checklist} />
      <nav className="section-tabs" aria-label="Trip sections">{sections.map((value) => <button key={value} className={section === value ? 'active' : ''} onClick={() => openSection(value)} aria-current={section === value ? 'page' : undefined}>{sectionLabels[value]}</button>)}</nav>
      <section className={section === 'overview' ? 'content' : 'content on-cream'}>{section === 'overview' && <Overview detail={detail} onToggle={toggleChecklist} onAddChecklist={addChecklist} onRemoveChecklist={removeChecklist} onViewChange={openSection} onSelectPlan={(plan) => setSheet({ kind: 'planDetail', plan })} />}{section === 'plans' && <Plans detail={detail} onAdd={() => setSheet({ kind: 'plan' })} onSelectPlan={(plan) => setSheet({ kind: 'planDetail', plan })} />}{section === 'calendar' && <CalendarView plans={detail.plans} />}{section === 'imports' && <Imports tripID={activeTripID} onReload={() => loadDetail(activeTripID, true)} notify={notify} />}{section === 'routes' && <RouteOptions options={detail.routeOptions} onAdd={() => setSheet({ kind: 'route' })} onEdit={(option) => setSheet({ kind: 'route', option })} onDelete={removeRouteOption} />}{section === 'documents' && <Documents detail={detail} onReload={() => loadDetail(activeTripID)} onDelete={removeDocument} notify={notify} />}{section === 'members' && <Members detail={detail} tripID={activeTripID} onReload={() => loadDetail(activeTripID)} onRemove={removeMember} notify={notify} />}</section>
      <button className="fab" onClick={() => setSheet({ kind: section === 'routes' ? 'route' : 'plan' })} aria-label={section === 'routes' ? 'Add a route option' : 'Add a plan'}><Plus size={25} /></button>
    </>}
    <nav className="bottom-nav" aria-label="Areas"><button className={route.name === 'trip' || route.name === 'home' ? 'active' : ''} onClick={() => activeTripID ? openSection('overview') : trips[0] && openTrip(trips[0].id)}><Globe2 size={22} /><span>Trip</span></button><button className={route.name === 'search' ? 'active' : ''} onClick={() => navigate({ name: 'search' })}><Search size={22} /><span>Search</span></button><button className={route.name === 'inbox' ? 'active' : ''} onClick={() => navigate({ name: 'inbox' })}><PackageCheck size={22} /><span>Inbox</span></button></nav>
    {sheet?.kind === 'trip' && <TripFormSheet suggestion={sheet.suggestion} onClose={() => setSheet(null)} onSubmit={(input) => createTrip(input, sheet.importID)} />}
    {sheet?.kind === 'tripSettings' && detail && <TripFormSheet trip={detail.trip} onClose={() => setSheet(null)} onSubmit={saveTrip} onDelete={() => removeTrip(detail.trip)} />}
    {sheet?.kind === 'plan' && <PlanFormSheet plan={sheet.plan} onClose={() => setSheet(null)} onSubmit={savePlan} />}
    {sheet?.kind === 'planDetail' && <PlanDetailSheet plan={sheet.plan} onClose={() => setSheet(null)} onEdit={() => setSheet({ kind: 'plan', plan: sheet.plan })} onDelete={() => removePlan(sheet.plan)} />}
    {sheet?.kind === 'route' && <RouteFormSheet option={sheet.option} onClose={() => setSheet(null)} onSubmit={saveRouteOption} />}
    {sheet?.kind === 'switcher' && <TripSwitcherSheet trips={trips} activeTripID={activeTripID} onClose={() => setSheet(null)} onSelect={(tripId) => { setSheet(null); openTrip(tripId) }} onCreate={() => setSheet({ kind: 'trip' })} />}
    {sheet?.kind === 'confirm' && <ConfirmSheet title={sheet.title} body={sheet.body} confirmLabel={sheet.confirmLabel} onConfirm={sheet.run} onClose={() => setSheet(null)} />}
    {toast && <div className="toast"><Check size={16} /> <span>{toast.message}</span>{toast.undo && <button onClick={runUndo}>Undo</button>}</div>}
  </main>
}

function TripHeader({ trip, checklist }: { trip: Trip; checklist: ChecklistItem[] }) {
  const complete = checklist.filter((item) => item.isComplete).length
  return <section className="trip-summary"><div className="summary-copy"><p className="eyebrow">ACTIVE TRIP</p><h1>{trip.destination}</h1><p className="trip-dates"><CalendarDays size={15} /> {formatDateRange(trip.startDate, trip.endDate)} <span>·</span> {nights(trip.startDate, trip.endDate)} nights</p></div><div className="stamp"><svg viewBox="0 0 42 42" aria-hidden="true"><circle className="stamp-track" cx="21" cy="21" r="17" /><circle className="stamp-value" cx="21" cy="21" r="17" pathLength="100" style={{ strokeDasharray: `${checklist.length ? complete / checklist.length * 100 : 0} 100` }} /></svg><strong>{complete}/{checklist.length}</strong><small>ready</small></div></section>
}

function TripDetailState({ error, onRetry }: { error: string; onRetry: () => void }) {
  return <section className="empty-trips"><span className="brand-mark">W</span><p className="eyebrow">TRIP DETAILS</p><h1>We couldn’t load this trip.</h1><p>{error}</p><button className="save-button" onClick={onRetry}>Try again</button></section>
}

function Shimmer({ className }: { className?: string }) { return <span className={`shimmer${className ? ` ${className}` : ''}`} aria-hidden="true" /> }

// Mirrors the overview layout so content lands where the placeholder sat.
function TripSkeleton() {
  return <div className="skeleton" role="status" aria-label="Loading your trip">
    <Shimmer className="sk-hero" />
    <div className="sk-quick">{[0, 1, 2, 3].map((key) => <Shimmer key={key} className="sk-quick-item" />)}</div>
    <Shimmer className="sk-heading" />
    <div className="sk-timeline">{[0, 1, 2].map((key) => <div className="sk-row" key={key}><Shimmer className="sk-time" /><Shimmer className="sk-dot" /><Shimmer className="sk-copy" /></div>)}</div>
    <Shimmer className="sk-card" />
  </div>
}

function WorkspaceSkeleton() {
  return <main className="app-shell production-shell">
    <header className="topbar"><Shimmer className="sk-avatar" /><Shimmer className="sk-picker" /><Shimmer className="sk-avatar" /></header>
    <section className="trip-summary"><div className="summary-copy"><Shimmer className="sk-eyebrow" /><Shimmer className="sk-title" /><Shimmer className="sk-dates" /></div><Shimmer className="sk-stamp" /></section>
    <div className="section-tabs sk-tabs">{[0, 1, 2, 3].map((key) => <Shimmer key={key} className="sk-tab" />)}</div>
    <section className="content"><TripSkeleton /></section>
  </main>
}

function TripListState({ error, onRetry }: { error: string; onRetry: () => void }) {
  return <section className="empty-trips"><span className="brand-mark">W</span><p className="eyebrow">YOUR TRAVEL SPACE</p><h1>We couldn’t load your trips.</h1><p>{error}</p><button className="save-button" onClick={onRetry}>Try again</button></section>
}

function Overview({ detail, onToggle, onAddChecklist, onRemoveChecklist, onViewChange, onSelectPlan }: { detail: TripDetail; onToggle: (item: ChecklistItem) => void; onAddChecklist: (title: string) => Promise<void>; onRemoveChecklist: (item: ChecklistItem) => void; onViewChange: (view: Section) => void; onSelectPlan: (plan: Plan) => void }) {
  const next = detail.plans.find((plan) => new Date(plan.startsAt) >= new Date()) || detail.plans[0]
  return <>{next ? <section className="hero-card"><div className="hero-card-top"><span className="live-pill"><i /> NEXT PLAN</span><span>{formatDay(next.startsAt)}</span></div><div className="hero-time"><strong>{next.title}</strong><span>{formatTime(next.startsAt)}</span></div><div className="hero-detail"><span className={`timeline-dot ${kinds[next.kind].color}`}>{iconFor(next.kind, 18)}</span><div><b>{kinds[next.kind].label}</b><small>{next.location || 'Location to be added'}</small></div></div><div className="airline-row"><span>{next.confirmationCode ? `Confirmation · ${next.confirmationCode}` : 'No confirmation code added'}</span><button onClick={() => onViewChange('plans')}>All plans <ChevronDown size={14} /></button></div></section> : <section className="empty-card"><CalendarDays size={25} /><b>No plans yet</b><p>Start with the thing you cannot afford to miss.</p><button onClick={() => onViewChange('plans')}>Open plans</button></section>}
  <section className="quick-actions"><button onClick={() => onViewChange('documents')}><span className="quick-icon coral"><FolderOpen size={20} /></span>Documents</button><button onClick={() => onViewChange('plans')}><span className="quick-icon sea"><CalendarDays size={20} /></span>Itinerary</button><button onClick={() => onViewChange('members')}><span className="quick-icon sun"><UsersRound size={20} /></span>Travel mates</button><button onClick={() => onViewChange('plans')}><span className="quick-icon lilac"><MapPin size={20} /></span>Places</button></section>
  <section className="section-heading"><div><p className="eyebrow">ITINERARY</p><h2>What’s next</h2></div><button className="text-button" onClick={() => onViewChange('plans')}>All plans <ChevronDown size={15} /></button></section><Timeline plans={detail.plans.slice(0, 3)} onSelect={onSelectPlan} />
  <Checklist checklist={detail.checklist} onToggle={onToggle} onAdd={onAddChecklist} onRemove={onRemoveChecklist} />
  </>
}

function Timeline({ plans, onSelect }: { plans: Plan[]; onSelect: (plan: Plan) => void }) {
  if (!plans.length) return null
  return <section className="timeline">{plans.map((plan, index) => <button className="timeline-item" key={plan.id} onClick={() => onSelect(plan)}><time>{formatTime(plan.startsAt)}<small>{formatDay(plan.startsAt)}</small></time><span className={`timeline-dot ${kinds[plan.kind].color}`}>{iconFor(plan.kind, 17)}</span><span className="timeline-copy"><b>{plan.title}</b><small>{plan.location || kinds[plan.kind].label}</small></span><span className="chevron-side"><ChevronDown size={15} /></span>{index < plans.length - 1 && <i className="timeline-line" />}</button>)}</section>
}

function Checklist({ checklist, onToggle, onAdd, onRemove }: { checklist: ChecklistItem[]; onToggle: (item: ChecklistItem) => void; onAdd: (title: string) => Promise<void>; onRemove: (item: ChecklistItem) => void }) {
  const [newItem, setNewItem] = useState(''); const [saving, setSaving] = useState(false)
  const complete = checklist.filter((item) => item.isComplete).length
  async function submit(event: FormEvent) { event.preventDefault(); if (!newItem.trim()) return; setSaving(true); try { await onAdd(newItem.trim()); setNewItem('') } finally { setSaving(false) } }
  return <section className="readiness-card"><div className="readiness-head"><div><p className="eyebrow">TRIP READINESS</p><h2>Before you go</h2></div><span>{complete} of {checklist.length}</span></div><div className="progress"><i style={{ width: `${checklist.length ? complete / checklist.length * 100 : 0}%` }} /></div><div className="checklist-list">{checklist.map((item) => <div className="checklist-row" key={item.id}><button onClick={() => onToggle(item)} className={item.isComplete ? 'checked' : ''}><span>{item.isComplete && <Check size={13} />}</span>{item.title}</button><button className="row-delete" onClick={() => onRemove(item)} aria-label={`Remove ${item.title}`}><Trash2 size={14} /></button></div>)}</div><form className="inline-form" onSubmit={submit}><input value={newItem} onChange={(event) => setNewItem(event.target.value)} placeholder="Add a reminder" /><button type="submit" disabled={!newItem.trim() || saving} aria-label="Add checklist item"><Plus size={16} /></button></form></section>
}

function Plans({ detail, onAdd, onSelectPlan }: { detail: TripDetail; onAdd: () => void; onSelectPlan: (plan: Plan) => void }) {
  const days = groupByDay(detail.plans)
  const [activeDay, setActiveDay] = useState('')
  // An empty selection means "all days"; a day that loses its last plan falls back to that.
  const selected = days.some((day) => day.key === activeDay) ? activeDay : ''
  const shown = selected ? days.filter((day) => day.key === selected) : days
  return <><section className="plans-title"><div><p className="eyebrow">YOUR SCHEDULE</p><h2>{detail.trip.title}</h2></div><button className="round-add" onClick={onAdd} aria-label="Add a plan"><Plus size={19} /></button></section>
    {days.length > 1 && <div className="day-strip" role="group" aria-label="Filter by day">
      <button className={selected === '' ? 'active' : ''} onClick={() => setActiveDay('')}><small>ALL</small><b>{detail.plans.length}</b></button>
      {days.map((day) => <button key={day.key} className={selected === day.key ? 'active' : ''} onClick={() => setActiveDay(day.key)} aria-pressed={selected === day.key}><small>{weekday(day.key)}</small><b>{new Date(day.key).getDate()}</b></button>)}
    </div>}
    {detail.plans.length ? shown.map((day) => <section className="day-group" key={day.key}><p className="group-label">{formatWeekday(day.key)}</p><Timeline plans={day.plans} onSelect={onSelectPlan} /></section>)
      : <EmptyPanel icon={<CalendarDays />} title="No plans yet" description="Add your transport, stays, bookings, and things you want to do." />}</>
}

function SearchView({ detail, onSelectPlan, onOpenSection }: { detail: TripDetail | null; onSelectPlan: (plan: Plan) => void; onOpenSection: (section: Section) => void }) {
  const [query, setQuery] = useState('')
  const term = query.trim().toLowerCase()
  const matches = (...fields: (string | undefined)[]) => fields.some((field) => field?.toLowerCase().includes(term))
  const plans = term && detail ? detail.plans.filter((plan) => matches(plan.title, plan.location, plan.confirmationCode, plan.notes)) : []
  const documents = term && detail ? detail.documents.filter((doc) => matches(doc.name)) : []
  const options = term && detail ? detail.routeOptions.filter((option) => matches(option.title, option.origin, option.destination, option.notes)) : []
  const total = plans.length + documents.length + options.length
  return <><section className="plans-title"><div><p className="eyebrow">{detail ? detail.trip.title.toUpperCase() : 'NO TRIP SELECTED'}</p><h2>Search this trip</h2></div><Search className="shield" size={25} /></section>
    <label className="search-field"><Search size={16} /><input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Plans, documents, route options…" aria-label="Search this trip" autoFocus />{query && <button onClick={() => setQuery('')} aria-label="Clear search"><X size={15} /></button>}</label>
    {!detail ? <EmptyPanel icon={<Globe2 />} title="Pick a trip first" description="Search looks inside the trip you have open." />
      : !term ? <EmptyPanel icon={<Search />} title="Search this trip" description="Find a plan, a stored document, or a route option by name, place, or confirmation code." />
      : !total ? <EmptyPanel icon={<Search />} title={`Nothing matches “${query.trim()}”`} description="Try a place, a confirmation code, or part of a title." />
      : <>
        {plans.length > 0 && <><p className="group-label">PLANS · {plans.length}</p><Timeline plans={plans} onSelect={onSelectPlan} /></>}
        {options.length > 0 && <><p className="group-label">ROUTE OPTIONS · {options.length}</p><section className="import-list">{options.map((option) => <button key={option.id} onClick={() => onOpenSection('routes')}><span className="doc-icon sea"><Route size={18} /></span><span><b>{option.title}</b><small>{option.origin || 'Origin'} → {option.destination || 'Destination'}</small></span><em>{routeStatusLabels[option.status]}</em></button>)}</section></>}
        {documents.length > 0 && <><p className="group-label">DOCUMENTS · {documents.length}</p><section className="import-list">{documents.map((doc) => <button key={doc.id} onClick={() => onOpenSection('documents')}><span className="doc-icon blue"><FileText size={18} /></span><span><b>{doc.name}</b><small>{formatBytes(doc.sizeBytes)} · {formatDay(doc.createdAt)}</small></span><em>Open</em></button>)}</section></>}
      </>}
  </>
}

const routeLabels: Record<RouteOptionType, string> ={ direct_flight: 'Direct flight', flight_train: 'Flight + train', train: 'Train', bus: 'Bus', other: 'Other' }
const routeStatusLabels: Record<RouteOptionStatus, string> = { considering: 'Considering', shortlisted: 'Shortlisted', booked: 'Booked', dismissed: 'Dismissed' }

function RouteOptions({ options, onAdd, onEdit, onDelete }: { options: RouteOption[]; onAdd: () => void; onEdit: (option: RouteOption) => void; onDelete: (option: RouteOption) => void }) {
  const usablePrices = options.filter((option) => option.priceAmount !== undefined)
  const cheapest = usablePrices.length ? usablePrices.reduce((lowest, option) => option.priceAmount! < lowest.priceAmount! ? option : lowest) : undefined
  const usableDurations = options.filter((option) => option.durationMinutes !== undefined)
  const fastest = usableDurations.length ? usableDurations.reduce((shortest, option) => option.durationMinutes! < shortest.durationMinutes! ? option : shortest) : undefined
  return <><section className="plans-title"><div><p className="eyebrow">YOUR RESEARCH</p><h2>Route options</h2></div><button className="round-add" onClick={onAdd} aria-label="Add route option"><Plus size={19} /></button></section><p className="route-intro">Save the routes you find, then compare trade-offs before booking. Nothing is searched or booked here.</p>{options.length ? <><section className="route-insights">{cheapest && <div><span>LOWEST PRICE</span><b>{formatPrice(cheapest)}</b><small>{cheapest.title}</small></div>}{fastest && <div><span>FASTEST</span><b>{formatDuration(fastest.durationMinutes)}</b><small>{fastest.title}</small></div>}</section><section className="route-list">{options.map((option) => <article className={`route-card ${option.status}`} key={option.id}><div className="route-card-head"><span className="route-type"><Route size={15} /> {routeLabels[option.routeType]}</span><span className={`route-status ${option.status}`}>{routeStatusLabels[option.status]}</span></div><h3>{option.title}</h3>{option.origin || option.destination ? <p className="route-place"><MapPin size={13} /> {option.origin || 'Origin'} <i /> {option.destination || 'Destination'}</p> : null}<div className="route-metrics"><span>{option.priceAmount !== undefined ? formatPrice(option) : 'Price unknown'}<small>price</small></span><span>{formatDuration(option.durationMinutes)}<small>duration</small></span><span>{option.transfers === 0 ? 'Direct' : `${option.transfers} transfer${option.transfers === 1 ? '' : 's'}`}<small>changes</small></span></div>{option.notes && <p className="route-notes">{option.notes}</p>}<div className="route-card-foot">{option.bookingUrl ? <a href={option.bookingUrl} target="_blank" rel="noreferrer">Open saved link</a> : <span />}<div className="row-actions"><button onClick={() => onEdit(option)} aria-label={`Edit ${option.title}`}><Pencil size={14} /></button><button className="danger" onClick={() => onDelete(option)} aria-label={`Delete ${option.title}`}><Trash2 size={14} /></button></div></div></article>)}</section></> : <EmptyPanel icon={<Route />} title="No routes compared yet" description="Add the options you find: direct flight, flight plus train, train-only, or anything else." />}</>
}

function Inbox({ trips, onOpenTrip, onSuggestTrip, onReload, notify }: { trips: Trip[]; onOpenTrip: (tripId: string) => void; onSuggestTrip: (suggestion: TripSuggestion, importID: string) => void; onReload: () => Promise<void>; notify: (message: string) => void }) {
  const [items, setItems] = useState<ReservationImport[]>([])
  const [selected, setSelected] = useState<ImportDetail | null>(null)
  const [address, setAddress] = useState('')
  const [working, setWorking] = useState(false)
  const [error, setError] = useState('')
  async function refresh() { try { setItems(await api.listInbox()) } catch (err) { setError(message(err)) } }
  useEffect(() => { refresh() }, [])
  // Each person has their own address, so it is safe to show on load rather than mint on demand.
  useEffect(() => { api.getInboxAddress().then((result) => setAddress(result.address)).catch(() => setAddress('')) }, [])
  // Forwarding then opening the Inbox is the common path, so run the worker against
  // this user's queue now instead of waiting for the schedule. Rounds are capped:
  // the scheduled worker is the guarantee, this is only the head start.
  useEffect(() => {
    let live = true
    let timer = 0
    async function pass(round: number) {
      if (!live) return
      setWorking(true)
      try {
        const result = await api.processInbox()
        if (!live) return
        await refresh()
        if (result.pending > 0 && round < 4) timer = window.setTimeout(() => pass(round + 1), 4000)
      } catch { /* the scheduled worker still picks this up */ } finally { if (live) setWorking(false) }
    }
    pass(0)
    return () => { live = false; window.clearTimeout(timer) }
  }, [])
  // Processing happens on a worker, so poll while anything is still in flight.
  const active = items.some((item) => item.status === 'queued' || item.status === 'processing')
  useEffect(() => { if (!active) return; const id = window.setInterval(refresh, 3000); return () => window.clearInterval(id) }, [active])
  async function open(id: string) { try { setSelected(await api.getImport(id)) } catch (err) { setError(message(err)) } }
  async function assign(tripID: string) { if (!selected) return; try { await api.assignImport(selected.import.id, tripID); setSelected(await api.getImport(selected.import.id)); await refresh(); notify('Pending task assigned to trip') } catch (err) { setError(message(err)) } }
  async function retry(id: string) { try { await api.retryImport(id); await refresh(); if (selected?.import.id === id) setSelected(await api.getImport(id)); notify('Extraction queued for retry') } catch (err) { setError(message(err)) } }
  async function approve(draft: ReservationDraft) {
    if (!selected || !draft.startsAt) { setError('This draft has no start time. Open it in the trip to add one before approving.'); return }
    try { await api.approveDraft(selected.import.id, draft); setSelected(await api.getImport(selected.import.id)); await refresh(); await onReload(); notify('Reservation added to your itinerary') } catch (err) { setError(message(err)) }
  }
  async function discard(draft: ReservationDraft) {
    if (!selected) return
    try { await api.discardDraft(selected.import.id, draft.id); setSelected(await api.getImport(selected.import.id)); await refresh(); notify('Draft discarded') } catch (err) { setError(message(err)) }
  }
  const suggestion = selected && !selected.import.tripId ? deriveTripSuggestion(selected.drafts) : null
  return <><section className="plans-title"><div><p className="eyebrow">FORWARD & REVIEW</p><h2>Incoming tasks</h2></div><PackageCheck className="shield" size={25} /></section>{working && <p className="inbox-working"><LoaderCircle className="spin" size={14} /> Checking for new reservations…</p>}<p className="route-intro">Forward a confirmation to your own address below. Review and assign it before it changes any trip.</p>{address && <button className="forwarding-address" onClick={() => { navigator.clipboard?.writeText(address); notify('Forwarding address copied') }}><span>YOUR FORWARDING ADDRESS</span><b>{address}</b><small>Tap to copy · only you receive mail sent here</small></button>}{error && <p className="form-error">{error}</p>}{selected ? <section className="import-detail"><button className="text-button" onClick={() => setSelected(null)}>‹ All tasks</button><p className="eyebrow">{selected.import.sender}</p><h2>{selected.import.subject || 'Forwarded reservation'}</h2><ImportStatus item={selected.import} onRetry={retry} />{selected.import.extractionError && <ExtractionNotice reason={selected.import.extractionError} />}{selected.import.duplicateOfImportId && <p className="route-intro">This looks similar to an earlier booking. Compare both before approving.</p>}{selected.import.tripId && <button className="text-button" onClick={() => onOpenTrip(selected.import.tripId!)}>Open in trip <ChevronDown size={14} /></button>}<label className="form-label">ASSIGN TO A TRIP<select value={selected.import.tripId || ''} onChange={(event) => event.target.value && assign(event.target.value)}><option value="">Choose a trip…</option>{trips.map((trip) => <option key={trip.id} value={trip.id}>{trip.title} · {trip.destination}</option>)}</select></label>{suggestion && <SuggestedTrip suggestion={suggestion} onCreate={() => onSuggestTrip(suggestion, selected.import.id)} />}{selected.drafts.map((draft) => <article className="import-draft" key={draft.id}><span className={`timeline-dot ${kinds[draft.kind].color}`}>{iconFor(draft.kind, 16)}</span><div><b>{draft.title}</b><small>{draft.supplier || 'Reservation proposal'} · {Math.round(draft.confidence * 100)}% confidence</small><p>{draft.startsAt ? `${formatDay(draft.startsAt)} ${formatTime(draft.startsAt)}` : 'Time needs review'}{draft.location ? ` · ${draft.location}` : ''}</p></div>{draft.status === 'pending' && (selected.import.tripId ? <aside><button onClick={() => approve(draft)} disabled={!draft.startsAt}>Approve</button><button onClick={() => discard(draft)}>Discard</button></aside> : <aside><small className="needs-trip">Assign a trip to approve</small></aside>)}</article>)}</section> : items.length ? <section className="import-list">{items.map((item) => <button key={item.id} onClick={() => open(item.id)}><span className="doc-icon sea"><PackageCheck size={18} /></span><span><b>{item.subject || 'Forwarded reservation'}</b><small>{item.sender} · {formatDay(item.createdAt)}</small></span><em>{importStatusLabel(item)}</em></button>)}</section> : <EmptyPanel icon={<PackageCheck />} title="Inbox is clear" description="Forward a booking confirmation to your central address and it will appear here for review." />}</>
}

function SuggestedTrip({ suggestion, onCreate }: { suggestion: TripSuggestion; onCreate: () => void }) {
  const shaky = suggestion.confidence < 0.5
  return <div className="suggested-trip">
    <div><p className="eyebrow">NO MATCHING TRIP</p><b>{suggestion.destination}</b><small>{formatDateRange(suggestion.startDate, suggestion.endDate)}</small>
      {shaky && <p className="suggested-warning"><TriangleAlert size={13} /> Extracted with low confidence ({Math.round(suggestion.confidence * 100)}%) — check the dates carefully.</p>}
    </div>
    <button onClick={onCreate}><Plus size={14} /> Create trip</button>
  </div>
}

// A fallback draft is a keyword guess, not an extraction. Say so rather than letting
// a low-confidence draft look like the AI's considered answer.
function ExtractionNotice({ reason }: { reason: string }) {
  return <div className="extraction-notice"><TriangleAlert size={16} /><div><b>AI extraction didn’t run</b><p>{reason}</p><p>The draft below is a keyword guess from the subject line. Check every field before approving it.</p></div></div>
}

function Imports({ tripID, onReload, notify }: { tripID: string; onReload: () => Promise<void>; notify: (message: string) => void }) {
  const [items,setItems]=useState<ReservationImport[]>([]);const [address,setAddress]=useState('');const [selected,setSelected]=useState<ImportDetail | null>(null);const [error,setError]=useState('')
  async function refresh(){try{setItems(await api.listImports(tripID))}catch(err){setError(message(err))}}
  useEffect(()=>{setSelected(null);setAddress('');refresh()},[tripID])
  const active=items.some((item)=>item.status==='queued'||item.status==='processing')
  useEffect(()=>{if(!active)return;const id=window.setInterval(refresh,3000);return()=>window.clearInterval(id)},[active,tripID])
  async function forwardingAddress(){try{const result=await api.getImportAddress(tripID);setAddress(result.address);await navigator.clipboard?.writeText(result.address);notify('Forwarding address copied')}catch(err){setError(message(err))}}
  async function open(id:string){try{setSelected(await api.getImport(id))}catch(err){setError(message(err))}}
  async function approve(draft:ImportDetail['drafts'][number]){if(!selected||!draft.startsAt){setError('Add a start time in the plan form before approving this draft.');return};try{await api.approveDraft(selected.import.id,draft);await refresh();setSelected(await api.getImport(selected.import.id));await onReload();notify('Reservation added to your itinerary')}catch(err){setError(message(err))}}
  async function discard(draft:ImportDetail['drafts'][number]){if(!selected)return;try{await api.discardDraft(selected.import.id,draft.id);setSelected(await api.getImport(selected.import.id));await refresh();notify('Draft discarded')}catch(err){setError(message(err))}}
  async function retry(id:string){try{await api.retryImport(id);notify('Retrying…');await refresh();if(selected?.import.id===id)setSelected(await api.getImport(id))}catch(err){setError(message(err))}}
  return <><section className="plans-title"><div><p className="eyebrow">FORWARD & REVIEW</p><h2>Reservation imports</h2></div><button className="round-add" onClick={forwardingAddress} aria-label="Get forwarding address"><Plus size={19}/></button></section><p className="route-intro">Forward booking emails here. Waypoint creates a private draft; nothing reaches the itinerary until you approve it.</p>{address&&<button className="forwarding-address" onClick={()=>navigator.clipboard?.writeText(address)}><span>FORWARD TO THIS TRIP</span><b>{address}</b><small>Tap to copy</small></button>}{error&&<p className="form-error">{error}</p>}{selected?<section className="import-detail"><button className="text-button" onClick={()=>setSelected(null)}>‹ All imports</button><p className="eyebrow">{selected.import.sender}</p><h2>{selected.import.subject}</h2><ImportStatus item={selected.import} onRetry={retry}/>{selected.drafts.map((draft)=><article className="import-draft" key={draft.id}><span className={`timeline-dot ${kinds[draft.kind].color}`}>{iconFor(draft.kind,16)}</span><div><b>{draft.title}</b><small>{draft.supplier||'Reservation draft'} · {Math.round(draft.confidence*100)}% confidence</small><p>{draft.startsAt?`${formatDay(draft.startsAt)} ${formatTime(draft.startsAt)}`:'Time needs review'}{draft.location?` · ${draft.location}`:''}</p></div>{draft.status==='pending'&&<aside><button onClick={()=>approve(draft)} disabled={!draft.startsAt}>Approve</button><button onClick={()=>discard(draft)}>Discard</button></aside>}</article>)}</section>:items.length?<section className="import-list">{items.map((item)=><button key={item.id} onClick={()=>open(item.id)}><span className="doc-icon sea"><FileText size={18}/></span><span><b>{item.subject||'Reservation email'}</b><small>{item.sender} · {formatDay(item.createdAt)}</small></span><em>{importStatusLabel(item)}</em></button>)}</section>:<EmptyPanel icon={<FileText/>} title="No forwarded reservations" description="Create the address above, then forward an airline, hotel, train, or ticket confirmation."/>}</>
}

function CalendarView({ plans }: { plans: Plan[] }) { const [status,setStatus]=useState<{connected:boolean;email?:string;lastSyncedAt?:string;lastError?:string}>({connected:false});const [error,setError]=useState('');async function refresh(){try{setStatus(await api.calendarStatus())}catch(err){setError(message(err))}}useEffect(()=>{refresh()},[]);async function connect(){try{const result=await api.calendarConnect();window.location.assign(result.url)}catch(err){setError(message(err))}}async function sync(){try{await api.calendarSync();await refresh()}catch(err){setError(message(err))}}return <><section className="plans-title"><div><p className="eyebrow">YOUR SCHEDULE</p><h2>Trip calendar</h2></div><CalendarDays className="shield" size={25}/></section><section className="calendar-connection"><div><b>{status.connected?'Google Calendar connected':'Keep plans in sync'}</b><p>{status.connected?`${status.email||'Dedicated Waypoint calendar'}${status.lastSyncedAt?` · synced ${formatDay(status.lastSyncedAt)}`:''}`:'Connect a separate Waypoint calendar. Personal calendars stay private.'}</p></div><button onClick={status.connected?sync:connect}>{status.connected?'Sync now':'Connect'}</button></section>{error&&<p className="form-error">{error}</p>}<section className="calendar-agenda">{plans.length?plans.map((plan)=><div key={plan.id}><time>{formatDay(plan.startsAt)}<small>{formatTime(plan.startsAt)}</small></time><span className={`timeline-dot ${kinds[plan.kind].color}`}>{iconFor(plan.kind,16)}</span><p><b>{plan.title}</b><small>{plan.location||kinds[plan.kind].label}</small></p></div>):<EmptyPanel icon={<CalendarDays/>} title="No calendar events" description="Add itinerary plans to see them in your trip calendar."/>}</section></> }

function Documents({ detail, onReload, onDelete, notify }: { detail: TripDetail; onReload: () => Promise<void>; onDelete: (doc: Document) => void; notify: (message: string) => void }) {
  const upload = useRef<HTMLInputElement>(null); const [busy, setBusy] = useState(false); const [error, setError] = useState('')
  async function uploadDocument(file: File) { if (!supabase) return; setError(''); if (!['application/pdf','image/jpeg','image/png','image/webp'].includes(file.type) || file.size > 10 * 1024 * 1024) { setError('Use a PDF, JPEG, PNG, or WebP under 10 MB.'); return }; setBusy(true); const path = `${detail.trip.id}/${crypto.randomUUID()}-${file.name.replace(/[^a-zA-Z0-9._-]/g, '-')}`; const { error: storageError } = await supabase.storage.from('trip-documents').upload(path, file, { contentType: file.type, upsert: false }); if (storageError) { setError(storageError.message); setBusy(false); return }; try { await api.createDocument(detail.trip.id, { name: file.name, storagePath: path, contentType: file.type, sizeBytes: file.size }); await onReload(); notify('Document saved to your private wallet') } catch (err) { await supabase.storage.from('trip-documents').remove([path]); setError(message(err)) } finally { setBusy(false) } }
  async function openDocument(doc: Document) { if (!supabase) return; const { data, error } = await supabase.storage.from('trip-documents').createSignedUrl(doc.storagePath, 60); if (error || !data) { setError(error?.message || 'Could not open document'); return }; window.open(data.signedUrl, '_blank', 'noopener,noreferrer') }
  return <><section className="plans-title"><div><p className="eyebrow">PRIVATE & OFFLINE-READY</p><h2>Travel wallet</h2></div><ShieldCheck className="shield" size={25} /></section><section className="wallet-note"><span><ShieldCheck size={19} /></span><div><b>Private by default</b><p>Only members of this trip can open these documents.</p></div></section><button className="upload-zone" onClick={() => upload.current?.click()} disabled={busy}>{busy ? <LoaderCircle className="spin" /> : <Upload />}<span>{busy ? 'Saving document…' : 'Add a confirmation or document'}</span><small>PDF, JPEG, PNG, or WebP · up to 10 MB</small></button><input ref={upload} className="visually-hidden" type="file" accept="application/pdf,image/jpeg,image/png,image/webp" onChange={(event) => { const file = event.target.files?.[0]; if (file) uploadDocument(file); event.target.value = '' }} />{error && <p className="form-error">{error}</p>}<p className="group-label">YOUR DOCUMENTS</p>{detail.documents.length ? <section className="doc-list">{detail.documents.map((doc) => <div className="list-row" key={doc.id}><button className="doc-row" onClick={() => openDocument(doc)}><span className="doc-icon blue"><FileText size={19} /></span><span><b>{doc.name}</b><small>{formatBytes(doc.sizeBytes)} · {formatDay(doc.createdAt)}</small></span><em>Open</em></button><button className="row-delete" onClick={() => onDelete(doc)} aria-label={`Delete ${doc.name}`}><Trash2 size={15} /></button></div>)}</section> : <EmptyPanel icon={<FolderOpen />} title="Nothing stored yet" description="Keep boarding passes, hotel bookings, insurance, and tickets here." />}</> }

function Members({ detail, tripID, onReload, onRemove, notify }: { detail: TripDetail; tripID: string; onReload: () => Promise<void>; onRemove: (member: TripDetail['members'][number]) => void; notify: (message: string) => void }) {
  const [email, setEmail] = useState(''); const [busy, setBusy] = useState(false); const [error, setError] = useState('')
  async function invite(event: FormEvent) { event.preventDefault(); if (!email) return; setBusy(true); setError(''); try { await api.addMember(tripID, email); setEmail(''); await onReload(); notify('Travel mate added') } catch (err) { setError(message(err)) } finally { setBusy(false) } }
  return <><section className="plans-title"><div><p className="eyebrow">SHARED TRIP</p><h2>Travel mates</h2></div><UsersRound className="shield" size={25} /></section><section className="member-list">{detail.members.map((member) => <div key={member.id}><span className="member-avatar">{initials(member.displayName || member.email)}</span><span><b>{member.displayName || member.email}</b><small>{member.email}</small></span><em>{member.role}</em>{member.role === 'owner' ? <span className="row-spacer" /> : <button className="row-delete" onClick={() => onRemove(member)} aria-label={`Remove ${member.displayName || member.email}`}><Trash2 size={15} /></button>}</div>)}</section><form className="invite-form" onSubmit={invite}><label className="form-label">INVITE A FRIEND<input type="email" value={email} onChange={(event) => setEmail(event.target.value)} placeholder="friend@example.com" required /></label><button className="save-button" disabled={busy}>{busy ? 'Adding…' : 'Add travel mate'}</button></form>{error && <p className="form-error">{error}</p>}<p className="invite-note">They need to sign in with Google once before you can add them.</p></> }

function PlanDetailSheet({ plan, onClose, onEdit, onDelete }: { plan: Plan; onClose: () => void; onEdit: () => void; onDelete: () => void }) {
  return <Sheet className="detail-sheet" eyebrow={kinds[plan.kind].label.toUpperCase()} title={plan.title} onClose={onClose}>
    <div className={`detail-icon ${kinds[plan.kind].color}`}>{iconFor(plan.kind, 24)}</div>
    <p className="detail-meta">{formatDay(plan.startsAt)} · {formatTime(plan.startsAt)}{plan.endsAt ? ` – ${formatTime(plan.endsAt)}` : ''}</p>
    {plan.location && <div className="detail-block"><MapPin size={17} /><div><b>{plan.location}</b><span>Location</span></div></div>}
    {plan.confirmationCode && <div className="detail-block"><Ticket size={17} /><div><b>{plan.confirmationCode}</b><span>Confirmation code</span></div></div>}
    {plan.endsAt && <div className="detail-block"><Clock3 size={17} /><div><b>{formatDay(plan.endsAt)} · {formatTime(plan.endsAt)}</b><span>Ends</span></div></div>}
    {plan.notes && <div className="detail-block"><FileText size={17} /><div><b>{plan.notes}</b><span>Notes</span></div></div>}
    {!plan.location && !plan.confirmationCode && !plan.notes && !plan.endsAt && <div className="detail-block"><MoreHorizontal size={17} /><div><b>No extra details yet</b><span>Add a location, confirmation code, or notes</span></div></div>}
    <div className="detail-actions"><button onClick={onEdit}><Pencil size={15} /> Edit</button><button className="danger" onClick={onDelete}><Trash2 size={15} /> Delete</button></div>
  </Sheet>
}

function TripSwitcherSheet({ trips, activeTripID, onClose, onSelect, onCreate }: { trips: Trip[]; activeTripID: string; onClose: () => void; onSelect: (tripId: string) => void; onCreate: () => void }) {
  return <Sheet className="picker-sheet" eyebrow="YOUR TRIPS" title="Switch trip" onClose={onClose}>
    <div className="trip-list">{trips.map((trip) => <button key={trip.id} className={trip.id === activeTripID ? 'selected' : ''} onClick={() => onSelect(trip.id)} aria-current={trip.id === activeTripID ? 'true' : undefined}><span className="trip-swatch" style={{ background: trip.coverColor }} /><span><b>{trip.title}</b><small>{trip.destination} · {formatDateRange(trip.startDate, trip.endDate)}</small></span>{trip.id === activeTripID ? <Check size={17} /> : <span />}</button>)}</div>
    <button className="create-trip" onClick={onCreate}><Plus size={17} /> Create a trip</button>
  </Sheet>
}

function ConfirmSheet({ title, body, confirmLabel, onConfirm, onClose }: { title: string; body: string; confirmLabel: string; onConfirm: () => Promise<void>; onClose: () => void }) {
  const [busy, setBusy] = useState(false)
  async function run() { setBusy(true); await onConfirm() }
  return <Sheet eyebrow="CONFIRM" title={title} onClose={onClose}><p className="confirm-body">{body}</p><button className="save-button danger" onClick={run} disabled={busy}>{busy ? 'Working…' : confirmLabel}</button><button className="save-button soft" onClick={onClose} disabled={busy}>Keep it</button></Sheet>
}

function ColorPicker({ value, onChange }: { value: string; onChange: (color: string) => void }) {
  return <div className="form-label">TRIP COLOUR<div className="color-row">{coverColors.map((color) => <button type="button" key={color} className={value === color ? 'selected' : ''} style={{ background: color }} onClick={() => onChange(color)} aria-label={`Use ${color}`} aria-pressed={value === color} />)}</div></div>
}

function TripFormSheet({ trip, suggestion, onClose, onSubmit, onDelete }: { trip?: Trip; suggestion?: TripSuggestion; onClose: () => void; onSubmit: (input: TripInput) => Promise<void>; onDelete?: () => void }) {
  const [busy, setBusy] = useState(false); const [error, setError] = useState(''); const [coverColor, setCoverColor] = useState(trip?.coverColor || coverColors[0])
  const [startDate, setStartDate] = useState(toDateInput(trip?.startDate) || suggestion?.startDate || '')
  const [endDate, setEndDate] = useState(toDateInput(trip?.endDate) || suggestion?.endDate || '')
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); const form = new FormData(event.currentTarget); setBusy(true); setError('')
    // Hidden inputs are exempt from constraint validation, so the dates are checked here.
    if (!form.get('startDate') || !form.get('endDate')) { setError('Pick a check-in and a check-out date.'); setBusy(false); return }
    try { await onSubmit({ title: String(form.get('title')), destination: String(form.get('destination')), startDate: toAPIDate(String(form.get('startDate'))), endDate: toAPIDate(String(form.get('endDate'))), coverColor }) } catch (err) { setError(message(err)); setBusy(false) }
  }
  return <Sheet title={trip ? 'Trip settings' : suggestion ? 'Create the suggested trip' : 'Create a trip'} eyebrow={trip ? 'EDIT THIS TRIP' : suggestion ? 'FROM A FORWARDED EMAIL' : 'START A NEW ADVENTURE'} onClose={onClose}><form onSubmit={submit}>{suggestion && <p className="confirm-body">Filled in from the forwarded reservation. Check each field — nothing is created until you save.</p>}<label className="form-label">TRIP NAME<input name="title" defaultValue={trip?.title ?? suggestion?.title} placeholder="Barcelona weekend" required maxLength={100} /></label><label className="form-label">DESTINATION<input name="destination" defaultValue={trip?.destination ?? suggestion?.destination} placeholder="Barcelona, Spain" required maxLength={120} /></label><DateRangePicker startDate={startDate} endDate={endDate} onChange={(nextStart, nextEnd) => { setStartDate(nextStart); setEndDate(nextEnd) }} /><input type="hidden" name="startDate" value={startDate} /><input type="hidden" name="endDate" value={endDate} /><ColorPicker value={coverColor} onChange={setCoverColor} />{error && <p className="form-error">{error}</p>}<button className="save-button" disabled={busy || !startDate || !endDate}>{busy ? 'Saving…' : trip ? 'Save changes' : 'Create trip'}</button>{onDelete && <button type="button" className="save-button soft danger" onClick={onDelete}><Trash2 size={15} /> Delete this trip</button>}</form></Sheet>
}

// The calendar works entirely in "YYYY-MM-DD" keys — the same shape the form submits — so
// nothing here can drift a day across time zones the way a Date round-trip can.
const weekdayInitials = ['M', 'T', 'W', 'T', 'F', 'S', 'S']
const rangePresets: { label: string; nights: number; fromWeekend?: boolean }[] = [
  { label: 'Weekend', nights: 2, fromWeekend: true },
  { label: '3 nights', nights: 3 },
  { label: '1 week', nights: 7 },
  { label: '2 weeks', nights: 14 },
]

function DateRangePicker({ startDate, endDate, onChange }: { startDate: string; endDate: string; onChange: (start: string, end: string) => void }) {
  const today = dateKey(new Date())
  const [month, setMonth] = useState(() => monthStart(startDate || today))
  const first = new Date(month.getFullYear(), month.getMonth(), 1)
  // Monday-first grid: shift Sunday (0) to the end of the week.
  const lead = (first.getDay() + 6) % 7
  const days = new Date(month.getFullYear(), month.getMonth() + 1, 0).getDate()
  const cells = [...Array(lead).fill(''), ...Array.from({ length: days }, (_, index) => dateKey(new Date(month.getFullYear(), month.getMonth(), index + 1)))]
  const stay = startDate && endDate ? nightsBetween(startDate, endDate) : 0

  // A click either closes the open range or re-anchors it; an earlier day always re-anchors,
  // so an end before the start is unreachable rather than merely rejected.
  function pick(day: string) {
    if (!startDate || endDate || day < startDate) { onChange(day, ''); return }
    onChange(startDate, day)
  }
  function applyPreset(nights: number, fromWeekend?: boolean) {
    const from = fromWeekend ? nextFriday(today) : today
    onChange(from, addDays(from, nights)); setMonth(monthStart(from))
  }
  function shiftMonth(step: number) { setMonth(new Date(month.getFullYear(), month.getMonth() + step, 1)) }

  return <div className="date-range-picker">
    <div className="drp-head"><p className="eyebrow">TRIP DATES</p><span className="drp-nights">{stay} night{stay === 1 ? '' : 's'}</span></div>
    <div className="drp-fields">
      <button type="button" className={startDate ? 'drp-field filled' : 'drp-field'} onClick={() => startDate && setMonth(monthStart(startDate))}><small>CHECK-IN</small><b>{startDate ? formatKey(startDate) : 'Select a date'}</b></button>
      <button type="button" className={endDate ? 'drp-field filled' : 'drp-field'} onClick={() => endDate && setMonth(monthStart(endDate))}><small>CHECK-OUT</small><b>{endDate ? formatKey(endDate) : 'Select a date'}</b></button>
    </div>
    <div className="drp-month"><button type="button" onClick={() => shiftMonth(-1)} aria-label="Previous month"><ChevronLeft size={17} /></button><b>{formatMonth(month)}</b><button type="button" onClick={() => shiftMonth(1)} aria-label="Next month"><ChevronRight size={17} /></button></div>
    <div className="drp-weekdays" aria-hidden="true">{weekdayInitials.map((initial, index) => <span key={index}>{initial}</span>)}</div>
    <div className="drp-grid">{cells.map((day, index) => {
      if (!day) return <span key={`blank-${index}`} />
      const selected = day === startDate || day === endDate
      const within = Boolean(endDate) && day > startDate && day < endDate
      // A lone anchor is both ends of itself, so it stays a full rounded pill until a range closes.
      const edges = `${day === startDate ? ' start' : ''}${day === (endDate || startDate) ? ' end' : ''}`
      return <button type="button" key={day} className={`drp-day${selected ? ` selected${edges}` : ''}${within ? ' within' : ''}${day === today ? ' today' : ''}`} onClick={() => pick(day)} aria-pressed={selected || within} aria-label={formatKeyLong(day)}>{Number(day.slice(8))}</button>
    })}</div>
    <div className="drp-presets">{rangePresets.map((preset) => <button type="button" key={preset.label} onClick={() => applyPreset(preset.nights, preset.fromWeekend)}>{preset.label}</button>)}<button type="button" className="drp-clear" onClick={() => onChange('', '')}>Clear</button></div>
  </div>
}

function PlanFormSheet({ plan, onClose, onSubmit }: { plan?: Plan; onClose: () => void; onSubmit: (input: PlanInput, planID?: string) => Promise<void> }) {
  const [busy, setBusy] = useState(false); const [error, setError] = useState(''); const [kind, setKind] = useState<PlanKind>(plan?.kind || 'flight')
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); const form = new FormData(event.currentTarget); const endsAt = String(form.get('endsAt') || '').trim(); setBusy(true); setError('')
    try { await onSubmit({ kind, title: String(form.get('title')), startsAt: new Date(String(form.get('startsAt'))).toISOString(), endsAt: endsAt ? new Date(endsAt).toISOString() : undefined, location: String(form.get('location')), confirmationCode: String(form.get('confirmationCode')), notes: String(form.get('notes')) }, plan?.id) } catch (err) { setError(message(err)); setBusy(false) }
  }
  return <Sheet title={plan ? 'Edit plan' : 'New plan'} eyebrow={plan ? 'UPDATE THIS PLAN' : 'ADD TO YOUR ITINERARY'} onClose={onClose}><form onSubmit={submit}><div className="type-grid">{(Object.keys(kinds) as PlanKind[]).map((value) => { const Icon = kinds[value].icon; return <button type="button" key={value} className={kind === value ? 'selected' : ''} onClick={() => setKind(value)}><Icon size={21} />{kinds[value].label}</button> })}</div><label className="form-label">WHAT'S HAPPENING?<input name="title" defaultValue={plan?.title} placeholder="Flight to Barcelona" required maxLength={180} /></label><div className="input-pair"><label className="form-label">START TIME<input name="startsAt" type="datetime-local" defaultValue={toLocalInput(plan?.startsAt)} required /></label><label className="form-label">END TIME<input name="endsAt" type="datetime-local" defaultValue={toLocalInput(plan?.endsAt)} /></label></div><label className="form-label">LOCATION<input name="location" defaultValue={plan?.location} placeholder="Airport, hotel, venue…" /></label><label className="form-label">CONFIRMATION CODE<input name="confirmationCode" defaultValue={plan?.confirmationCode} placeholder="Optional" /></label><label className="form-label">NOTES<input name="notes" defaultValue={plan?.notes} placeholder="Gate closes 40 minutes before departure." /></label>{error && <p className="form-error">{error}</p>}<button className="save-button" disabled={busy}>{busy ? 'Saving…' : plan ? 'Save changes' : `Add ${kinds[kind].label.toLowerCase()}`}</button></form></Sheet>
}

function RouteFormSheet({ option, onClose, onSubmit }: { option?: RouteOption; onClose: () => void; onSubmit: (input: RouteOptionInput, optionID?: string) => Promise<void> }) {
  const [busy, setBusy] = useState(false); const [error, setError] = useState(''); const [routeType, setRouteType] = useState<RouteOptionType>(option?.routeType || 'direct_flight')
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); const form = new FormData(event.currentTarget); const price = String(form.get('price')).trim(); const duration = String(form.get('duration')).trim(); setBusy(true); setError('')
    try { await onSubmit({ title: String(form.get('title')), routeType, origin: String(form.get('origin')), destination: String(form.get('destination')), durationMinutes: duration ? Number(duration) : undefined, transfers: Number(form.get('transfers') || 0), priceAmount: price ? Number(price) : undefined, currency: price ? String(form.get('currency')).toUpperCase() : undefined, bookingUrl: String(form.get('bookingUrl')), notes: String(form.get('notes')), status: String(form.get('status')) as RouteOptionStatus }, option?.id) } catch (err) { setError(message(err)); setBusy(false) }
  }
  return <Sheet title={option ? 'Edit route option' : 'Compare a route'} eyebrow={option ? 'UPDATE THIS OPTION' : 'LOG WHAT YOU FOUND'} onClose={onClose}><form onSubmit={submit}><div className="route-type-grid">{(Object.keys(routeLabels) as RouteOptionType[]).map((value) => <button type="button" key={value} className={routeType === value ? 'selected' : ''} onClick={() => setRouteType(value)}>{routeLabels[value]}</button>)}</div><label className="form-label">OPTION NAME<input name="title" defaultValue={option?.title} placeholder="EasyJet + Renfe via Madrid" required maxLength={180} /></label><div className="input-pair"><label className="form-label">FROM<input name="origin" defaultValue={option?.origin} placeholder="London" /></label><label className="form-label">TO<input name="destination" defaultValue={option?.destination} placeholder="Barcelona" /></label></div><div className="route-input-row"><label className="form-label">PRICE<input name="price" defaultValue={option?.priceAmount} inputMode="decimal" placeholder="79.50" /></label><label className="form-label">CURRENCY<input name="currency" defaultValue={option?.currency || 'EUR'} maxLength={3} /></label><label className="form-label">MINUTES<input name="duration" defaultValue={option?.durationMinutes} inputMode="numeric" placeholder="165" /></label></div><label className="form-label">TRANSFERS<input name="transfers" type="number" min="0" max="20" defaultValue={option?.transfers ?? 0} /></label><label className="form-label">SAVED BOOKING LINK<input name="bookingUrl" type="url" defaultValue={option?.bookingUrl} placeholder="https://…" /></label><label className="form-label">WHY CONSIDER IT?<input name="notes" defaultValue={option?.notes} placeholder="Cheapest, but an early airport transfer." /></label><label className="form-label">STATUS<select name="status" defaultValue={option?.status || 'considering'}><option value="considering">Considering</option><option value="shortlisted">Shortlisted</option><option value="booked">Booked</option><option value="dismissed">Dismissed</option></select></label>{error && <p className="form-error">{error}</p>}<button className="save-button" disabled={busy}>{busy ? 'Saving…' : option ? 'Save changes' : 'Save route option'}</button></form></Sheet>
}
function Sheet({ eyebrow,title,onClose,className,children }:{eyebrow:string;title:string;onClose:()=>void;className?:string;children:ReactNode}){return <div className="overlay" onMouseDown={onClose}><section className={`sheet${className?` ${className}`:''}`} onMouseDown={(event)=>event.stopPropagation()}><div className="sheet-handle"/><div className="sheet-heading"><div><p className="eyebrow">{eyebrow}</p><h2>{title}</h2></div><button className="close-button" onClick={onClose} aria-label="Close"><X size={20}/></button></div>{children}</section></div>}
function EmptyTrips({ onCreate }:{onCreate:()=>void}){return <section className="empty-trips"><span className="brand-mark">W</span><p className="eyebrow">YOUR TRAVEL SPACE</p><h1>Start your first shared trip.</h1><p>Bring your bookings, plans, and travel documents into one place before the group chat gets chaotic.</p><button className="save-button" onClick={onCreate}><Plus size={18}/> Create a trip</button></section>}
function EmptyPanel({ icon,title,description }:{icon:ReactNode;title:string;description:string}){return <section className="empty-panel"><span>{icon}</span><b>{title}</b><p>{description}</p></section>}

// A trip is only worth suggesting when the extraction produced something to build it
// from: real dates and a place. Keyword fallbacks have neither, so they never qualify.
// The result is a starting point for the create form, never a trip created outright.
const destinationPriority: PlanKind[] = ['stay', 'activity', 'food', 'transport', 'flight', 'other']

export function deriveTripSuggestion(drafts: ReservationDraft[]): TripSuggestion | null {
  const usable = drafts.filter((draft) => draft.status !== 'discarded' && draft.startsAt)
  if (!usable.length) return null
  const destination = suggestDestination(usable)
  if (!destination) return null
  const starts = usable.map((draft) => draft.startsAt!).sort()
  const ends = usable.map((draft) => draft.endsAt || draft.startsAt!).sort()
  return {
    destination,
    title: `${destination}, ${new Intl.DateTimeFormat(undefined, { month: 'long' }).format(new Date(starts[0]))}`,
    startDate: starts[0].slice(0, 10),
    endDate: ends[ends.length - 1].slice(0, 10),
    confidence: Math.min(...usable.map((draft) => draft.confidence)),
  }
}

function suggestDestination(drafts: ReservationDraft[]): string {
  for (const kind of destinationPriority) {
    const match = drafts.find((draft) => draft.kind === kind && draft.location.trim())
    // Addresses conventionally end with the city, and a whole street address makes
    // a poor destination. The user edits this before anything is created.
    if (match) return match.location.split(',').map((part) => part.trim()).filter(Boolean).pop() || ''
  }
  // Flight titles from the extractor read "BA487 London → Barcelona".
  const routed = drafts.map((draft) => draft.title).find((title) => title.includes('→'))
  return routed ? routed.split('→').pop()!.trim() : ''
}

// Mirror the server's ordering so an optimistic insert lands where a reload would put it.
function sortPlans(plans:Plan[]){return [...plans].sort((a,b)=>a.startsAt.localeCompare(b.startsAt))}
function sortRouteOptions(options:RouteOption[]){return [...options].sort((a,b)=>Number(b.status==='shortlisted')-Number(a.status==='shortlisted')||b.createdAt.localeCompare(a.createdAt))}
// Strip the server-owned fields so a deleted record can be posted back for undo.
function planInput({kind,title,startsAt,endsAt,location,confirmationCode,notes}:Plan):PlanInput{return {kind,title,startsAt,endsAt,location,confirmationCode,notes}}
function routeOptionInput({title,routeType,origin,destination,departsAt,arrivesAt,durationMinutes,transfers,priceAmount,currency,bookingUrl,notes,status}:RouteOption):RouteOptionInput{return {title,routeType,origin,destination,departsAt,arrivesAt,durationMinutes,transfers,priceAmount,currency,bookingUrl,notes,status}}

function iconFor(kind:PlanKind,size:number){const Icon=kinds[kind].icon;return <Icon size={size}/>}
function initials(value:string){return value.split(/\s+/).slice(0,2).map((part)=>part[0]).join('').toUpperCase()}
function importStatusLabel(item:ReservationImport){const duplicate=item.duplicateOfImportId?' · duplicate?':'';if(item.status==='processing')return `${item.stage?`Processing · ${item.stage}`:'Processing'}${duplicate}`;if(item.status==='failed')return `Failed${duplicate}`;return `${item.status}${duplicate}`}
function ImportStatus({ item, onRetry }: { item: ReservationImport; onRetry: (id: string) => void }) {
  const stage = item.status === 'processing' ? (item.stage ? ` · ${item.stage}` : '') : ''
  return <div className="import-status-row"><p className="import-status">{item.status}{stage}{item.usedLlm ? ' · assisted extraction' : ''}{item.duplicateOfImportId ? ' · possible duplicate' : ''}</p>{item.status === 'failed' && <button className="text-button" onClick={() => onRetry(item.id)}>Retry</button>}{item.status === 'failed' && item.errorMessage && <p className="form-error">{item.errorMessage}</p>}</div>
}
function message(error:unknown){return error instanceof Error?error.message:'Something went wrong'}
function toAPIDate(value:string){return new Date(`${value}T00:00:00.000Z`).toISOString()}
// Trip dates are stored as UTC midnight, so read the calendar day back off the raw string.
function toDateInput(value?:string){return value?value.slice(0,10):''}
// datetime-local expects wall-clock time in the viewer's zone, not UTC.
function toLocalInput(value?:string){if(!value)return '';const date=new Date(value);const pad=(part:number)=>String(part).padStart(2,'0');return `${date.getFullYear()}-${pad(date.getMonth()+1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`}
// Calendar-key helpers for the date range picker: plain local "YYYY-MM-DD", never parsed as UTC.
function dateKey(date:Date){const pad=(part:number)=>String(part).padStart(2,'0');return `${date.getFullYear()}-${pad(date.getMonth()+1)}-${pad(date.getDate())}`}
function fromKey(key:string){const [year,month,day]=key.split('-').map(Number);return new Date(year,month-1,day)}
function monthStart(key:string){const date=fromKey(key);return new Date(date.getFullYear(),date.getMonth(),1)}
function addDays(key:string,days:number){const date=fromKey(key);date.setDate(date.getDate()+days);return dateKey(date)}
function nextFriday(key:string){return addDays(key,(5-fromKey(key).getDay()+7)%7)}
function nightsBetween(start:string,end:string){return Math.max(0,Math.round((fromKey(end).getTime()-fromKey(start).getTime())/86400000))}
function formatKey(key:string){return new Intl.DateTimeFormat(undefined,{day:'numeric',month:'short'}).format(fromKey(key))}
function formatKeyLong(key:string){return new Intl.DateTimeFormat(undefined,{weekday:'long',day:'numeric',month:'long',year:'numeric'}).format(fromKey(key))}
function formatMonth(date:Date){return new Intl.DateTimeFormat(undefined,{month:'long',year:'numeric'}).format(date)}
function formatDay(value:string){return new Intl.DateTimeFormat(undefined,{day:'numeric',month:'short'}).format(new Date(value))}
function formatTime(value:string){return new Intl.DateTimeFormat(undefined,{hour:'numeric',minute:'2-digit'}).format(new Date(value))}
function formatDateRange(start:string,end:string){return `${formatDay(start)} – ${formatDay(end)}`}
// Group by the plan's local calendar day, keyed YYYY-MM-DD so the keys sort naturally.
function groupByDay(plans:Plan[]){
  const days=new Map<string,Plan[]>()
  for(const plan of plans){const date=new Date(plan.startsAt);const key=`${date.getFullYear()}-${String(date.getMonth()+1).padStart(2,'0')}-${String(date.getDate()).padStart(2,'0')}`;days.set(key,[...(days.get(key)||[]),plan])}
  return [...days.entries()].sort(([a],[b])=>a.localeCompare(b)).map(([key,dayPlans])=>({key,plans:dayPlans}))
}
function weekday(key:string){return new Intl.DateTimeFormat(undefined,{weekday:'short'}).format(new Date(`${key}T12:00:00`)).toUpperCase()}
function formatWeekday(key:string){return new Intl.DateTimeFormat(undefined,{weekday:'long',day:'numeric',month:'long'}).format(new Date(`${key}T12:00:00`)).toUpperCase()}
function nights(start:string,end:string){return Math.max(0,Math.round((new Date(end).getTime()-new Date(start).getTime())/86400000))}
function formatBytes(bytes:number){return bytes<1024*1024?`${Math.ceil(bytes/1024)} KB`:`${(bytes/1024/1024).toFixed(1)} MB`}
function formatDuration(minutes?:number){if(minutes===undefined)return 'Time unknown';const hours=Math.floor(minutes/60);const remainder=minutes%60;return hours?`${hours}h ${remainder?`${remainder}m`:''}`:`${remainder}m`}
function formatPrice(option:RouteOption){return option.priceAmount===undefined?'Price unknown':new Intl.NumberFormat(undefined,{style:'currency',currency:option.currency||'EUR'}).format(option.priceAmount)}
