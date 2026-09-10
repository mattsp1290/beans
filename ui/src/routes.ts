export interface AppRoute {
  path: string
  label: string
  title: string
  description: string
}

export const routes: AppRoute[] = [
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
  if (pathname === '/' || pathname === '/issues' || pathname.startsWith('/issues/')) {
    return routes[0]
  }
  if (pathname === '/ready') {
    return routes[1]
  }
  if (pathname === '/graph') {
    return routes[2]
  }
  if (pathname === '/wiki' || pathname.startsWith('/wiki/')) {
    return routes[3]
  }
  if (pathname === '/search') {
    return routes[4]
  }
  return routes[0]
}

/** Extracts the issue id from `/issues/:id`, or null when the path is not a detail path. */
export function parseIssueId(pathname: string): string | null {
  const match = /^\/issues\/([^/]+)\/?$/.exec(pathname)
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
