import { useEffect, useRef, useState, type FormEvent, type ReactNode } from 'react'
import type { Session } from '@supabase/supabase-js'
import {
  CalendarDays, Check, ChevronDown, FileText, FolderOpen, Globe2, Hotel, LoaderCircle,
  Clock3, LogOut, MapPin, MoreHorizontal, PackageCheck, Plane, Plus, Route, ShieldCheck, Ticket,
  TrainFront, Upload, UsersRound, X,
} from 'lucide-react'
import { api, type ChecklistItem, type Document, type ImportDetail, type Plan, type PlanKind, type ReservationImport, type RouteOption, type RouteOptionStatus, type RouteOptionType, type Trip, type TripDetail } from './lib/api'
import { isSupabaseConfigured, supabase } from './lib/supabase'

type View = 'overview' | 'plans' | 'routes' | 'documents' | 'imports' | 'calendar' | 'members' | 'inbox'

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
  const [trips, setTrips] = useState<Trip[]>([])
  const [activeTripID, setActiveTripID] = useState('')
  const [detail, setDetail] = useState<TripDetail | null>(null)
  const [view, setView] = useState<View>('overview')
  const [loading, setLoading] = useState(true)
  const [tripListFailed, setTripListFailed] = useState(false)
  const [error, setError] = useState('')
  const [toast, setToast] = useState('')
  const [sheet, setSheet] = useState<'trip' | 'plan' | 'route' | null>(null)

  function notify(message: string) { setToast(message); window.setTimeout(() => setToast(''), 3000) }
  async function loadTrips(preferredID?: string) {
    setLoading(true); setTripListFailed(false); setError('')
    try { const nextTrips = await api.listTrips(); setTrips(nextTrips); setActiveTripID((current) => preferredID || current || nextTrips[0]?.id || '') } catch (err) { setError(message(err)); setTripListFailed(true) } finally { setLoading(false) }
  }
  async function loadDetail(tripID: string) {
    if (!tripID) { setDetail(null); return }
    setDetail(null); setError('')
    try { setDetail(await api.getTrip(tripID)) } catch (err) { setError(message(err)) }
  }
  useEffect(() => { loadTrips() }, [])
  useEffect(() => { loadDetail(activeTripID) }, [activeTripID])

  async function createTrip(input: { title: string; destination: string; startDate: string; endDate: string }) {
    const trip = await api.createTrip({ ...input, startDate: toAPIDate(input.startDate), endDate: toAPIDate(input.endDate), coverColor: '#1d4c46' })
    setSheet(null); await loadTrips(trip.id); notify('Trip created')
  }
  async function addPlan(input: Omit<Plan, 'id' | 'tripId' | 'createdBy'>) {
    if (!activeTripID) return
    await api.createPlan(activeTripID, input); setSheet(null); await loadDetail(activeTripID); notify('Plan added')
  }
  async function addRouteOption(input: Omit<RouteOption, 'id' | 'tripId' | 'createdBy' | 'createdAt'>) {
    if (!activeTripID) return
    await api.createRouteOption(activeTripID, input); setSheet(null); await loadDetail(activeTripID); notify('Route option saved')
  }
  async function toggleChecklist(item: ChecklistItem) {
    if (!activeTripID) return
    try { await api.updateChecklist(activeTripID, { ...item, isComplete: !item.isComplete }); await loadDetail(activeTripID) } catch (err) { setError(message(err)) }
  }
  async function addChecklist(title: string) {
    if (!activeTripID || !detail) return
    await api.createChecklist(activeTripID, { title, isComplete: false, sortOrder: detail.checklist.length }); await loadDetail(activeTripID); notify('Checklist item added')
  }

  const profileName = session.user.user_metadata.full_name || session.user.email?.split('@')[0] || 'Traveller'
  if (loading && !detail) return <LoadingScreen />
  return <main className="app-shell production-shell">
    <header className="topbar"><div className="avatar">{initials(profileName)}</div><label className="trip-picker"><Globe2 size={15} /><select value={activeTripID} onChange={(event) => setActiveTripID(event.target.value)} aria-label="Active trip">{trips.map((trip) => <option key={trip.id} value={trip.id}>{trip.title}</option>)}</select><ChevronDown size={15} /></label><button className="icon-button" onClick={() => supabase?.auth.signOut()} aria-label="Sign out"><LogOut size={18} /></button></header>
    {error && <div className="error-banner">{error}<button onClick={() => setError('')} aria-label="Dismiss error"><X size={15} /></button></div>}
    {view === 'inbox' ? <Inbox trips={trips} notify={notify} /> : tripListFailed ? <TripListState error={error} onRetry={() => loadTrips()} /> : !activeTripID ? <EmptyTrips onCreate={() => setSheet('trip')} /> : !detail ? <TripDetailState error={error} onRetry={() => loadDetail(activeTripID)} /> : <>
      <TripHeader trip={detail.trip} checklist={detail.checklist} />
      <nav className="section-tabs" aria-label="Trip sections"><button className={view === 'overview' ? 'active' : ''} onClick={() => setView('overview')}>Overview</button><button className={view === 'plans' ? 'active' : ''} onClick={() => setView('plans')}>Plans</button><button className={view === 'calendar' ? 'active' : ''} onClick={() => setView('calendar')}>Calendar</button><button className={view === 'imports' ? 'active' : ''} onClick={() => setView('imports')}>Imports</button><button className={view === 'routes' ? 'active' : ''} onClick={() => setView('routes')}>Routes</button><button className={view === 'documents' ? 'active' : ''} onClick={() => setView('documents')}>Documents</button><button className={view === 'members' ? 'active' : ''} onClick={() => setView('members')}>People</button></nav>
      <section className="content">{view === 'overview' && <Overview detail={detail} onToggle={toggleChecklist} onAddChecklist={addChecklist} onViewChange={setView} />}{view === 'plans' && <Plans detail={detail} onAdd={() => setSheet('plan')} />}{view === 'calendar' && <CalendarView plans={detail.plans} />}{view === 'imports' && <Imports tripID={activeTripID} notify={notify} />}{view === 'routes' && <RouteOptions options={detail.routeOptions} onAdd={() => setSheet('route')} />}{view === 'documents' && <Documents detail={detail} onReload={() => loadDetail(activeTripID)} notify={notify} />}{view === 'members' && <Members detail={detail} tripID={activeTripID} onReload={() => loadDetail(activeTripID)} notify={notify} />}</section>
      <button className="fab" onClick={() => setSheet(view === 'routes' ? 'route' : 'plan')} aria-label={view === 'routes' ? 'Add a route option' : 'Add a plan'}><Plus size={25} /></button>
    </>}
    <nav className="bottom-nav"><button className={view === 'overview' ? 'active' : ''} onClick={() => setView('overview')}><Globe2 size={22} /><span>Trip</span></button><button className={view === 'plans' ? 'active' : ''} onClick={() => setView('plans')}><CalendarDays size={22} /><span>Plans</span></button><button className={view === 'inbox' ? 'active' : ''} onClick={() => setView('inbox')}><PackageCheck size={22} /><span>Inbox</span></button><button className={view === 'routes' ? 'active' : ''} onClick={() => setView('routes')}><Route size={22} /><span>Routes</span></button><button className={view === 'documents' ? 'active' : ''} onClick={() => setView('documents')}><FileText size={22} /><span>Documents</span></button></nav>
    {sheet === 'trip' && <CreateTripSheet onClose={() => setSheet(null)} onSubmit={createTrip} />}{sheet === 'plan' && <CreatePlanSheet onClose={() => setSheet(null)} onSubmit={addPlan} />}{sheet === 'route' && <CreateRouteSheet onClose={() => setSheet(null)} onSubmit={addRouteOption} />}{toast && <div className="toast"><Check size={16} /> {toast}</div>}
  </main>
}

