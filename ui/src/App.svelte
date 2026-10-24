<script lang="ts">
  import AppShell from './lib/components/AppShell.svelte'
	import EmptyState from './lib/components/EmptyState.svelte'
  import { ALL_PROJECTS, api, type ProjectSummary, type WorkflowInfo } from './lib/api'
  import GraphRoute from './routes/graph/GraphRoute.svelte'
  import IssueDetailRoute from './routes/issues/IssueDetailRoute.svelte'
  import IssuesRoute from './routes/issues/IssuesRoute.svelte'
  import ReadyRoute from './routes/ready/ReadyRoute.svelte'
  import SearchRoute from './routes/search/SearchRoute.svelte'
  import WikiRoute from './routes/wiki/WikiRoute.svelte'
	import PlansRoute from './routes/plans/PlansRoute.svelte'
	import RequestsRoute from './routes/requests/RequestsRoute.svelte'
	import PromptsRoute from './routes/prompts/PromptsRoute.svelte'
	import { handleNavigationClick } from './lib/nav'
	import { getRoute, parseIssueId, parsePlanId, parsePromptId, parseRequestId, parseWikiPath, routes } from './routes'

  const PROJECT_STORAGE_KEY = 'bn-ui:project'
  const EMPTY_WORKFLOW: WorkflowInfo = { statuses: [], active: [], terminal: [] }

  let pathname = $state(window.location.pathname)
  let search = $state(window.location.search)
	let hash = $state(window.location.hash)
  const route = $derived(getRoute(pathname))
  const issueId = $derived(parseIssueId(pathname))
	const planId = $derived(parsePlanId(pathname))
	const requestId = $derived(parseRequestId(pathname))
	const promptId = $derived(parsePromptId(pathname))
  const wikiPath = $derived(parseWikiPath(pathname))
  const searchQuery = $derived(new URLSearchParams(search).get('q') ?? '')

  let project = $state(loadStoredProject())
  let reloadKey = $state(0)
  let banner = $state('')

  let projects = $state<ProjectSummary[]>([])
  let projectsError = $state('')

  const workflow = $derived(projectWorkflow(project, projects))

  $effect(() => {
    if (pathname === '/') {
      history.replaceState({}, '', '/issues')
      pathname = '/issues'
    }
  })

  $effect(() => {
    const source = new EventSource(api.eventsUrl())
    source.addEventListener('reload', () => {
      reloadKey += 1
    })
    return () => source.close()
  })

  $effect(() => {
    // Re-fetch on reload too: counts and, in principle, workflow config can
    // change under a live hub.
    reloadKey
    let cancelled = false
    api
      .listProjects()
      .then((list) => {
        if (!cancelled) {
          projects = list
          projectsError = ''
        }
      })
      .catch(() => {
        if (!cancelled) {
          projectsError = 'Could not load projects.'
        }
      })
    return () => {
      cancelled = true
    }
  })

  /**
   * The workflow to drive column ordering by. For a single project, its own
   * workflow. For "all projects", the union of every project's workflow,
   * with statuses ordered by first appearance across the project list.
   */
  function projectWorkflow(projectName: string, list: ProjectSummary[]): WorkflowInfo {
    if (projectName !== ALL_PROJECTS) {
      return list.find((item) => item.name === projectName)?.workflow ?? EMPTY_WORKFLOW
    }
    return {
      statuses: unionInOrder(list.map((item) => item.workflow.statuses)),
      active: unionInOrder(list.map((item) => item.workflow.active)),
      terminal: unionInOrder(list.map((item) => item.workflow.terminal)),
    }
  }

  function unionInOrder(lists: string[][]): string[] {
    const seen = new Set<string>()
    const result: string[] = []
    for (const list of lists) {
      for (const value of list) {
        if (!seen.has(value)) {
          seen.add(value)
          result.push(value)
        }
      }
    }
    return result
  }

  function loadStoredProject(): string {
    try {
      return window.localStorage.getItem(PROJECT_STORAGE_KEY) ?? ALL_PROJECTS
    } catch {
      return ALL_PROJECTS
    }
  }

  function setBanner(message: string) {
    banner = message
  }

  function setProject(value: string) {
    project = value
    try {
      window.localStorage.setItem(PROJECT_STORAGE_KEY, value)
    } catch {
      // Storage may be unavailable (private browsing, disabled storage); the
      // project selection just won't persist across reloads.
    }
  }

  function applyPath(path: string) {
		const destination = new URL(path, window.location.href)
		const next = destination.pathname + destination.search + destination.hash
		history.pushState({}, '', next)
		pathname = destination.pathname
		search = destination.search
		hash = destination.hash
  }

  function navigate(event: MouseEvent, path: string) {
    handleNavigationClick(event, go, path)
  }

  function go(path: string) {
		const destination = new URL(path, window.location.href)
		if (destination.pathname + destination.search + destination.hash === pathname + search + hash) {
      return
    }
    applyPath(path)
  }

	function syncLocation() {
    pathname = window.location.pathname
    search = window.location.search
		hash = window.location.hash
  }
</script>

<svelte:window onpopstate={syncLocation} onhashchange={syncLocation} />

<svelte:head>
  <title>{route.title} | beans</title>
</svelte:head>

<AppShell
  {routes}
  activePath={route.path}
  {project}
  {projects}
  {projectsError}
  {banner}
  onNavigate={navigate}
  onProjectChange={setProject}
  onDismissBanner={() => setBanner('')}
>
  {#snippet title()}
    <h1>{route.title}</h1>
    <p>{route.description}</p>
  {/snippet}

  {#if planId || pathname === '/plans'}
    <PlansRoute {project} id={planId} navigate={go} {reloadKey} />
	{:else if issueId}
		<IssueDetailRoute id={issueId} navigate={go} {reloadKey} onBanner={setBanner} />
	{:else if requestId || pathname === '/requests'}
		<RequestsRoute {project} id={requestId} navigate={go} {reloadKey} />
	{:else if promptId || pathname === '/prompts'}
		<PromptsRoute {project} id={promptId} navigate={go} {reloadKey} />
	{:else if pathname.startsWith('/prompts/')}
		<EmptyState title="Prompt not found" message="Use a valid prompt link or return to the prompts list." />
  {:else if pathname === '/ready'}
    <ReadyRoute {project} navigate={go} {reloadKey} />
  {:else if pathname === '/graph'}
    <GraphRoute {project} {reloadKey} />
  {:else if pathname === '/wiki' || pathname.startsWith('/wiki/')}
    <WikiRoute {project} path={wikiPath} navigate={go} {reloadKey} />
  {:else if pathname === '/search'}
    <SearchRoute {project} navigate={go} {reloadKey} initialQuery={searchQuery} />
  {:else}
    <IssuesRoute {project} {workflow} navigate={go} {reloadKey} onBanner={setBanner} />
  {/if}
</AppShell>
