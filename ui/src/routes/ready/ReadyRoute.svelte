<script lang="ts">
  import { ALL_PROJECTS, ApiError, api, type Issue } from '../../lib/api'
  import EmptyState from '../../lib/components/EmptyState.svelte'
  import ErrorState from '../../lib/components/ErrorState.svelte'
  import LoadingState from '../../lib/components/LoadingState.svelte'

  interface Props {
    project: string
    navigate: (path: string) => void
    reloadKey: number
  }

  let { project, navigate, reloadKey }: Props = $props()

  let issues = $state<Issue[]>([])
  let loading = $state(false)
  let error = $state('')
  let refreshedAt = $state<Date | null>(null)

  $effect(() => {
    project
    reloadKey
    let cancelled = false
    load(() => cancelled)
    return () => {
      cancelled = true
    }
  })

  async function load(isCancelled: () => boolean = () => false) {
    loading = true
    error = ''
    try {
      const result = await api.ready(project)
      if (isCancelled()) return
      issues = result
      refreshedAt = new Date()
    } catch (err) {
      if (isCancelled()) return
      error = errorMessage(err)
    } finally {
      if (!isCancelled()) loading = false
    }
  }

  function errorMessage(err: unknown): string {
    if (err instanceof ApiError) {
      return err.message
    }
    return err instanceof Error ? err.message : 'Request failed.'
  }
</script>

<section class="workspace ready-workspace" aria-label="Ready queue workspace">
  <div class="toolbar">
    <div class="queue-summary">
      <strong>{issues.length}</strong>
      <span>{issues.length === 1 ? 'ready issue' : 'ready issues'}</span>
      {#if refreshedAt}
        <small>Refreshed {refreshedAt.toLocaleTimeString()}</small>
      {/if}
    </div>
    <button type="button" class="secondary" disabled={loading} onclick={() => load()}>
      {loading ? 'Refreshing' : 'Refresh'}
    </button>
  </div>

  {#if loading && issues.length === 0}
    <LoadingState message="Loading ready queue" />
  {:else if error !== '' && issues.length === 0}
    <ErrorState title="Could not load ready queue" message={error} />
  {:else if issues.length === 0}
    <EmptyState title="No ready work" message="Blocked and closed issues are excluded from this queue." />
  {:else}
    {#if error !== ''}
      <p class="form-error" role="alert">{error}</p>
    {/if}
    <ul class="ready-list" aria-label="Ready issues">
      {#each issues as issue, index (issue.id)}
        <li>
          <button type="button" class="ready-row" onclick={() => navigate(`/issues/${encodeURIComponent(issue.id)}`)}>
            <span class="queue-rank">{index + 1}</span>
            <span class="ready-copy">
              <strong>{issue.title}</strong>
              <small>{issue.id} · {issue.type}{project === ALL_PROJECTS ? ` · ${issue.project}` : ''}</small>
            </span>
            <span class="status-pill">{issue.status}</span>
            <span class="priority-pill">P{issue.priority}</span>
          </button>
        </li>
      {/each}
    </ul>
  {/if}
</section>
