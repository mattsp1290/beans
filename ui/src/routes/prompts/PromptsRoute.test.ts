// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { api, PROMPT_STATUSES, type PromptDetail, type PromptSummary } from '../../lib/api'
import PromptsRoute from './PromptsRoute.svelte'

const summary: PromptSummary = {
  id: 'beans-p-a1', title: 'Review the patch', status: 'open', priority: 1,
  labels: ['review', 'agent'], from: 'alice', to: 'all', claimed_by: 'bob',
  created: '2026-10-22T01:00:00Z', updated: '2026-10-23T01:00:00Z', project: 'beans',
  path: 'projects/beans/prompts/a1.md', has_issue: true,
}

const detail: PromptDetail = {
  ...summary,
  html: '<h2 id="steps">Steps</h2><p>Read <a href="/prompts/beans-p-b2">the next prompt</a>.</p>',
  toc: [{ level: 2, id: 'steps', text: 'Steps' }],
  backlinks: [{ from: 'beans-i-9', kind: 'reference', note_kind: 'issue', title: 'Parent issue' }],
  outlinks: [],
  log: [{ at: '2026-10-23T01:00:00Z', actor: 'alice', event: 'created', raw: 'initial' }],
  issue: { id: 'beans-i-1', title: 'Linked issue', missing: false },
}

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

describe('PromptsRoute list', () => {
  it('keeps the browser prompt-status contract aligned with the HTTP vocabulary', () => {
    expect(PROMPT_STATUSES).toEqual(['open', 'in_progress', 'completed', 'declined', 'cancelled'])
  })

  it('requests active prompts for all recipients by default, renders every card field, and updates filters', async () => {
    const list = vi.spyOn(api, 'listPrompts').mockResolvedValue([summary])
    const navigate = vi.fn()
    render(PromptsRoute, { project: '_all', id: null, reloadKey: 0, navigate })

    const card = await screen.findByRole('link', { name: /Review the patch/ })
    expect(list).toHaveBeenCalledWith('_all', { q: undefined, status: undefined, to: undefined, terminal: false }, expect.any(AbortSignal))
    expect(card.textContent).toContain('P1')
    expect(card.textContent).toContain('beans-p-a1 · open · beans')
    expect(card.textContent).toContain('alice → all · claimed by bob')
    expect(card.textContent).toContain('review, agent')
    expect(card.textContent).toContain('Linked issue')
    expect(card.textContent).toContain('Updated')
    expect(card.querySelector('time')?.getAttribute('datetime')).toBe(summary.updated)

    await fireEvent.input(screen.getByRole('searchbox', { name: 'Search' }), { target: { value: 'security' } })
    await waitFor(() => expect(list).toHaveBeenLastCalledWith('_all', expect.objectContaining({ q: 'security' }), expect.any(AbortSignal)))
    await fireEvent.change(screen.getByRole('combobox', { name: 'Status' }), { target: { value: 'completed' } })
    await waitFor(() => expect(list).toHaveBeenLastCalledWith('_all', expect.objectContaining({ status: 'completed' }), expect.any(AbortSignal)))
    await fireEvent.input(screen.getByRole('textbox', { name: 'Recipient' }), { target: { value: 'reviewer' } })
    await waitFor(() => expect(list).toHaveBeenLastCalledWith('_all', expect.objectContaining({ to: 'reviewer' }), expect.any(AbortSignal)))
    await fireEvent.click(screen.getByRole('checkbox', { name: 'Include terminal' }))
    await waitFor(() => expect(list).toHaveBeenLastCalledWith('_all', {
      q: 'security', status: 'completed', to: 'reviewer', terminal: true,
    }, expect.any(AbortSignal)))
  })

  it('debounces text filters and aborts a superseded list request', async () => {
    const list = vi.spyOn(api, 'listPrompts')
      .mockResolvedValueOnce([])
      .mockReturnValue(new Promise(() => {}))
    render(PromptsRoute, { project: '_all', id: null, reloadKey: 0, navigate: vi.fn() })
    await waitFor(() => expect(list).toHaveBeenCalledTimes(1))
    await screen.findByRole('region', { name: 'Prompts' })
    const initialSignal = list.mock.calls[0][2]

    const search = screen.getByRole('searchbox', { name: 'Search' })
    await fireEvent.input(search, { target: { value: 's' } })
    await fireEvent.input(search, { target: { value: 'security' } })

    expect(initialSignal?.aborted).toBe(true)
    expect(list).toHaveBeenCalledTimes(1)
    await waitFor(() => expect(list).toHaveBeenCalledTimes(2))
    const supersededSignal = list.mock.calls[1][2]
    await fireEvent.input(search, { target: { value: 'security review' } })
    expect(supersededSignal?.aborted).toBe(true)
    expect(list).toHaveBeenCalledTimes(2)
    await waitFor(() => expect(list).toHaveBeenCalledTimes(3))
    expect(list).toHaveBeenLastCalledWith(
      '_all',
      expect.objectContaining({ q: 'security review' }),
      expect.any(AbortSignal),
    )
  })

  it('routes an ordinary card click but leaves modified and non-primary clicks to the browser', async () => {
    vi.spyOn(api, 'listPrompts').mockResolvedValue([summary])
    const navigate = vi.fn()
    render(PromptsRoute, { project: '_all', id: null, reloadKey: 0, navigate })
    const link = await screen.findByRole('link', { name: /Review the patch/ })

    expect(await fireEvent.click(link, { ctrlKey: true })).toBe(true)
    expect(await fireEvent.click(link, { button: 1 })).toBe(true)
    expect(navigate).not.toHaveBeenCalled()
    expect(await fireEvent.click(link)).toBe(false)
    expect(navigate).toHaveBeenCalledWith('/prompts/beans-p-a1')
  })
})