function TripHeader({ trip, checklist }: { trip: Trip; checklist: ChecklistItem[] }) {
  const complete = checklist.filter((item) => item.isComplete).length
  return <section className="trip-summary"><div className="summary-copy"><p className="eyebrow">ACTIVE TRIP</p><h1>{trip.destination}</h1><p className="trip-dates"><CalendarDays size={15} /> {formatDateRange(trip.startDate, trip.endDate)} <span>·</span> {nights(trip.startDate, trip.endDate)} nights</p></div><div className="stamp"><svg viewBox="0 0 42 42" aria-hidden="true"><circle className="stamp-track" cx="21" cy="21" r="17" /><circle className="stamp-value" cx="21" cy="21" r="17" pathLength="100" style={{ strokeDasharray: `${checklist.length ? complete / checklist.length * 100 : 0} 100` }} /></svg><strong>{complete}/{checklist.length}</strong><small>ready</small></div></section>
}

function TripDetailState({ error, onRetry }: { error: string; onRetry: () => void }) {
  return <section className="empty-trips"><span className="brand-mark">W</span><p className="eyebrow">TRIP DETAILS</p><h1>{error ? 'We couldn’t load this trip.' : 'Loading your trip…'}</h1><p>{error || 'Getting your itinerary and travel details ready.'}</p>{error && <button className="save-button" onClick={onRetry}>Try again</button>}</section>
}

function TripListState({ error, onRetry }: { error: string; onRetry: () => void }) {
  return <section className="empty-trips"><span className="brand-mark">W</span><p className="eyebrow">YOUR TRAVEL SPACE</p><h1>We couldn’t load your trips.</h1><p>{error}</p><button className="save-button" onClick={onRetry}>Try again</button></section>
}

