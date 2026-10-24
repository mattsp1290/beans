import type { Backlink } from './api'

/**
 * Routes a backlink to the in-app path that shows it, by `note_kind` rather
 * than guessing from the shape of `from` (do not resurrect
 * `looksLikeIssueId`-style heuristics here).
 */
export function backlinkHref(link: Backlink): string {
  if (link.note_kind === 'issue') {
    return `/issues/${encodeURIComponent(link.from)}`
  }
	if (link.note_kind === 'memory') {
		return `/search?q=${encodeURIComponent(link.from)}`
	}
	if (link.note_kind === 'request') { return `/requests/${encodeURIComponent(link.from)}` }
	if (link.note_kind === 'prompt') { return `/prompts/${encodeURIComponent(link.from)}` }
  return `/wiki/${docPathSegments(link.path ?? link.from)}`
}

function docPathSegments(path: string): string {
  const withoutExtension = path.replace(/\.md$/i, '')
  return withoutExtension
    .split('/')
    .filter((segment) => segment !== '')
    .map(encodeURIComponent)
    .join('/')
}

/**
 * Whether an anchor's href, found inside server-rendered `{@html}` content
 * (an issue description or a wiki page), should be handled by the client
 * router instead of a full page load. Excludes `/wiki/new...` links, which
 * the renderer emits for unresolved wikilinks and which have no in-app route.
 */
export function isInterceptableContentLink(href: string): boolean {
  if (href.startsWith('/wiki/new')) {
    return false
  }
	return href.startsWith('/issues/') || href.startsWith('/requests/') || href.startsWith('/prompts/') || href.startsWith('/wiki/') || href.startsWith('/search')
}

/**
 * Intercepts only an ordinary primary-button activation of an internal link.
 * Modified/non-primary clicks, links with browser navigation attributes, and
 * non-root-relative destinations retain the browser's native behaviour.
 */
export function handleNavigationClick(
  event: MouseEvent,
  navigate: (path: string) => void,
  path: string,
  anchor: Element | null = event.currentTarget as Element | null,
): void {
  const target = anchor?.getAttribute('target')
  if (
    event.defaultPrevented ||
    event.button !== 0 ||
    event.metaKey ||
    event.ctrlKey ||
    event.shiftKey ||
    event.altKey ||
    !path.startsWith('/') ||
    anchor?.hasAttribute('download') ||
    (target !== null && target !== '' && target !== '_self')
  ) {
    return
  }
  event.preventDefault()
  navigate(path)
}

/**
 * Click handler for a `{@html}` container: intercepts clicks on internal
 * links (per {@link isInterceptableContentLink}) and routes them through the
 * client-side `navigate` function instead of a full page load. External
 * links and unresolved `/wiki/new...` links are left alone.
 */
export function handleRenderedContentClick(event: MouseEvent, navigate: (path: string) => void): void {
  const target = event.target as HTMLElement | null
  const anchor = target?.closest('a')
  if (!anchor) {
    return
  }
  const href = anchor.getAttribute('href')
  if (!href || !isInterceptableContentLink(href)) {
    return
  }
  handleNavigationClick(event, navigate, href, anchor)
}
