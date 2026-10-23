import { describe, expect, it, vi } from 'vitest'

import type { Backlink } from './api'
import { backlinkHref, handleNavigationClick, handleRenderedContentClick, isInterceptableContentLink } from './nav'

function backlink(overrides: Partial<Backlink>): Backlink {
  return { from: 'bn-1', kind: 'reference', note_kind: 'issue', ...overrides }
}

describe('backlinkHref', () => {
  it('routes an issue backlink to the issue detail page', () => {
    expect(backlinkHref(backlink({ note_kind: 'issue', from: 'bn-1' }))).toBe('/issues/bn-1')
  })

  it('routes a doc backlink to the wiki page, stripping the .md extension', () => {
    expect(backlinkHref(backlink({ note_kind: 'doc', from: 'readme', path: 'projects/beans/README.md' }))).toBe(
      '/wiki/projects/beans/README',
    )
  })

  it('falls back to `from` for a doc backlink with no path', () => {
    expect(backlinkHref(backlink({ note_kind: 'doc', from: 'notes/todo.md', path: undefined }))).toBe('/wiki/notes/todo')
  })

	it('routes a memory backlink to a search for its basename', () => {
    expect(backlinkHref(backlink({ note_kind: 'memory', from: '2026-06-14 standup' }))).toBe(
      '/search?q=2026-06-14%20standup',
    )
	})

	it('routes a request backlink to request detail', () => {
		expect(backlinkHref(backlink({ note_kind: 'request', from: 'beans-r-a3f2' }))).toBe('/requests/beans-r-a3f2')
	})

  it('routes a prompt backlink to prompt detail with encoding', () => {
    expect(backlinkHref(backlink({ note_kind: 'prompt', from: 'beans-p-a/b' }))).toBe('/prompts/beans-p-a%2Fb')
  })
})

describe('isInterceptableContentLink', () => {
  it('intercepts issue, prompt, wiki, and search links', () => {
    expect(isInterceptableContentLink('/issues/bn-1')).toBe(true)
		expect(isInterceptableContentLink('/requests/bn-r-1')).toBe(true)
    expect(isInterceptableContentLink('/prompts/bn-p-a1')).toBe(true)
    expect(isInterceptableContentLink('/wiki/projects/beans/README')).toBe(true)
    expect(isInterceptableContentLink('/search?q=hello')).toBe(true)
  })

  it('leaves unresolved wiki-new links alone', () => {
    expect(isInterceptableContentLink('/wiki/new?title=Untitled')).toBe(false)
  })

  it('leaves external and unrelated links alone', () => {
    expect(isInterceptableContentLink('https://example.test/x')).toBe(false)
    expect(isInterceptableContentLink('/assets/diagram.png')).toBe(false)
  })
})

describe('handleRenderedContentClick', () => {
  interface FakeAnchor {
    getAttribute(name: string): string | null
    hasAttribute(name: string): boolean
  }

  function fakeAnchor(href: string | null): FakeAnchor {
    return {
      getAttribute: (name) => (name === 'href' ? href : null),
      hasAttribute: () => false,
    }
  }

  function fakeEvent(anchor: FakeAnchor | null): { event: MouseEvent; prevented: () => boolean } {
    let prevented = false
    const target = {
      closest: (selector: string) => (selector === 'a' ? anchor : null),
    }
    const event = {
      target,
      button: 0,
      metaKey: false,
      ctrlKey: false,
      shiftKey: false,
      altKey: false,
      defaultPrevented: false,
      preventDefault: () => {
        prevented = true
      },
    } as unknown as MouseEvent
    return { event, prevented: () => prevented }
  }

  it('intercepts a click on an internal link and navigates client-side', () => {
    const { event, prevented } = fakeEvent(fakeAnchor('/issues/bn-1'))
    const navigate = vi.fn()

    handleRenderedContentClick(event, navigate)

    expect(navigate).toHaveBeenCalledWith('/issues/bn-1')
    expect(prevented()).toBe(true)
  })

  it('ignores clicks that do not land on an anchor', () => {
    const { event } = fakeEvent(null)
    const navigate = vi.fn()

    handleRenderedContentClick(event, navigate)

    expect(navigate).not.toHaveBeenCalled()
  })

  it('leaves external links for the browser to handle', () => {
    const { event, prevented } = fakeEvent(fakeAnchor('https://example.test/x'))
    const navigate = vi.fn()

    handleRenderedContentClick(event, navigate)

    expect(navigate).not.toHaveBeenCalled()
    expect(prevented()).toBe(false)
  })

  it('leaves unresolved /wiki/new links for the browser to handle', () => {
    const { event } = fakeEvent(fakeAnchor('/wiki/new?title=Untitled'))
    const navigate = vi.fn()

    handleRenderedContentClick(event, navigate)

    expect(navigate).not.toHaveBeenCalled()
  })

  it('ignores modified clicks (e.g. cmd-click to open in a new tab)', () => {
    const { event, prevented } = fakeEvent(fakeAnchor('/issues/bn-1'))
    Object.defineProperty(event, 'metaKey', { value: true })
    const navigate = vi.fn()

    handleRenderedContentClick(event, navigate)

    expect(navigate).not.toHaveBeenCalled()
    expect(prevented()).toBe(false)
  })
})

describe('handleNavigationClick', () => {
  it('only intercepts plain primary internal link clicks', () => {
    const navigate = vi.fn()
    for (const event of [
      new MouseEvent('click', { button: 1, cancelable: true }),
      new MouseEvent('click', { ctrlKey: true, cancelable: true }),
    ]) {
      handleNavigationClick(event, navigate, '/prompts/beans-p-a1', document.createElement('a'))
      expect(event.defaultPrevented).toBe(false)
    }

    const target = document.createElement('a')
    target.target = '_blank'
    const targeted = new MouseEvent('click', { cancelable: true })
    handleNavigationClick(targeted, navigate, '/prompts/beans-p-a1', target)
    expect(targeted.defaultPrevented).toBe(false)

    const download = document.createElement('a')
    download.setAttribute('download', '')
    const downloaded = new MouseEvent('click', { cancelable: true })
    handleNavigationClick(downloaded, navigate, '/prompts/beans-p-a1', download)
    expect(downloaded.defaultPrevented).toBe(false)

    const external = new MouseEvent('click', { cancelable: true })
    handleNavigationClick(external, navigate, 'https://example.test', document.createElement('a'))
    expect(external.defaultPrevented).toBe(false)
    expect(navigate).not.toHaveBeenCalled()

    const ordinary = new MouseEvent('click', { cancelable: true })
    handleNavigationClick(ordinary, navigate, '/prompts/beans-p-a1', document.createElement('a'))
    expect(ordinary.defaultPrevented).toBe(true)
    expect(navigate).toHaveBeenCalledWith('/prompts/beans-p-a1')
  })
})