function Overview({ detail, onToggle, onAddChecklist, onViewChange }: { detail: TripDetail; onToggle: (item: ChecklistItem) => void; onAddChecklist: (title: string) => Promise<void>; onViewChange: (view: View) => void }) {
  const next = detail.plans.find((plan) => new Date(plan.startsAt) >= new Date()) || detail.plans[0]
  return <>{next ? <section className="hero-card"><div className="hero-card-top"><span className="live-pill"><i /> NEXT PLAN</span><span>{formatDay(next.startsAt)}</span></div><div className="hero-time"><strong>{next.title}</strong><span>{formatTime(next.startsAt)}</span></div><div className="hero-detail"><span className={`timeline-dot ${kinds[next.kind].color}`}>{iconFor(next.kind, 18)}</span><div><b>{kinds[next.kind].label}</b><small>{next.location || 'Location to be added'}</small></div></div><div className="airline-row"><span>{next.confirmationCode ? `Confirmation · ${next.confirmationCode}` : 'No confirmation code added'}</span><button onClick={() => onViewChange('plans')}>All plans <ChevronDown size={14} /></button></div></section> : <section className="empty-card"><CalendarDays size={25} /><b>No plans yet</b><p>Start with the thing you cannot afford to miss.</p><button onClick={() => onViewChange('plans')}>Open plans</button></section>}
  <section className="quick-actions"><button onClick={() => onViewChange('documents')}><span className="quick-icon coral"><FolderOpen size={20} /></span>Documents</button><button onClick={() => onViewChange('plans')}><span className="quick-icon sea"><CalendarDays size={20} /></span>Itinerary</button><button onClick={() => onViewChange('members')}><span className="quick-icon sun"><UsersRound size={20} /></span>Travel mates</button><button onClick={() => onViewChange('plans')}><span className="quick-icon lilac"><MapPin size={20} /></span>Places</button></section>
  <section className="section-heading"><div><p className="eyebrow">ITINERARY</p><h2>What’s next</h2></div><button className="text-button" onClick={() => onViewChange('plans')}>All plans <ChevronDown size={15} /></button></section><Timeline plans={detail.plans.slice(0, 3)} />
  <Checklist checklist={detail.checklist} onToggle={onToggle} onAdd={onAddChecklist} />
  </>
}

function Timeline({ plans }: { plans: Plan[] }) {
  if (!plans.length) return null
  return <section className="timeline">{plans.map((plan, index) => <div className="timeline-item" key={plan.id}><time>{formatTime(plan.startsAt)}<small>{formatDay(plan.startsAt)}</small></time><span className={`timeline-dot ${kinds[plan.kind].color}`}>{iconFor(plan.kind, 17)}</span><span className="timeline-copy"><b>{plan.title}</b><small>{plan.location || kinds[plan.kind].label}</small></span>{index < plans.length - 1 && <i className="timeline-line" />}</div>)}</section>
}

function Checklist({ checklist, onToggle, onAdd }: { checklist: ChecklistItem[]; onToggle: (item: ChecklistItem) => void; onAdd: (title: string) => Promise<void> }) {
  const [newItem, setNewItem] = useState(''); const [saving, setSaving] = useState(false)
  const complete = checklist.filter((item) => item.isComplete).length
  async function submit(event: FormEvent) { event.preventDefault(); if (!newItem.trim()) return; setSaving(true); try { await onAdd(newItem.trim()); setNewItem('') } finally { setSaving(false) } }
  return <section className="readiness-card"><div className="readiness-head"><div><p className="eyebrow">TRIP READINESS</p><h2>Before you go</h2></div><span>{complete} of {checklist.length}</span></div><div className="progress"><i style={{ width: `${checklist.length ? complete / checklist.length * 100 : 0}%` }} /></div><div className="checklist-list">{checklist.map((item) => <button key={item.id} onClick={() => onToggle(item)} className={item.isComplete ? 'checked' : ''}><span>{item.isComplete && <Check size={13} />}</span>{item.title}</button>)}</div><form className="inline-form" onSubmit={submit}><input value={newItem} onChange={(event) => setNewItem(event.target.value)} placeholder="Add a reminder" /><button type="submit" disabled={!newItem.trim() || saving} aria-label="Add checklist item"><Plus size={16} /></button></form></section>
}

function Plans({ detail, onAdd }: { detail: TripDetail; onAdd: () => void }) { return <><section className="plans-title"><div><p className="eyebrow">YOUR SCHEDULE</p><h2>{detail.trip.title}</h2></div><button className="round-add" onClick={onAdd} aria-label="Add a plan"><Plus size={19} /></button></section>{detail.plans.length ? <Timeline plans={detail.plans} /> : <EmptyPanel icon={<CalendarDays />} title="No plans yet" description="Add your transport, stays, bookings, and things you want to do." />}</> }

const routeLabels: Record<RouteOptionType, string> = { direct_flight: 'Direct flight', flight_train: 'Flight + train', train: 'Train', bus: 'Bus', other: 'Other' }
const routeStatusLabels: Record<RouteOptionStatus, string> = { considering: 'Considering', shortlisted: 'Shortlisted', booked: 'Booked', dismissed: 'Dismissed' }

