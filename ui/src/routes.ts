export interface AppRoute {
  path: string
  label: string
  title: string
  description: string
}

export const routes: AppRoute[] = [
	{ path: '/plans', label: 'Plans', title: 'Plans', description: 'Read-only implementation plans' },
  {
    path: '/issues',
    label: 'Issues',
    title: 'Issues',
    description: 'Project work queue',
  },
  {
    path: '/ready',
    label: 'Ready',
    title: 'Ready Queue',
    description: 'Unblocked work ordered by priority',
  },
  {
    path: '/graph',
    label: 'Graph',
    title: 'Dependency Graph',
    description: 'Issue relationships and blockers',
  },
  {
    path: '/wiki',
    label: 'Wiki',
    title: 'Wiki',
    description: 'Hub and project documentation',
  },
  {
    path: '/search',
    label: 'Search',
    title: 'Search',
    description: 'Find issues and docs across the hub',
  },
]

/** Returns the route metadata (nav highlight, page title) for a pathname. */
export function getRoute(pathname: string): AppRoute {
	const route = (path: string) => routes.find((item) => item.path === path)!
	if (pathname === '/plans' || pathname.startsWith('/plans/')) { return route('/plans') }
	if (pathname === '/' || pathname === '/issues' || pathname.startsWith('/issues/')) {
		return route('/issues')
  }
  if (pathname === '/ready') {
		return route('/ready')
  }
  if (pathname === '/graph') {
		return route('/graph')
  }
  if (pathname === '/wiki' || pathname.startsWith('/wiki/')) {
		return route('/wiki')
  }
  if (pathname === '/search') {
		return route('/search')
  }
	return route('/issues')
}

/** Extracts the issue id from `/issues/:id`, or null when the path is not a detail path. */
export function parseIssueId(pathname: string): string | null {
  const match = /^\/issues\/([^/]+)\/?$/.exec(pathname)
  return match ? decodeURIComponent(match[1]) : null
}

export function parsePlanId(pathname: string): string | null {
	const match = /^\/plans\/([^/]+)\/?$/.exec(pathname)
	return match ? decodeURIComponent(match[1]) : null
}

/** Extracts the doc path from `/wiki` or `/wiki/*path`, as a hub-relative path (no leading slash). */
export function parseWikiPath(pathname: string): string {
  if (pathname === '/wiki' || pathname === '/wiki/') {
    return ''
  }
  const match = /^\/wiki\/(.+)$/.exec(pathname)
  return match ? match[1].split('/').map(decodeURIComponent).join('/') : ''
}
