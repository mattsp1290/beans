// @vitest-environment jsdom

import { cleanup, render, screen } from '@testing-library/svelte'
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
    vi.unstubAllGlobals()
  })
})
