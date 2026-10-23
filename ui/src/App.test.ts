// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'
import App from './App.svelte'
import { api, type PromptDetail } from './lib/api'

const prompt: PromptDetail = {
  id: 'beans-p-a1', title: 'Deep-linked prompt', status: 'open', priority: 1, labels: [],
  from: 'alice', to: 'agent', claimed_by: null, created: '2026-10-22T01:00:00Z', updated: '2026-10-23T01:00:00Z',
  project: 'beans', path: 'projects/beans/prompts/a1.md', has_issue: false, html: '<p>Deep detail body</p>',
  toc: [], backlinks: [], outlinks: [], log: [], issue: null,
}

class FakeEventSource {
  addEventListener() {}
  close() {}
}

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  window.history.replaceState({}, '', '/')
})

describe('App prompt deep links', () => {
  it('renders prompt detail directly from a /prompts/:id location', async () => {
    window.history.replaceState({}, '', '/prompts/beans-p-a1')
    vi.stubGlobal('EventSource', FakeEventSource)
    vi.spyOn(api, 'listProjects').mockResolvedValue([])
    vi.spyOn(api, 'getPrompt').mockResolvedValue(prompt)

    render(App)

    expect(await screen.findByRole('heading', { name: 'Deep-linked prompt' })).toBeTruthy()
    expect(screen.getByText('Deep detail body')).toBeTruthy()
    expect(api.getPrompt).toHaveBeenCalledWith('beans-p-a1')
  })

  it('routes a rendered prompt link with a fragment without treating the hash as part of the id', async () => {
    const linked = { ...prompt, id: 'beans-p-b2', title: 'Linked prompt', html: '<h2 id="steps">Steps</h2>' }
    window.history.replaceState({}, '', '/prompts/beans-p-a1')
    vi.stubGlobal('EventSource', FakeEventSource)
    vi.spyOn(api, 'listProjects').mockResolvedValue([])
    vi.spyOn(api, 'getPrompt').mockImplementation(async (id) => id === linked.id ? linked : {
      ...prompt,
      html: '<p><a href="/prompts/beans-p-b2#steps">Open linked steps</a></p>',
    })

    render(App)
    await fireEvent.click(await screen.findByRole('link', { name: 'Open linked steps' }))

    expect(await screen.findByRole('heading', { name: 'Linked prompt' })).toBeTruthy()
    expect(api.getPrompt).toHaveBeenLastCalledWith('beans-p-b2')
    expect(window.location.pathname).toBe('/prompts/beans-p-b2')
    expect(window.location.hash).toBe('#steps')
  })

  it('keeps query and fragment separate during rendered navigation and popstate', async () => {
    window.history.replaceState({}, '', '/prompts/beans-p-a1')
    vi.stubGlobal('EventSource', FakeEventSource)
    vi.spyOn(api, 'listProjects').mockResolvedValue([])
    vi.spyOn(api, 'getPrompt').mockResolvedValue({
      ...prompt,
      html: '<p><a href="/search?q=x#heading">Search for x</a></p>',
    })
    vi.spyOn(api, 'search').mockResolvedValue([])

    render(App)
    await fireEvent.click(await screen.findByRole('link', { name: 'Search for x' }))

    expect(await screen.findByRole('heading', { name: 'Search' })).toBeTruthy()
    expect((screen.getByRole('textbox', { name: 'Query' }) as HTMLInputElement).value).toBe('x')
    expect(window.location.search).toBe('?q=x')
    expect(window.location.hash).toBe('#heading')

    window.history.replaceState({}, '', '/prompts/beans-p-a1#instructions')
    window.dispatchEvent(new PopStateEvent('popstate'))
    expect(await screen.findByRole('heading', { name: 'Deep-linked prompt' })).toBeTruthy()
    expect(window.location.hash).toBe('#instructions')
  })
})

describe('App malformed prompt paths', () => {
  it.each([
    '/prompts/BN-p-a1',
    '/prompts/beans-r-a1',
    '/prompts/%E0%A4%A',
    '/prompts/beans-p-a1/more',
  ])('shows a prompt-specific not-found state for %s without loading issues', async (path) => {
    window.history.replaceState({}, '', path)
    vi.stubGlobal('EventSource', FakeEventSource)
    vi.spyOn(api, 'listProjects').mockResolvedValue([])
    const getPrompt = vi.spyOn(api, 'getPrompt').mockResolvedValue(prompt)
    const listIssues = vi.spyOn(api, 'listIssues').mockResolvedValue([])

    render(App)

    expect(await screen.findByRole('heading', { name: 'Prompt not found' })).toBeTruthy()
    expect(screen.getByText(/valid prompt link/i)).toBeTruthy()
    expect(screen.queryByRole('button', { name: /new issue/i })).toBeNull()
    await waitFor(() => {
      expect(getPrompt).not.toHaveBeenCalled()
      expect(listIssues).not.toHaveBeenCalled()
    })
  })
})
