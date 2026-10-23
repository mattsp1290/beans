import { describe, expect, it } from 'vitest'
import type { Backlink, IssuePromptSummary, PromptSummary } from '../../lib/api'
import { render } from 'svelte/server'
import IssuePrompts from './IssuePrompts.svelte'
import { promptHref, withoutPromptBacklinkDuplicates } from './prompts'

describe('issue prompt presentation', () => {
  it('builds encoded client-side prompt destinations', () => {
    expect(promptHref('beans-p/a1')).toBe('/prompts/beans-p%2Fa1')
  })

  it('renders linked and missing prompts with client-router destinations', () => {
    const linked: PromptSummary = {
      id: 'beans-p-a1', title: 'Review patch', status: 'open', priority: 1, labels: [], from: 'alice', to: 'agent',
      claimed_by: null, created: '2026-10-23T01:00:00Z', updated: '2026-10-23T01:00:00Z', project: 'beans', path: 'projects/beans/prompts/a.md', has_issue: true,
    }
    const { body } = render(IssuePrompts, { props: { prompts: [linked, { id: 'beans-p-gone', missing: true }], navigate: () => {} } })
    expect(body).toContain('Review patch')
    expect(body).toContain('href="/prompts/beans-p-a1"')
    expect(body).toContain('beans-p-gone')
    expect(body).toContain('missing')
  })

  it('suppresses generic backlinks represented in the derived Prompts section', () => {
    const prompts = [{ id: 'beans-p-a1' }] as IssuePromptSummary[]
    const backlinks = [
      { from: 'beans-p-a1', kind: 'prompt-issue', note_kind: 'prompt' },
      { from: 'README', kind: 'wikilink', note_kind: 'doc' },
    ] as Backlink[]
    expect(withoutPromptBacklinkDuplicates(backlinks, prompts)).toEqual([backlinks[1]])
  })
})
