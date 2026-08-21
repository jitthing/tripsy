import { useCallback, useEffect, useState } from 'react'

export type Section = 'overview' | 'plans' | 'calendar' | 'imports' | 'routes' | 'documents' | 'members'

export const sections: Section[] = ['overview', 'plans', 'calendar', 'imports', 'routes', 'documents', 'members']

export const sectionLabels: Record<Section, string> = {
  overview: 'Overview', plans: 'Plans', calendar: 'Calendar', imports: 'Imports',
  routes: 'Routes', documents: 'Documents', members: 'People',
}

export type Route =
  | { name: 'home' }
  | { name: 'trip'; tripId: string; section: Section }
  | { name: 'search' }
  | { name: 'inbox' }

function isSection(value: string): value is Section {
  return (sections as string[]).includes(value)
}

export function parsePath(pathname: string): Route {
  const [first, second, third] = pathname.split('/').filter(Boolean)
  if (first === 'inbox') return { name: 'inbox' }
  if (first === 'search') return { name: 'search' }
  if (first === 'trip' && second) return { name: 'trip', tripId: second, section: third && isSection(third) ? third : 'overview' }
  return { name: 'home' }
}

export function routePath(route: Route): string {
  if (route.name === 'inbox') return '/inbox'
  if (route.name === 'search') return '/search'
  if (route.name === 'trip') return route.section === 'overview' ? `/trip/${route.tripId}` : `/trip/${route.tripId}/${route.section}`
  return '/'
}

export function useRoute(): [Route, (next: Route, replace?: boolean) => void] {
  const [route, setRoute] = useState(() => parsePath(window.location.pathname))
  useEffect(() => {
    const onPop = () => setRoute(parsePath(window.location.pathname))
    window.addEventListener('popstate', onPop)
    return () => window.removeEventListener('popstate', onPop)
  }, [])
  const navigate = useCallback((next: Route, replace = false) => {
    const path = routePath(next)
    // Supabase strips its own OAuth fragment, so only the path is ours to manage.
    if (path !== window.location.pathname) window.history[replace ? 'replaceState' : 'pushState']({}, '', path)
    setRoute(next)
  }, [])
  return [route, navigate]
}
