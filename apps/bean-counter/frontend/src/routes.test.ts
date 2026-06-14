import { describe, expect, it } from 'vitest'

import { getRoute } from './routes'

describe('routes', () => {
  it('maps issue list, detail, and edit paths to the issues route metadata', () => {
    for (const path of ['/', '/issues', '/issues/bc-1', '/issues/bc-1/edit']) {
      expect(getRoute(path)).toMatchObject({
        path: '/',
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

  it('falls back to the issue workspace for unknown paths', () => {
    expect(getRoute('/missing')).toMatchObject({
      path: '/',
      title: 'Issues',
    })
  })
})
