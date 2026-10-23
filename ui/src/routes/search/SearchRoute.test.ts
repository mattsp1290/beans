// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from '../../lib/api'
import SearchRoute from './SearchRoute.svelte'

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

describe('SearchRoute prompt integration', () => {
  it('renders a prompt search hit and navigates to its encoded detail route', async () => {
    vi.spyOn(api, 'search').mockResolvedValue([{
      kind: 'prompt', id: 'beans-p-a1', basename: 'a1', title: 'Review patch', project: 'beans',
      path: 'projects/beans/prompts/a1.md', score: 0.875,
    }])
    const navigate = vi.fn()
    render(SearchRoute, { project: '_all', navigate, reloadKey: 0, initialQuery: 'review' })

    const result = await screen.findByRole('button', { name: /prompt.*Review patch.*beans-p-a1.*beans.*0.88/i })
    expect(api.search).toHaveBeenCalledWith({
      q: 'review', kind: undefined, project: undefined, include_archived_handoffs: false,
    })
    await fireEvent.click(result)
    expect(navigate).toHaveBeenCalledWith('/prompts/beans-p-a1')
  })
})
