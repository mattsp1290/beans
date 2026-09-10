import { describe, expect, it, vi } from 'vitest'

import type { Backlink } from './api'
import { backlinkHref, handleRenderedContentClick, isInterceptableContentLink } from './nav'

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
})

describe('isInterceptableContentLink', () => {
  it('intercepts issue, wiki, and search links', () => {
    expect(isInterceptableContentLink('/issues/bn-1')).toBe(true)
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
  }

  function fakeAnchor(href: string | null): FakeAnchor {
    return { getAttribute: (name) => (name === 'href' ? href : null) }
  }

  function fakeEvent(anchor: FakeAnchor | null): { event: MouseEvent; prevented: () => boolean } {
    let prevented = false
    const target = {
      closest: (selector: string) => (selector === 'a' ? anchor : null),
    }
    const event = {
      target,
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
