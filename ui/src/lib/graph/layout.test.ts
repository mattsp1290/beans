import { describe, expect, it } from 'vitest'

import { graphEdgePath, layoutDependencyGraph } from './layout'

describe('dependency graph layout', () => {
  it('positions blockers before blocked issues and keeps edge endpoints attached', () => {
    const layout = layoutDependencyGraph(
      [
        { id: 'bc-2', title: 'Child', state: 'open', priority: 2, labels: ['ui'] },
        { id: 'bc-1', title: 'Parent', state: 'closed', priority: 1, labels: [] },
      ],
      [{ source: 'bc-1', target: 'bc-2' }],
    )

    expect(layout.nodes.map((node) => [node.id, node.level])).toEqual([
      ['bc-1', 0],
      ['bc-2', 1],
    ])
    expect(layout.edges[0].sourceNode.id).toBe('bc-1')
    expect(layout.edges[0].targetNode.id).toBe('bc-2')
    expect(layout.edges[0].sourceNode.x).toBeLessThan(layout.edges[0].targetNode.x)
    expect(graphEdgePath(layout.edges[0])).toContain('C')
  })

  it('drops edges whose endpoints are absent from the graph response', () => {
    const layout = layoutDependencyGraph(
      [{ id: 'bc-1', title: 'Only node', state: 'open', priority: 2, labels: [] }],
      [{ source: 'bc-missing', target: 'bc-1' }],
    )

    expect(layout.nodes).toHaveLength(1)
    expect(layout.edges).toEqual([])
  })

  it('expands the rendered layout for long dependency chains', () => {
    const layout = layoutDependencyGraph(
      [
        { id: 'bc-1', title: 'One', state: 'open', priority: 1, labels: [] },
        { id: 'bc-2', title: 'Two', state: 'open', priority: 1, labels: [] },
        { id: 'bc-3', title: 'Three', state: 'open', priority: 1, labels: [] },
        { id: 'bc-4', title: 'Four', state: 'open', priority: 1, labels: [] },
      ],
      [
        { source: 'bc-1', target: 'bc-2' },
        { source: 'bc-2', target: 'bc-3' },
        { source: 'bc-3', target: 'bc-4' },
      ],
    )

    expect(layout.nodes.map((node) => node.level)).toEqual([0, 1, 2, 3])
    expect(layout.width).toBeGreaterThan(720)
  })
})
