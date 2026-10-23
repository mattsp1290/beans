// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { api, type IssueDetail } from '../../lib/api'
import IssueDetailRoute from './IssueDetailRoute.svelte'

const issue: IssueDetail = {
  id: 'beans-i-1', title: 'Integration issue', type: 'task', status: 'open', priority: 2,
  labels: [], assignee: '', parent: '', blocked_by: [], url: '', created: '2026-10-22T01:00:00Z',
  updated: '2026-10-23T01:00:00Z', project: 'beans', path: 'projects/beans/issues/1.md', archived: false,
  html: '<p>Description</p>', workflow: { statuses: ['open', 'closed'], active: ['open'], terminal: ['closed'] },
  prompts: [
    {
      id: 'beans-p-a1', title: 'Review patch', status: 'open', priority: 1, labels: [], from: 'alice', to: 'agent',
      claimed_by: null, created: '2026-10-22T01:00:00Z', updated: '2026-10-23T01:00:00Z', project: 'beans',
      path: 'projects/beans/prompts/a1.md', has_issue: true,
    },
    { id: 'beans-p-gone', missing: true },
  ],
  requests: [], blockers: [], children: [], log: [],
  backlinks: [
    { from: 'beans-p-a1', kind: 'prompt-issue', note_kind: 'prompt', title: 'Duplicate prompt backlink' },
    { from: 'README', kind: 'wikilink', note_kind: 'doc', path: 'projects/beans/README.md', title: 'Project readme' },
  ],
}

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

describe('IssueDetailRoute prompt integration', () => {
  it('renders linked and missing prompts, suppresses duplicate generic backlinks, and preserves click semantics', async () => {
    vi.spyOn(api, 'getIssue').mockResolvedValue(issue)
    const navigate = vi.fn()
    render(IssueDetailRoute, { id: issue.id, navigate, reloadKey: 0, onBanner: vi.fn() })

    const prompt = await screen.findByRole('link', { name: 'Review patch' })
    expect(prompt.getAttribute('href')).toBe('/prompts/beans-p-a1')
    expect(screen.getByText('beans-p-gone')).toBeTruthy()
    expect(screen.getByText('missing')).toBeTruthy()
    expect(screen.queryByText('Duplicate prompt backlink')).toBeNull()
    expect(screen.getByRole('link', { name: 'Project readme' })).toBeTruthy()

    expect(await fireEvent.click(prompt, { ctrlKey: true })).toBe(true)
    expect(navigate).not.toHaveBeenCalled()
    expect(await fireEvent.click(prompt)).toBe(false)
    expect(navigate).toHaveBeenCalledWith('/prompts/beans-p-a1')
  })

  it('renders the empty prompts state when the issue has no prompt links', async () => {
    vi.spyOn(api, 'getIssue').mockResolvedValue({ ...issue, prompts: [] })
    render(IssueDetailRoute, { id: issue.id, navigate: vi.fn(), reloadKey: 0, onBanner: vi.fn() })
    expect(await screen.findByText('No prompts link to this issue.')).toBeTruthy()
  })
})