function RouteOptions({ options, onAdd }: { options: RouteOption[]; onAdd: () => void }) {
  const usablePrices = options.filter((option) => option.priceAmount !== undefined)
  const cheapest = usablePrices.length ? usablePrices.reduce((lowest, option) => option.priceAmount! < lowest.priceAmount! ? option : lowest) : undefined
  const usableDurations = options.filter((option) => option.durationMinutes !== undefined)
  const fastest = usableDurations.length ? usableDurations.reduce((shortest, option) => option.durationMinutes! < shortest.durationMinutes! ? option : shortest) : undefined
  return <><section className="plans-title"><div><p className="eyebrow">YOUR RESEARCH</p><h2>Route options</h2></div><button className="round-add" onClick={onAdd} aria-label="Add route option"><Plus size={19} /></button></section><p className="route-intro">Save the routes you find, then compare trade-offs before booking. Nothing is searched or booked here.</p>{options.length ? <><section className="route-insights">{cheapest && <div><span>LOWEST PRICE</span><b>{formatPrice(cheapest)}</b><small>{cheapest.title}</small></div>}{fastest && <div><span>FASTEST</span><b>{formatDuration(fastest.durationMinutes)}</b><small>{fastest.title}</small></div>}</section><section className="route-list">{options.map((option) => <article className={`route-card ${option.status}`} key={option.id}><div className="route-card-head"><span className="route-type"><Route size={15} /> {routeLabels[option.routeType]}</span><span className={`route-status ${option.status}`}>{routeStatusLabels[option.status]}</span></div><h3>{option.title}</h3>{option.origin || option.destination ? <p className="route-place"><MapPin size={13} /> {option.origin || 'Origin'} <i /> {option.destination || 'Destination'}</p> : null}<div className="route-metrics"><span>{option.priceAmount !== undefined ? formatPrice(option) : 'Price unknown'}<small>price</small></span><span>{formatDuration(option.durationMinutes)}<small>duration</small></span><span>{option.transfers === 0 ? 'Direct' : `${option.transfers} transfer${option.transfers === 1 ? '' : 's'}`}<small>changes</small></span></div>{option.notes && <p className="route-notes">{option.notes}</p>}{option.bookingUrl && <a href={option.bookingUrl} target="_blank" rel="noreferrer">Open saved link</a>}</article>)}</section></> : <EmptyPanel icon={<Route />} title="No routes compared yet" description="Add the options you find: direct flight, flight plus train, train-only, or anything else." />}</>
}

function Inbox({ trips, notify }: { trips: Trip[]; notify: (message: string) => void }) {
  const [items, setItems] = useState<ReservationImport[]>([])
  const [selected, setSelected] = useState<ImportDetail | null>(null)
  const [error, setError] = useState('')
  async function refresh() { try { setItems(await api.listInbox()) } catch (err) { setError(message(err)) } }
  useEffect(() => { refresh() }, [])
  async function open(id: string) { try { setSelected(await api.getImport(id)) } catch (err) { setError(message(err)) } }
  async function assign(tripID: string) { if (!selected) return; try { await api.assignImport(selected.import.id, tripID); setSelected(await api.getImport(selected.import.id)); await refresh(); notify('Pending task assigned to trip') } catch (err) { setError(message(err)) } }
  async function retry() { if (!selected) return; try { await api.retryImport(selected.import.id); setSelected(await api.getImport(selected.import.id)); await refresh(); notify('Extraction queued for retry') } catch (err) { setError(message(err)) } }
  return <><section className="plans-title"><div><p className="eyebrow">FORWARD & REVIEW</p><h2>Incoming tasks</h2></div><PackageCheck className="shield" size={25} /></section><p className="route-intro">Forward a confirmation to your central inbox. Review and assign it before it changes any trip.</p>{error && <p className="form-error">{error}</p>}{selected ? <section className="import-detail"><button className="text-button" onClick={() => setSelected(null)}>‹ All tasks</button><p className="eyebrow">{selected.import.sender}</p><h2>{selected.import.subject || 'Forwarded reservation'}</h2><p className="import-status">{selected.import.status}{selected.import.usedLlm ? ' · assisted extraction' : ''}{selected.import.duplicateOfImportId ? ' · possible duplicate' : ''}</p>{selected.import.status === 'failed' && <button className="text-button" onClick={retry}>Retry extraction</button>}{selected.import.duplicateOfImportId && <p className="route-intro">This looks similar to an earlier booking. Compare both before approving.</p>}<label className="form-label">ASSIGN TO A TRIP<select defaultValue={selected.import.tripId || ''} onChange={(event) => event.target.value && assign(event.target.value)}><option value="">Choose a trip…</option>{trips.map((trip) => <option key={trip.id} value={trip.id}>{trip.title} · {trip.destination}</option>)}</select></label>{selected.drafts.map((draft) => <article className="import-draft" key={draft.id}><span className={`timeline-dot ${kinds[draft.kind].color}`}>{iconFor(draft.kind, 16)}</span><div><b>{draft.title}</b><small>{draft.supplier || 'Reservation proposal'} · {Math.round(draft.confidence * 100)}% confidence</small><p>{draft.startsAt ? `${formatDay(draft.startsAt)} ${formatTime(draft.startsAt)}` : 'Time needs review'}{draft.location ? ` · ${draft.location}` : ''}</p></div></article>)}</section> : items.length ? <section className="import-list">{items.map((item) => <button key={item.id} onClick={() => open(item.id)}><span className="doc-icon sea"><PackageCheck size={18} /></span><span><b>{item.subject || 'Forwarded reservation'}</b><small>{item.sender} · {formatDay(item.createdAt)}</small></span><em>{item.status}{item.duplicateOfImportId ? ' · duplicate?' : ''}</em></button>)}</section> : <EmptyPanel icon={<PackageCheck />} title="Inbox is clear" description="Forward a booking confirmation to your central address and it will appear here for review." />}</>
}

