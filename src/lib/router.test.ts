import { describe, expect, it } from 'vitest'
import { parsePath, routePath, type Route } from './router'

describe('parsePath', () => {
  it('reads a trip section', () => {
    expect(parsePath('/trip/abc/documents')).toEqual({ name: 'trip', tripId: 'abc', section: 'documents' })
  })

  it('defaults a bare trip path to the overview', () => {
    expect(parsePath('/trip/abc')).toEqual({ name: 'trip', tripId: 'abc', section: 'overview' })
  })

  it('falls back to the overview for an unknown section', () => {
    expect(parsePath('/trip/abc/nonsense')).toEqual({ name: 'trip', tripId: 'abc', section: 'overview' })
  })

  it('reads the top-level areas', () => {
    expect(parsePath('/inbox')).toEqual({ name: 'inbox' })
    expect(parsePath('/search')).toEqual({ name: 'search' })
  })

  it('treats an unrecognised path as home', () => {
    expect(parsePath('/')).toEqual({ name: 'home' })
    expect(parsePath('/trip')).toEqual({ name: 'home' })
    expect(parsePath('/something/else')).toEqual({ name: 'home' })
  })

  it('tolerates trailing slashes', () => {
    expect(parsePath('/trip/abc/plans/')).toEqual({ name: 'trip', tripId: 'abc', section: 'plans' })
  })
})

describe('routePath', () => {
  const cases: Route[] = [
    { name: 'home' },
    { name: 'inbox' },
    { name: 'search' },
    { name: 'trip', tripId: 'abc', section: 'overview' },
    { name: 'trip', tripId: 'abc', section: 'routes' },
  ]

  it('round-trips every route through the path and back', () => {
    for (const route of cases) expect(parsePath(routePath(route))).toEqual(route)
  })

  it('keeps the overview at the bare trip path', () => {
    expect(routePath({ name: 'trip', tripId: 'abc', section: 'overview' })).toBe('/trip/abc')
  })
})
