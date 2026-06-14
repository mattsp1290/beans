<script lang="ts">
  import { onMount } from 'svelte'

  import { ApiError, api, type Issue } from '../../lib/api'
  import EmptyState from '../../lib/components/EmptyState.svelte'
  import ErrorState from '../../lib/components/ErrorState.svelte'
  import LoadingState from '../../lib/components/LoadingState.svelte'

  interface Props {
    navigate: (path: string) => void
  }

  let { navigate }: Props = $props()

  let issues = $state<Issue[]>([])
  let loading = $state(false)
  let error = $state('')
  let refreshedAt = $state<Date | null>(null)

  onMount(() => {
    void loadReadyQueue()
  })

  async function loadReadyQueue() {
    loading = true
    error = ''
    try {
      const response = await api.ready()
      issues = response.issues
      refreshedAt = new Date()
    } catch (err) {
      error = errorMessage(err)
    } finally {
      loading = false
    }
  }

  function errorMessage(err: unknown): string {
    if (err instanceof ApiError) {
      return err.fields?.map((field) => `${field.field}: ${field.message}`).join(', ') || err.message
    }
    return err instanceof Error ? err.message : 'Request failed.'
  }

  function ageLabel(issue: Issue): string {
    const updatedAt = new Date(issue.updated_at).getTime()
    const elapsed = Math.max(0, Date.now() - updatedAt)
    const minutes = Math.floor(elapsed / 60000)
    if (minutes < 1) {
      return 'Updated just now'
    }
    if (minutes < 60) {
      return `Updated ${minutes}m ago`
    }
    const hours = Math.floor(minutes / 60)
    if (hours < 24) {
      return `Updated ${hours}h ago`
    }
    return `Updated ${Math.floor(hours / 24)}d ago`
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
    <button type="button" class="secondary" disabled={loading} onclick={loadReadyQueue}>
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
      <p class="form-error">{error}</p>
    {/if}
    <div class="ready-list" role="list" aria-label="Ready issues">
      {#each issues as issue, index}
        <button type="button" class="ready-row" onclick={() => navigate(`/issues/${issue.id}`)}>
          <span class="queue-rank">{index + 1}</span>
          <span class="ready-copy">
            <strong>{issue.title}</strong>
            <small>{issue.id} · {issue.issue_type} · {ageLabel(issue)}</small>
          </span>
          <span class="status-pill">{issue.state}</span>
          <span class="priority-pill">P{issue.priority}</span>
        </button>
      {/each}
    </div>
  {/if}
</section>