function Imports({ tripID, notify }: { tripID: string; notify: (message: string) => void }) {
  const [items,setItems]=useState<ReservationImport[]>([]);const [address,setAddress]=useState('');const [selected,setSelected]=useState<ImportDetail | null>(null);const [error,setError]=useState('')
  async function refresh(){try{setItems(await api.listImports(tripID))}catch(err){setError(message(err))}}
  useEffect(()=>{setSelected(null);setAddress('');refresh()},[tripID])
  async function forwardingAddress(){try{const result=await api.getImportAddress(tripID);setAddress(result.address);await navigator.clipboard?.writeText(result.address);notify('Forwarding address copied')}catch(err){setError(message(err))}}
  async function open(id:string){try{setSelected(await api.getImport(id))}catch(err){setError(message(err))}}
  async function approve(draft:ImportDetail['drafts'][number]){if(!selected||!draft.startsAt){setError('Add a start time in the plan form before approving this draft.');return};try{await api.approveDraft(selected.import.id,draft);await refresh();setSelected(await api.getImport(selected.import.id));notify('Reservation added to your itinerary')}catch(err){setError(message(err))}}
  async function discard(draft:ImportDetail['drafts'][number]){if(!selected)return;try{await api.discardDraft(selected.import.id,draft.id);setSelected(await api.getImport(selected.import.id));await refresh();notify('Draft discarded')}catch(err){setError(message(err))}}
  return <><section className="plans-title"><div><p className="eyebrow">FORWARD & REVIEW</p><h2>Reservation imports</h2></div><button className="round-add" onClick={forwardingAddress} aria-label="Get forwarding address"><Plus size={19}/></button></section><p className="route-intro">Forward booking emails here. Waypoint creates a private draft; nothing reaches the itinerary until you approve it.</p>{address&&<button className="forwarding-address" onClick={()=>navigator.clipboard?.writeText(address)}><span>FORWARD TO THIS TRIP</span><b>{address}</b><small>Tap to copy</small></button>}{error&&<p className="form-error">{error}</p>}{selected?<section className="import-detail"><button className="text-button" onClick={()=>setSelected(null)}>‹ All imports</button><p className="eyebrow">{selected.import.sender}</p><h2>{selected.import.subject}</h2><p className="import-status">{selected.import.status}{selected.import.usedLlm?' · assisted extraction':''}</p>{selected.drafts.map((draft)=><article className="import-draft" key={draft.id}><span className={`timeline-dot ${kinds[draft.kind].color}`}>{iconFor(draft.kind,16)}</span><div><b>{draft.title}</b><small>{draft.supplier||'Reservation draft'} · {Math.round(draft.confidence*100)}% confidence</small><p>{draft.startsAt?`${formatDay(draft.startsAt)} ${formatTime(draft.startsAt)}`:'Time needs review'}{draft.location?` · ${draft.location}`:''}</p></div>{draft.status==='pending'&&<aside><button onClick={()=>approve(draft)} disabled={!draft.startsAt}>Approve</button><button onClick={()=>discard(draft)}>Discard</button></aside>}</article>)}</section>:items.length?<section className="import-list">{items.map((item)=><button key={item.id} onClick={()=>open(item.id)}><span className="doc-icon sea"><FileText size={18}/></span><span><b>{item.subject||'Reservation email'}</b><small>{item.sender} · {formatDay(item.createdAt)}</small></span><em>{item.status}</em></button>)}</section>:<EmptyPanel icon={<FileText/>} title="No forwarded reservations" description="Create the address above, then forward an airline, hotel, train, or ticket confirmation."/>}</>
}