describe('PromptsRoute detail', () => {
  it('renders metadata, sanitized HTML, linked issue, backlinks, log, toc, and no mutation controls', async () => {
    vi.spyOn(api, 'getPrompt').mockResolvedValue(detail)
    const navigate = vi.fn()
    render(PromptsRoute, { project: 'beans', id: summary.id, reloadKey: 0, navigate })

    const article = await screen.findByRole('article', { name: 'Prompt detail' })
    expect(article.textContent).toContain('beans-p-a1 · beans')
    expect(article.textContent).toContain('Review the patch')
    expect(article.textContent).toContain('open')
    expect(article.textContent).toContain('P1')
    expect(article.textContent).toContain('Fromalice')
    expect(article.textContent).toContain('Toall')
    expect(article.textContent).toContain('Claimantbob')
    expect(article.textContent).toContain('Labelsreview, agent')
    expect(article.querySelector('h2#steps')?.textContent).toBe('Steps')
    expect(screen.getByRole('link', { name: 'Linked issue' }).getAttribute('href')).toBe('/issues/beans-i-1')
    expect(screen.getByRole('link', { name: 'Parent issue' }).getAttribute('href')).toBe('/issues/beans-i-9')
    expect(article.textContent).toContain('alice · created · initial')
    expect(screen.getByRole('navigation', { name: 'Prompt table of contents' }).textContent).toContain('Steps')
    expect(screen.getAllByRole('button').map((button) => button.textContent?.trim())).toEqual(['← Back to prompts'])
  })

  it('renders null and unresolved issue variants', async () => {
    const get = vi.spyOn(api, 'getPrompt')
      .mockResolvedValueOnce({ ...detail, issue: null })
      .mockResolvedValueOnce({ ...detail, id: 'beans-p-b2', issue: { id: 'beans-i-gone', title: '', missing: true } })
    const view = render(PromptsRoute, { project: 'beans', id: summary.id, reloadKey: 0, navigate: vi.fn() })
    expect(await screen.findByText('No issue is linked to this prompt.')).toBeTruthy()

    await view.rerender({ project: 'beans', id: 'beans-p-b2', reloadKey: 0, navigate: vi.fn() })
    await waitFor(() => expect(screen.getByText('beans-i-gone')).toBeTruthy())
    expect(screen.getByText('Missing')).toBeTruthy()
    expect(get).toHaveBeenLastCalledWith('beans-p-b2')
  })

  it('clears detail immediately when changing to the list', async () => {
    let resolveList!: (value: PromptSummary[]) => void
    vi.spyOn(api, 'getPrompt').mockResolvedValue(detail)
    vi.spyOn(api, 'listPrompts').mockReturnValue(new Promise((resolve) => { resolveList = resolve }))
    const view = render(PromptsRoute, { project: '_all', id: summary.id, reloadKey: 0, navigate: vi.fn() })
    await screen.findByRole('article', { name: 'Prompt detail' })

    await view.rerender({ project: '_all', id: null, reloadKey: 0, navigate: vi.fn() })
    expect(screen.queryByRole('article', { name: 'Prompt detail' })).toBeNull()
    resolveList([summary])
    expect(await screen.findByRole('region', { name: 'Prompts' })).toBeTruthy()
  })

  it('does not let a stale detail response overwrite newer list navigation', async () => {
    let resolveDetail!: (value: PromptDetail) => void
    vi.spyOn(api, 'getPrompt').mockReturnValue(new Promise((resolve) => { resolveDetail = resolve }))
    vi.spyOn(api, 'listPrompts').mockResolvedValue([summary])
    const view = render(PromptsRoute, { project: '_all', id: summary.id, reloadKey: 0, navigate: vi.fn() })
    await waitFor(() => expect(api.getPrompt).toHaveBeenCalled())

    await view.rerender({ project: '_all', id: null, reloadKey: 0, navigate: vi.fn() })
    await screen.findByRole('region', { name: 'Prompts' })
    resolveDetail(detail)
    await Promise.resolve()
    expect(screen.queryByRole('article', { name: 'Prompt detail' })).toBeNull()
  })

  it('handles rendered prompt wikilinks and backlinks only on ordinary clicks', async () => {
    vi.spyOn(api, 'getPrompt').mockResolvedValue(detail)
    const navigate = vi.fn()
    render(PromptsRoute, { project: 'beans', id: summary.id, reloadKey: 0, navigate })
    const rendered = await screen.findByRole('link', { name: 'the next prompt' })
    const backlink = screen.getByRole('link', { name: 'Parent issue' })

    expect(await fireEvent.click(rendered, { metaKey: true })).toBe(true)
    expect(await fireEvent.click(backlink, { shiftKey: true })).toBe(true)
    expect(navigate).not.toHaveBeenCalled()
    expect(await fireEvent.click(rendered)).toBe(false)
    expect(await fireEvent.click(backlink)).toBe(false)
    expect(navigate.mock.calls).toEqual([['/prompts/beans-p-b2'], ['/issues/beans-i-9']])
  })
})
