import { describe, expect, it } from 'vitest'
import type { SearchResult } from '../../lib/api'
import { searchResultPath } from './routing'

describe('searchResultPath', () => {
  it('routes prompt hits to encoded prompt detail paths', () => {
    expect(searchResultPath(result({ kind: 'prompt', id: 'beans-p/a1' }))).toBe('/prompts/beans-p%2Fa1')
  })
})

function result(overrides: Partial<SearchResult>): SearchResult {
  return { kind: 'doc', id: '', basename: 'note', title: 'Note', project: 'beans', path: 'docs/note.md', score: 1, ...overrides }
}
