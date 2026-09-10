import { describe, expect, it } from 'vitest'

import { getRoute, parseIssueId, parseWikiPath } from './routes'

describe('routes', () => {
  it('redirects the root path to the issues board metadata', () => {
    expect(getRoute('/')).toMatchObject({ path: '/issues', title: 'Issues' })
  })

  it('maps issue list and detail paths to the issues route metadata', () => {
    for (const path of ['/issues', '/issues/bc-1', '/issues/bc-a3f2dd']) {
      expect(getRoute(path)).toMatchObject({
        path: '/issues',
        title: 'Issues',
      })
    }
  })

  it('maps ready and graph paths to their workspace metadata', () => {
    expect(getRoute('/ready')).toMatchObject({
      path: '/ready',
      title: 'Ready Queue',
    })
    expect(getRoute('/graph')).toMatchObject({
      path: '/graph',
      title: 'Dependency Graph',
    })
  })

  it('maps wiki index and wiki page paths to the wiki route metadata', () => {
    expect(getRoute('/wiki')).toMatchObject({ path: '/wiki', title: 'Wiki' })
    expect(getRoute('/wiki/projects/beans/README')).toMatchObject({
      path: '/wiki',
      title: 'Wiki',
    })
  })

  it('maps the search path to the search route metadata', () => {
    expect(getRoute('/search')).toMatchObject({ path: '/search', title: 'Search' })
  })

  it('falls back to the issue workspace for unknown paths', () => {
    expect(getRoute('/missing')).toMatchObject({
      path: '/issues',
      title: 'Issues',
    })
  })
})

describe('parseIssueId', () => {
  it('extracts the id from an issue detail path', () => {
    expect(parseIssueId('/issues/bc-1')).toBe('bc-1')
    expect(parseIssueId('/issues/bc-1/')).toBe('bc-1')
  })

  it('decodes URL-encoded ids', () => {
    expect(parseIssueId('/issues/bc%2F1')).toBe('bc/1')
  })

  it('returns null for non-detail issue paths', () => {
    expect(parseIssueId('/issues')).toBeNull()
    expect(parseIssueId('/ready')).toBeNull()
  })
})

describe('parseWikiPath', () => {
  it('returns an empty path for the wiki index', () => {
    expect(parseWikiPath('/wiki')).toBe('')
    expect(parseWikiPath('/wiki/')).toBe('')
  })

  it('extracts nested doc paths', () => {
    expect(parseWikiPath('/wiki/projects/beans/README')).toBe('projects/beans/README')
  })

  it('decodes each URL-encoded path segment', () => {
    expect(parseWikiPath('/wiki/docs/a%20b/c')).toBe('docs/a b/c')
  })
})