function CalendarView({ plans }: { plans: Plan[] }) { const [status,setStatus]=useState<{connected:boolean;email?:string;lastSyncedAt?:string;lastError?:string}>({connected:false});const [error,setError]=useState('');async function refresh(){try{setStatus(await api.calendarStatus())}catch(err){setError(message(err))}}useEffect(()=>{refresh()},[]);async function connect(){try{const result=await api.calendarConnect();window.location.assign(result.url)}catch(err){setError(message(err))}}async function sync(){try{await api.calendarSync();await refresh()}catch(err){setError(message(err))}}return <><section className="plans-title"><div><p className="eyebrow">YOUR SCHEDULE</p><h2>Trip calendar</h2></div><CalendarDays className="shield" size={25}/></section><section className="calendar-connection"><div><b>{status.connected?'Google Calendar connected':'Keep plans in sync'}</b><p>{status.connected?`${status.email||'Dedicated Waypoint calendar'}${status.lastSyncedAt?` · synced ${formatDay(status.lastSyncedAt)}`:''}`:'Connect a separate Waypoint calendar. Personal calendars stay private.'}</p></div><button onClick={status.connected?sync:connect}>{status.connected?'Sync now':'Connect'}</button></section>{error&&<p className="form-error">{error}</p>}<section className="calendar-agenda">{plans.length?plans.map((plan)=><div key={plan.id}><time>{formatDay(plan.startsAt)}<small>{formatTime(plan.startsAt)}</small></time><span className={`timeline-dot ${kinds[plan.kind].color}`}>{iconFor(plan.kind,16)}</span><p><b>{plan.title}</b><small>{plan.location||kinds[plan.kind].label}</small></p></div>):<EmptyPanel icon={<CalendarDays/>} title="No calendar events" description="Add itinerary plans to see them in your trip calendar."/>}</section></> }

function Documents({ detail, onReload, notify }: { detail: TripDetail; onReload: () => Promise<void>; notify: (message: string) => void }) {
  const upload = useRef<HTMLInputElement>(null); const [busy, setBusy] = useState(false); const [error, setError] = useState('')
  async function uploadDocument(file: File) { if (!supabase) return; setError(''); if (!['application/pdf','image/jpeg','image/png','image/webp'].includes(file.type) || file.size > 10 * 1024 * 1024) { setError('Use a PDF, JPEG, PNG, or WebP under 10 MB.'); return }; setBusy(true); const path = `${detail.trip.id}/${crypto.randomUUID()}-${file.name.replace(/[^a-zA-Z0-9._-]/g, '-')}`; const { error: storageError } = await supabase.storage.from('trip-documents').upload(path, file, { contentType: file.type, upsert: false }); if (storageError) { setError(storageError.message); setBusy(false); return }; try { await api.createDocument(detail.trip.id, { name: file.name, storagePath: path, contentType: file.type, sizeBytes: file.size }); await onReload(); notify('Document saved to your private wallet') } catch (err) { await supabase.storage.from('trip-documents').remove([path]); setError(message(err)) } finally { setBusy(false) } }
  async function openDocument(doc: Document) { if (!supabase) return; const { data, error } = await supabase.storage.from('trip-documents').createSignedUrl(doc.storagePath, 60); if (error || !data) { setError(error?.message || 'Could not open document'); return }; window.open(data.signedUrl, '_blank', 'noopener,noreferrer') }
  return <><section className="plans-title"><div><p className="eyebrow">PRIVATE & OFFLINE-READY</p><h2>Travel wallet</h2></div><ShieldCheck className="shield" size={25} /></section><section className="wallet-note"><span><ShieldCheck size={19} /></span><div><b>Private by default</b><p>Only members of this trip can open these documents.</p></div></section><button className="upload-zone" onClick={() => upload.current?.click()} disabled={busy}>{busy ? <LoaderCircle className="spin" /> : <Upload />}<span>{busy ? 'Saving document…' : 'Add a confirmation or document'}</span><small>PDF, JPEG, PNG, or WebP · up to 10 MB</small></button><input ref={upload} className="visually-hidden" type="file" accept="application/pdf,image/jpeg,image/png,image/webp" onChange={(event) => { const file = event.target.files?.[0]; if (file) uploadDocument(file); event.target.value = '' }} />{error && <p className="form-error">{error}</p>}<p className="group-label">YOUR DOCUMENTS</p>{detail.documents.length ? <section className="doc-list">{detail.documents.map((doc) => <button className="doc-row" key={doc.id} onClick={() => openDocument(doc)}><span className="doc-icon blue"><FileText size={19} /></span><span><b>{doc.name}</b><small>{formatBytes(doc.sizeBytes)} · {formatDay(doc.createdAt)}</small></span><em>Open</em></button>)}</section> : <EmptyPanel icon={<FolderOpen />} title="Nothing stored yet" description="Keep boarding passes, hotel bookings, insurance, and tickets here." />}</> }

