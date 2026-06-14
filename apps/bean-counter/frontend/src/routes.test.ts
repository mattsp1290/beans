import { describe, expect, it } from 'vitest'

import { getRoute, routes } from './routes'

describe('routes', () => {
  it('maps issue list, detail, and edit paths to the issues route metadata', () => {
    expect(getRoute('/')).toBe(routes[0])
    expect(getRoute('/issues')).toBe(routes[0])
    expect(getRoute('/issues/bc-1')).toBe(routes[0])
    expect(getRoute('/issues/bc-1/edit')).toBe(routes[0])
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

  it('falls back to the issue workspace for unknown paths', () => {
    expect(getRoute('/missing')).toBe(routes[0])
  })
})
