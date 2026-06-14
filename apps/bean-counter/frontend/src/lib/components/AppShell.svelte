<script lang="ts">
  import type { Snippet } from 'svelte'
  import type { AppRoute } from '../../routes'

  interface Props {
    routes: AppRoute[]
    activePath: string
    onNavigate: (event: MouseEvent, path: string) => void
    title: Snippet
    children: Snippet
  }

  let { routes, activePath, onNavigate, title, children }: Props = $props()
</script>

<div class="shell">
  <aside class="sidebar" aria-label="Primary navigation">
    <a class="brand" href="/" onclick={(event) => onNavigate(event, '/')}>
      <span class="brand-mark" aria-hidden="true">bc</span>
      <span>
        <strong>Bean Counter</strong>
        <small>Local tracker</small>
      </span>
    </a>

    <nav>
      {#each routes as route}
        <a
          class:active={route.path === activePath}
          href={route.path}
          onclick={(event) => onNavigate(event, route.path)}
          aria-current={route.path === activePath ? 'page' : undefined}
        >
          {route.label}
        </a>
      {/each}
    </nav>
  </aside>

  <main>
    <header class="topbar">
      <div>
        {@render title()}
      </div>
      <button type="button">New issue</button>
    </header>

    {@render children()}
  </main>
</div>