function Members({ detail, tripID, onReload, notify }: { detail: TripDetail; tripID: string; onReload: () => Promise<void>; notify: (message: string) => void }) {
  const [email, setEmail] = useState(''); const [busy, setBusy] = useState(false); const [error, setError] = useState('')
  async function invite(event: FormEvent) { event.preventDefault(); if (!email) return; setBusy(true); setError(''); try { await api.addMember(tripID, email); setEmail(''); await onReload(); notify('Travel mate added') } catch (err) { setError(message(err)) } finally { setBusy(false) } }
  return <><section className="plans-title"><div><p className="eyebrow">SHARED TRIP</p><h2>Travel mates</h2></div><UsersRound className="shield" size={25} /></section><section className="member-list">{detail.members.map((member) => <div key={member.id}><span className="member-avatar">{initials(member.displayName || member.email)}</span><span><b>{member.displayName || member.email}</b><small>{member.email}</small></span><em>{member.role}</em></div>)}</section><form className="invite-form" onSubmit={invite}><label className="form-label">INVITE A FRIEND<input type="email" value={email} onChange={(event) => setEmail(event.target.value)} placeholder="friend@example.com" required /></label><button className="save-button" disabled={busy}>{busy ? 'Adding…' : 'Add travel mate'}</button></form>{error && <p className="form-error">{error}</p>}<p className="invite-note">They need to sign in with Google once before you can add them.</p></> }

