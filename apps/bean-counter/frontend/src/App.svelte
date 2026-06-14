<script lang="ts">
  import AppShell from './lib/components/AppShell.svelte'
  import ErrorState from './lib/components/ErrorState.svelte'
  import IssuesRoute from './routes/issues/IssuesRoute.svelte'
  import ReadyRoute from './routes/ready/ReadyRoute.svelte'
  import { getRoute, routes } from './routes'

  let pathname = $state(window.location.pathname)
  const route = $derived(getRoute(pathname))

  function navigate(event: MouseEvent, path: string) {
    if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) {
      return
    }
    event.preventDefault()
    if (path === pathname) {
      return
    }
    history.pushState({}, '', path)
    pathname = path
  }

  function go(path: string) {
    if (path === pathname) {
      return
    }
    history.pushState({}, '', path)
    pathname = path
  }

  function syncPathname() {
    pathname = window.location.pathname
  }
</script>

<svelte:window onpopstate={syncPathname} />

<svelte:head>
  <title>{route.title} | Bean Counter</title>
</svelte:head>

<AppShell {routes} activePath={route.path} onNavigate={navigate}>
  {#snippet title()}
    <h1>{route.title}</h1>
    <p>{route.description}</p>
  {/snippet}

  {#if route.path === '/'}
    <IssuesRoute {pathname} navigate={go} />
  {:else if route.path === '/ready'}
    <ReadyRoute navigate={go} />
  {:else if route.path === '/graph'}
    <section class="workspace" aria-label="Dependency graph workspace">
      <ErrorState
        title="Graph not connected"
        message="Graph data will render here after the visualization slice lands."
      />
    </section>
  {/if}
</AppShell>
