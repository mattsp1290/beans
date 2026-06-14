<script lang="ts">
  import AppShell from './lib/components/AppShell.svelte'
  import EmptyState from './lib/components/EmptyState.svelte'
  import ErrorState from './lib/components/ErrorState.svelte'
  import LoadingState from './lib/components/LoadingState.svelte'
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
    <section class="workspace" aria-label="Issues workspace">
      <div class="toolbar">
        <label>
          <span>Filter</span>
          <input type="search" placeholder="Title, label, or id" />
        </label>
        <select aria-label="State">
          <option>All states</option>
          <option>Open</option>
          <option>In progress</option>
          <option>Blocked</option>
          <option>Closed</option>
        </select>
      </div>

      <EmptyState
        title="No issues loaded"
        message="The issues UI will connect to the API client in the next feature slice."
      />
    </section>
  {:else if route.path === '/ready'}
    <section class="workspace" aria-label="Ready queue workspace">
      <LoadingState label="Ready queue loader" message="Ready queue integration is queued for the next slice." />
    </section>
  {:else if route.path === '/graph'}
    <section class="workspace" aria-label="Dependency graph workspace">
      <ErrorState
        title="Graph not connected"
        message="Graph data will render here after the visualization slice lands."
      />
    </section>
  {/if}
</AppShell>
