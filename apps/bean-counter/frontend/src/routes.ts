export interface AppRoute {
  path: string
  label: string
  title: string
  description: string
}

export const routes: AppRoute[] = [
  {
    path: '/',
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
]

export function getRoute(pathname: string): AppRoute {
  return routes.find((route) => route.path === pathname) ?? routes[0]
}