function CreateTripSheet({ onClose, onSubmit }: { onClose: () => void; onSubmit: (input: { title: string; destination: string; startDate: string; endDate: string }) => Promise<void> }) { const [busy,setBusy]=useState(false); const [error,setError]=useState(''); async function submit(event:FormEvent<HTMLFormElement>){event.preventDefault();const form=new FormData(event.currentTarget);setBusy(true);setError('');try{await onSubmit({title:String(form.get('title')),destination:String(form.get('destination')),startDate:String(form.get('startDate')),endDate:String(form.get('endDate'))})}catch(err){setError(message(err));setBusy(false)}} return <Sheet title="Create a trip" eyebrow="START A NEW ADVENTURE" onClose={onClose}><form onSubmit={submit}><label className="form-label">TRIP NAME<input name="title" placeholder="Barcelona weekend" required maxLength={100} /></label><label className="form-label">DESTINATION<input name="destination" placeholder="Barcelona, Spain" required maxLength={120} /></label><div className="input-pair"><label className="form-label">START<input name="startDate" type="date" required /></label><label className="form-label">END<input name="endDate" type="date" required /></label></div>{error&&<p className="form-error">{error}</p>}<button className="save-button" disabled={busy}>{busy?'Creating…':'Create trip'}</button></form></Sheet> }
function CreatePlanSheet({ onClose, onSubmit }: { onClose: () => void; onSubmit: (input: Omit<Plan, 'id' | 'tripId' | 'createdBy'>) => Promise<void> }) { const [busy,setBusy]=useState(false); const [error,setError]=useState(''); const [kind,setKind]=useState<PlanKind>('flight'); async function submit(event:FormEvent<HTMLFormElement>){event.preventDefault();const form=new FormData(event.currentTarget);setBusy(true);setError('');try{await onSubmit({kind,title:String(form.get('title')),startsAt:new Date(String(form.get('startsAt'))).toISOString(),endsAt:form.get('endsAt')?new Date(String(form.get('endsAt'))).toISOString():undefined,location:String(form.get('location')),confirmationCode:String(form.get('confirmationCode')),notes:String(form.get('notes'))})}catch(err){setError(message(err));setBusy(false)}} return <Sheet title="New plan" eyebrow="ADD TO YOUR ITINERARY" onClose={onClose}><form onSubmit={submit}><div className="type-grid">{(Object.keys(kinds) as PlanKind[]).slice(0,4).map((value)=>{const Icon=kinds[value].icon;return <button type="button" key={value} className={kind===value?'selected':''} onClick={()=>setKind(value)}><Icon size={21}/>{kinds[value].label}</button>})}</div><label className="form-label">WHAT'S HAPPENING?<input name="title" placeholder="Flight to Barcelona" required maxLength={180} /></label><label className="form-label">START TIME<input name="startsAt" type="datetime-local" required /></label><label className="form-label">LOCATION<input name="location" placeholder="Airport, hotel, venue…" /></label><label className="form-label">CONFIRMATION CODE<input name="confirmationCode" placeholder="Optional" /></label><input type="hidden" name="endsAt" />{error&&<p className="form-error">{error}</p>}<button className="save-button" disabled={busy}>{busy?'Adding…':`Add ${kinds[kind].label.toLowerCase()}`}</button></form></Sheet> }
function CreateRouteSheet({ onClose, onSubmit }: { onClose: () => void; onSubmit: (input: Omit<RouteOption, 'id' | 'tripId' | 'createdBy' | 'createdAt'>) => Promise<void> }) { const [busy,setBusy]=useState(false); const [error,setError]=useState(''); const [routeType,setRouteType]=useState<RouteOptionType>('direct_flight'); async function submit(event:FormEvent<HTMLFormElement>){event.preventDefault();const form=new FormData(event.currentTarget);const price=String(form.get('price')).trim();const duration=String(form.get('duration')).trim();setBusy(true);setError('');try{await onSubmit({title:String(form.get('title')),routeType,origin:String(form.get('origin')),destination:String(form.get('destination')),durationMinutes:duration?Number(duration):undefined,transfers:Number(form.get('transfers')||0),priceAmount:price?Number(price):undefined,currency:price?String(form.get('currency')).toUpperCase():undefined,bookingUrl:String(form.get('bookingUrl')),notes:String(form.get('notes')),status:String(form.get('status')) as RouteOptionStatus});}catch(err){setError(message(err));setBusy(false)}} return <Sheet title="Compare a route" eyebrow="LOG WHAT YOU FOUND" onClose={onClose}><form onSubmit={submit}><div className="route-type-grid">{(Object.keys(routeLabels) as RouteOptionType[]).map((value)=><button type="button" key={value} className={routeType===value?'selected':''} onClick={()=>setRouteType(value)}>{routeLabels[value]}</button>)}</div><label className="form-label">OPTION NAME<input name="title" placeholder="EasyJet + Renfe via Madrid" required maxLength={180}/></label><div className="input-pair"><label className="form-label">FROM<input name="origin" placeholder="London"/></label><label className="form-label">TO<input name="destination" placeholder="Barcelona"/></label></div><div className="route-input-row"><label className="form-label">PRICE<input name="price" inputMode="decimal" placeholder="79.50"/></label><label className="form-label">CURRENCY<input name="currency" defaultValue="EUR" maxLength={3}/></label><label className="form-label">MINUTES<input name="duration" inputMode="numeric" placeholder="165"/></label></div><label className="form-label">TRANSFERS<input name="transfers" type="number" min="0" max="20" defaultValue="0" /></label><label className="form-label">SAVED BOOKING LINK<input name="bookingUrl" type="url" placeholder="https://…" /></label><label className="form-label">WHY CONSIDER IT?<input name="notes" placeholder="Cheapest, but an early airport transfer." /></label><label className="form-label">STATUS<select name="status" defaultValue="considering"><option value="considering">Considering</option><option value="shortlisted">Shortlisted</option><option value="booked">Booked</option><option value="dismissed">Dismissed</option></select></label>{error&&<p className="form-error">{error}</p>}<button className="save-button" disabled={busy}>{busy?'Saving…':'Save route option'}</button></form></Sheet> }
function Sheet({ eyebrow,title,onClose,children }:{eyebrow:string;title:string;onClose:()=>void;children:ReactNode}){return <div className="overlay" onMouseDown={onClose}><section className="sheet" onMouseDown={(event)=>event.stopPropagation()}><div className="sheet-handle"/><div className="sheet-heading"><div><p className="eyebrow">{eyebrow}</p><h2>{title}</h2></div><button className="close-button" onClick={onClose} aria-label="Close"><X size={20}/></button></div>{children}</section></div>}
function EmptyTrips({ onCreate }:{onCreate:()=>void}){return <section className="empty-trips"><span className="brand-mark">W</span><p className="eyebrow">YOUR TRAVEL SPACE</p><h1>Start your first shared trip.</h1><p>Bring your bookings, plans, and travel documents into one place before the group chat gets chaotic.</p><button className="save-button" onClick={onCreate}><Plus size={18}/> Create a trip</button></section>}
function EmptyPanel({ icon,title,description }:{icon:ReactNode;title:string;description:string}){return <section className="empty-panel"><span>{icon}</span><b>{title}</b><p>{description}</p></section>}
function LoadingScreen(){return <main className="loading-screen"><span className="brand-mark">W</span><LoaderCircle className="spin" size={22}/></main>}

function iconFor(kind:PlanKind,size:number){const Icon=kinds[kind].icon;return <Icon size={size}/>}
function initials(value:string){return value.split(/\s+/).slice(0,2).map((part)=>part[0]).join('').toUpperCase()}
function message(error:unknown){return error instanceof Error?error.message:'Something went wrong'}
function toAPIDate(value:string){return new Date(`${value}T00:00:00.000Z`).toISOString()}
function formatDay(value:string){return new Intl.DateTimeFormat(undefined,{day:'numeric',month:'short'}).format(new Date(value))}
function formatTime(value:string){return new Intl.DateTimeFormat(undefined,{hour:'numeric',minute:'2-digit'}).format(new Date(value))}
function formatDateRange(start:string,end:string){return `${formatDay(start)} – ${formatDay(end)}`}
function nights(start:string,end:string){return Math.max(0,Math.round((new Date(end).getTime()-new Date(start).getTime())/86400000))}
function formatBytes(bytes:number){return bytes<1024*1024?`${Math.ceil(bytes/1024)} KB`:`${(bytes/1024/1024).toFixed(1)} MB`}
function formatDuration(minutes?:number){if(minutes===undefined)return 'Time unknown';const hours=Math.floor(minutes/60);const remainder=minutes%60;return hours?`${hours}h ${remainder?`${remainder}m`:''}`:`${remainder}m`}
function formatPrice(option:RouteOption){return option.priceAmount===undefined?'Price unknown':new Intl.NumberFormat(undefined,{style:'currency',currency:option.currency||'EUR'}).format(option.priceAmount)}
