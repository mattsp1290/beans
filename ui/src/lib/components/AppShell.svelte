<script lang="ts">
  import type { Snippet } from 'svelte'
  import type { AppRoute } from '../../routes'
  import { ALL_PROJECTS, type ProjectSummary } from '../api'

  interface Props {
    routes: AppRoute[]
    activePath: string
    project: string
    projects: ProjectSummary[]
    projectsError?: string
    banner?: string
    onNavigate: (event: MouseEvent, path: string) => void
    onProjectChange: (project: string) => void
    onDismissBanner?: () => void
    title: Snippet
    children: Snippet
  }

  let {
    routes,
    activePath,
    project,
    projects,
    projectsError = '',
    banner = '',
    onNavigate,
    onProjectChange,
    onDismissBanner,
    title,
    children,
  }: Props = $props()

  function handleProjectChange(event: Event) {
    onProjectChange((event.target as HTMLSelectElement).value)
  }
</script>

<div class="shell">
  <aside class="sidebar" aria-label="Primary navigation">
    <a class="brand" href="/issues" onclick={(event) => onNavigate(event, '/issues')}>
      <span class="brand-mark" aria-hidden="true">bn</span>
      <span>
        <strong>beans</strong>
        <small>Local hub vault</small>
      </span>
    </a>

    <label class="project-switcher" for="project-select">
      Project
      <select id="project-select" value={project} onchange={handleProjectChange}>
        <option value={ALL_PROJECTS}>All projects</option>
        {#each projects as item (item.name)}
          <option value={item.name}>{item.name} ({item.prefix})</option>
        {/each}
      </select>
    </label>
    {#if projectsError !== ''}
      <p class="muted project-switcher-error">{projectsError}</p>
    {/if}

    <nav>
      {#each routes as route (route.path)}
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
    </header>

    {#if banner !== ''}
      <div class="banner warning app-banner" role="status">
        <span>{banner}</span>
        {#if onDismissBanner}
          <button type="button" class="secondary" onclick={onDismissBanner}>Dismiss</button>
        {/if}
      </div>
    {/if}

    {@render children()}
  </main>
</div>

<style>
  .project-switcher {
    display: grid;
    gap: 6px;
    margin-bottom: 16px;
    color: var(--muted);
    font-size: 13px;
  }

  .project-switcher select {
    width: 100%;
  }

  .project-switcher-error {
    margin-top: -10px;
    margin-bottom: 16px;
    font-size: 12px;
  }

  .app-banner {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    margin-bottom: 20px;
  }

  .app-banner button {
    flex: 0 0 auto;
  }
</style>
