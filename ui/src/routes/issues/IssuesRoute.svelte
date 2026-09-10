<script lang="ts">
  import { ALL_PROJECTS, ApiError, api, type Issue, type ListIssuesParams, type WorkflowInfo } from '../../lib/api'
  import EmptyState from '../../lib/components/EmptyState.svelte'
  import ErrorState from '../../lib/components/ErrorState.svelte'
  import LoadingState from '../../lib/components/LoadingState.svelte'
  import { emptyIssueForm, issueFormToCreateRequest, validateIssueForm, type IssueForm } from './form'

  interface Props {
    project: string
    workflow: WorkflowInfo
    navigate: (path: string) => void
    reloadKey: number
    onBanner: (message: string) => void
  }

  let { project, workflow, navigate, reloadKey, onBanner }: Props = $props()

  let issues = $state<Issue[]>([])
  let loading = $state(false)
  let error = $state('')

  let view = $state<'board' | 'list'>('board')
  let filterType = $state('')
  let filterLabel = $state('')
  let filterQuery = $state('')
  let showClosed = $state(false)

  let showNewForm = $state(false)
  let form = $state<IssueForm>(emptyIssueForm())
  let formError = $state('')
  let creating = $state(false)

  // Issues in a terminal status (e.g. closed/done) are hidden from both the
  // board and the list until "show closed" is on, mirroring the archived=true
  // request already made to the API for that toggle.
  const visibleIssues = $derived.by(() =>
    showClosed ? issues : issues.filter((issue) => !workflow.terminal.includes(issue.status)),
  )

  const grouped = $derived.by(() => {
    const order = workflow.statuses.length > 0 ? workflow.statuses : Array.from(new Set(visibleIssues.map((issue) => issue.status)))
    const columns = new Map<string, Issue[]>()
    for (const status of order) {
      if (!showClosed && workflow.terminal.includes(status)) {
        continue
      }
      columns.set(status, [])
    }
    for (const issue of visibleIssues) {
      if (!columns.has(issue.status)) {
        columns.set(issue.status, [])
      }
      columns.get(issue.status)!.push(issue)
    }
    return Array.from(columns.entries())
  })

  $effect(() => {
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
    const params: ListIssuesParams = {
      type: filterType || undefined,
      label: filterLabel || undefined,
      q: filterQuery || undefined,
      archived: showClosed || undefined,
    }
    try {
      const result = await api.listIssues(project, params)
      if (isCancelled()) return
      issues = result
    } catch (err) {
      if (isCancelled()) return
      error = errorMessage(err)
    } finally {
      if (!isCancelled()) loading = false
    }
  }

  async function submitNewIssue(event: SubmitEvent) {
    event.preventDefault()
    formError = validateIssueForm(form)
    if (formError !== '') {
      return
    }
    creating = true
    try {
      const result = await api.createIssue(project, issueFormToCreateRequest(form))
      showNewForm = false
      form = emptyIssueForm()
      onBanner(result.pushed ? '' : result.message)
      await load()
      navigate(`/issues/${encodeURIComponent(result.id)}`)
    } catch (err) {
      formError = errorMessage(err)
    } finally {
      creating = false
    }
  }

  function errorMessage(err: unknown): string {
    if (err instanceof ApiError) {
      return err.message
    }
    return err instanceof Error ? err.message : 'Request failed.'
  }
</script>

<section class="workspace issues-workspace" aria-label="Issues workspace">
  <div class="toolbar">
    <label>
      Type
      <input type="text" bind:value={filterType} placeholder="bug, feature..." />
    </label>
    <label>
      Label
      <input type="text" bind:value={filterLabel} placeholder="ui, backend..." />
    </label>
    <label>
      Search
      <input type="text" bind:value={filterQuery} placeholder="Search title and body" />
    </label>
    <label class="checkbox-field">
      <input type="checkbox" bind:checked={showClosed} />
      Show closed
    </label>
    <div class="view-toggle" role="group" aria-label="Board or list view">
      <button type="button" class:active={view === 'board'} onclick={() => (view = 'board')}>Board</button>
      <button type="button" class:active={view === 'list'} onclick={() => (view = 'list')}>List</button>
    </div>
    {#if project !== ALL_PROJECTS}
      <button type="button" class="secondary" onclick={() => (showNewForm = !showNewForm)}>
        {showNewForm ? 'Cancel' : 'New issue'}
      </button>
    {/if}
  </div>

  {#if project === ALL_PROJECTS}
    <p class="form-error" role="note">Select a single project to create an issue.</p>
  {/if}

  {#if showNewForm}
    <form class="issue-form" onsubmit={submitNewIssue}>
      <h2>New issue</h2>
      {#if formError !== ''}
        <p class="form-error" role="alert">{formError}</p>
      {/if}
      <label>
        Title
        <input type="text" bind:value={form.title} required maxlength="300" />
      </label>
      <label>
        Description
        <textarea bind:value={form.description} maxlength="20000"></textarea>
      </label>
      <div class="form-grid">
        <label>
          Priority
          <input type="number" min="0" max="4" bind:value={form.priority} />
        </label>
        <label>
          Type
          <input type="text" bind:value={form.type} />
        </label>
        <label>
          Assignee
          <input type="text" bind:value={form.assignee} />
        </label>
        <label>
          Parent id
          <input type="text" bind:value={form.parent} />
        </label>
        <label>
          Labels (comma separated)
          <input type="text" bind:value={form.labels} />
        </label>
        <label>
          Blocked by (comma separated ids)
          <input type="text" bind:value={form.blocked_by} />
        </label>
        <label>
          URL
          <input type="text" bind:value={form.url} />
        </label>
      </div>
      <div class="actions">
        <button type="submit" disabled={creating}>{creating ? 'Creating…' : 'Create issue'}</button>
      </div>
    </form>
  {/if}

  {#if loading && issues.length === 0}
    <LoadingState message="Loading issues" />
  {:else if error !== '' && issues.length === 0}
    <ErrorState title="Could not load issues" message={error} />
  {:else if visibleIssues.length === 0}
    <EmptyState
      title="No issues found"
      message={issues.length > 0
        ? 'All matching issues are closed. Turn on "Show closed" to see them.'
        : 'Adjust the filters or create the first issue.'}
    />
  {:else}
    {#if error !== ''}
      <p class="form-error" role="alert">{error}</p>
    {/if}
    {#if view === 'board'}
      <div class="board" aria-label="Issues board">
        {#each grouped as [status, items] (status)}
          <div class="board-column">
            <div class="board-column-header">
              <strong>{status}</strong>
              <span>{items.length}</span>
            </div>
            <div class="board-cards">
              {#each items as issue (issue.id)}
                <button type="button" class="board-card" onclick={() => navigate(`/issues/${encodeURIComponent(issue.id)}`)}>
                  <strong>{issue.title}</strong>
                  <span class="board-card-meta">
                    <span>{issue.id}</span>
                    <span class="priority-pill">P{issue.priority}</span>
                    {#if project === ALL_PROJECTS}
                      <span class="project-pill">{issue.project}</span>
                    {/if}
                  </span>
                </button>
              {/each}
            </div>
          </div>
        {/each}
      </div>
    {:else}
      <div class="issue-table" aria-label="Issues list">
        {#each visibleIssues as issue (issue.id)}
          <button type="button" class="issue-row" onclick={() => navigate(`/issues/${encodeURIComponent(issue.id)}`)}>
            <span>
              <strong>{issue.title}</strong>
              <small>{issue.id}{project === ALL_PROJECTS ? ` · ${issue.project}` : ''}</small>
            </span>
            <span class="status-pill">{issue.status}</span>
            <span class="priority-pill">P{issue.priority}</span>
            <span class="type-pill">{issue.type}</span>
          </button>
        {/each}
      </div>
    {/if}
  {/if}
</section>
