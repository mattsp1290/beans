<script lang="ts">
  import { onMount } from 'svelte'

  import {
    ApiError,
    api,
    type Issue,
    type IssueState,
  } from '../../lib/api'
  import EmptyState from '../../lib/components/EmptyState.svelte'
  import ErrorState from '../../lib/components/ErrorState.svelte'
  import LoadingState from '../../lib/components/LoadingState.svelte'
  import {
    emptyIssueForm,
    issueFormToCreateRequest,
    issueFormToUpdateRequest,
    issueToIssueForm,
    validateIssueForm,
  } from './form'

  interface Props {
    pathname: string
    navigate: (path: string) => void
  }

  type Mode = 'list' | 'create' | 'detail' | 'edit'

  let { pathname, navigate }: Props = $props()

  let issues = $state<Issue[]>([])
  let dependencyCandidates = $state<Issue[]>([])
  let selectedIssue = $state<Issue | null>(null)
  let listLoading = $state(false)
  let detailLoading = $state(false)
  let saving = $state(false)
  let error = $state('')
  let stateFilter = $state<'all' | IssueState>('all')
  let search = $state('')
  let formError = $state('')
  let dependencyError = $state('')
  let dependencyInput = $state('')
  let dependencySaving = $state(false)
  let form = $state(emptyIssueForm())

  const route = $derived(parseIssueRoute(pathname))
  const visibleIssues = $derived(filterIssues(issues, search))
  const dependencyOptions = $derived(availableDependencyOptions(dependencyCandidates, selectedIssue))

  onMount(() => {
    void loadIssues()
    void loadDependencyCandidates()
  })

  $effect(() => {
    if (route.mode === 'detail' || route.mode === 'edit') {
      void loadIssue(route.id)
    } else {
      selectedIssue = null
    }
  })

  async function loadIssues() {
    listLoading = true
    error = ''
    try {
      const response = await api.listIssues({
        state: stateFilter === 'all' ? undefined : stateFilter,
      })
      issues = response.issues
      syncSelectedIssueFromList()
    } catch (err) {
      error = errorMessage(err)
    } finally {
      listLoading = false
    }
  }

  async function loadDependencyCandidates() {
    try {
      const response = await api.listIssues()
      dependencyCandidates = response.issues
      syncSelectedIssueFromCandidates()
    } catch (err) {
      if (selectedIssue) {
        dependencyError = errorMessage(err)
      }
    }
  }

  async function loadIssue(id: string) {
    detailLoading = true
    error = ''
    try {
      selectedIssue = await api.getIssue(id)
      dependencyInput = defaultDependencyInput(selectedIssue, dependencyCandidates)
      dependencyError = ''
      if (route.mode === 'edit') {
        form = issueToIssueForm(selectedIssue)
      }
    } catch (err) {
      error = errorMessage(err)
      selectedIssue = null
    } finally {
      detailLoading = false
    }
  }

  function startCreate() {
    form = emptyIssueForm()
    formError = ''
    navigate('/issues/new')
  }

  function startEdit(issue: Issue) {
    form = issueToIssueForm(issue)
    formError = ''
    navigate(`/issues/${issue.id}/edit`)
  }

  async function addDependency(event: SubmitEvent) {
    event.preventDefault()
    if (!selectedIssue || dependencyInput === '') {
      dependencyError = 'Choose an issue to add as a blocker.'
      return
    }
    dependencySaving = true
    dependencyError = ''
    try {
      await api.addDependency(selectedIssue.id, { blocked_by_id: dependencyInput })
      await refreshSelectedIssue(selectedIssue.id)
    } catch (err) {
      dependencyError = errorMessage(err)
    } finally {
      dependencySaving = false
    }
  }

  async function removeDependency(blockedById: string) {
    if (!selectedIssue) {
      return
    }
    dependencySaving = true
    dependencyError = ''
    try {
      await api.removeDependency(selectedIssue.id, blockedById)
      await refreshSelectedIssue(selectedIssue.id)
    } catch (err) {
      dependencyError = errorMessage(err)
    } finally {
      dependencySaving = false
    }
  }

  async function submitIssue(event: SubmitEvent) {
    event.preventDefault()
    formError = validateIssueForm(form)
    if (formError !== '') {
      return
    }
    saving = true
    try {
      const issue =
        route.mode === 'edit' && route.id !== ''
          ? await api.updateIssue(route.id, issueFormToUpdateRequest(form))
          : await api.createIssue(issueFormToCreateRequest(form))
      await loadIssues()
      navigate(`/issues/${issue.id}`)
    } catch (err) {
      formError = errorMessage(err)
    } finally {
      saving = false
    }
  }

  async function closeIssue(issue: Issue) {
    if (!window.confirm(`Close ${issue.id}?`)) {
      return
    }
    try {
      selectedIssue = await api.closeIssue(issue.id, { reason: 'completed from UI' })
      await loadIssues()
    } catch (err) {
      error = errorMessage(err)
    }
  }

  async function deleteIssue(issue: Issue) {
    if (!window.confirm(`Delete ${issue.id}?`)) {
      return
    }
    try {
      await api.deleteIssue(issue.id)
      await loadIssues()
      navigate('/')
    } catch (err) {
      error = errorMessage(err)
    }
  }

  function setStateFilter(value: 'all' | IssueState) {
    stateFilter = value
    void loadIssues()
  }

  async function refreshSelectedIssue(id: string) {
    selectedIssue = await api.getIssue(id)
    dependencyInput = defaultDependencyInput(selectedIssue, dependencyCandidates)
    void refreshIssueLists()
  }

  async function refreshIssueLists() {
    await Promise.allSettled([loadIssues(), loadDependencyCandidates()])
  }

  function parseIssueRoute(path: string): { mode: Mode; id: string } {
    if (path === '/issues/new') {
      return { mode: 'create', id: '' }
    }
    const edit = path.match(/^\/issues\/([^/]+)\/edit$/)
    if (edit) {
      return { mode: 'edit', id: decodeURIComponent(edit[1]) }
    }
    const detail = path.match(/^\/issues\/([^/]+)$/)
    if (detail) {
      return { mode: 'detail', id: decodeURIComponent(detail[1]) }
    }
    return { mode: 'list', id: '' }
  }

  function filterIssues(items: Issue[], query: string): Issue[] {
    const needle = query.trim().toLowerCase()
    if (needle === '') {
      return items
    }
    return items.filter((issue) =>
      [issue.id, issue.title, issue.state, issue.issue_type, ...issue.labels]
        .join(' ')
        .toLowerCase()
        .includes(needle),
    )
  }

  function availableDependencyOptions(items: Issue[], issue: Issue | null): Issue[] {
    if (!issue) {
      return []
    }
    const existing = new Set(issue.blocked_by)
    return items
      .filter((candidate) => candidate.id !== issue.id && !existing.has(candidate.id))
      .sort((left, right) => left.priority - right.priority || left.title.localeCompare(right.title))
  }

  function defaultDependencyInput(issue: Issue | null, items: Issue[]): string {
    return availableDependencyOptions(items, issue)[0]?.id ?? ''
  }

  function syncSelectedIssueFromList() {
    if (!selectedIssue) {
      return
    }
    const updated = issues.find((issue) => issue.id === selectedIssue?.id)
    if (updated) {
      selectedIssue = updated
      if (
        dependencyInput === '' ||
        !availableDependencyOptions(dependencyCandidates, selectedIssue).some((issue) => issue.id === dependencyInput)
      ) {
        dependencyInput = defaultDependencyInput(selectedIssue, dependencyCandidates)
      }
    }
  }

  function syncSelectedIssueFromCandidates() {
    if (!selectedIssue) {
      return
    }
    const updated = dependencyCandidates.find((issue) => issue.id === selectedIssue?.id)
    if (updated) {
      selectedIssue = updated
    }
    if (
      dependencyInput === '' ||
      !availableDependencyOptions(dependencyCandidates, selectedIssue).some((issue) => issue.id === dependencyInput)
    ) {
      dependencyInput = defaultDependencyInput(selectedIssue, dependencyCandidates)
    }
  }

  function issueLabel(id: string): string {
    const issue = dependencyCandidates.find((item) => item.id === id) ?? issues.find((item) => item.id === id)
    return issue ? `${issue.title} (${issue.id})` : id
  }

  function errorMessage(err: unknown): string {
    if (err instanceof ApiError) {
      return err.fields?.map((field) => `${field.field}: ${field.message}`).join(', ') || err.message
    }
    return err instanceof Error ? err.message : 'Request failed.'
  }
</script>

<section class="issues-layout" aria-label="Issues workspace">
  <div class="workspace issues-list">
    <div class="toolbar">
      <label>
        <span>Filter</span>
        <input bind:value={search} type="search" placeholder="Title, label, or id" />
      </label>
      <select
        aria-label="State"
        value={stateFilter}
        onchange={(event) => setStateFilter(event.currentTarget.value as 'all' | IssueState)}
      >
        <option value="all">All states</option>
        <option value="open">Open</option>
        <option value="in_progress">In progress</option>
        <option value="blocked">Blocked</option>
        <option value="closed">Closed</option>
        <option value="done">Done</option>
      </select>
      <button type="button" onclick={startCreate}>New issue</button>
    </div>

    {#if listLoading}
      <LoadingState message="Loading issues" />
    {:else if error !== '' && issues.length === 0}
      <ErrorState title="Could not load issues" message={error} />
    {:else if visibleIssues.length === 0}
      <EmptyState title="No issues found" message="Create an issue or adjust the current filters." />
    {:else}
      <div class="issue-table" role="list" aria-label="Issues">
        {#each visibleIssues as issue}
          <button
            type="button"
            class:active={route.id === issue.id}
            class="issue-row"
            onclick={() => navigate(`/issues/${issue.id}`)}
          >
            <span>
              <strong>{issue.title}</strong>
              <small>{issue.id}</small>
            </span>
            <span>{issue.state}</span>
            <span>P{issue.priority}</span>
          </button>
        {/each}
      </div>
    {/if}
  </div>

  <div class="workspace issue-panel">
    {#if route.mode === 'create' || route.mode === 'edit'}
      <form class="issue-form" onsubmit={submitIssue}>
        <div>
          <h2>{route.mode === 'edit' ? 'Edit issue' : 'Create issue'}</h2>
          <p>{route.mode === 'edit' ? route.id : 'Add work to the current project.'}</p>
        </div>

        {#if formError !== ''}
          <p class="form-error">{formError}</p>
        {/if}

        <label>
          <span>Title</span>
          <input bind:value={form.title} required maxlength="300" />
        </label>

        <label>
          <span>Description</span>
          <textarea bind:value={form.description} maxlength="20000"></textarea>
        </label>

        <div class="form-grid">
          <label>
            <span>Priority</span>
            <input bind:value={form.priority} type="number" min="0" max="4" />
          </label>

          {#if route.mode === 'create'}
            <label>
              <span>Type</span>
              <select bind:value={form.issue_type}>
                <option value="bug">Bug</option>
                <option value="feature">Feature</option>
                <option value="task">Task</option>
                <option value="epic">Epic</option>
                <option value="chore">Chore</option>
              </select>
            </label>
          {:else}
            <label>
              <span>Type</span>
              <input value={form.issue_type} disabled />
            </label>
          {/if}
        </div>

        <label>
          <span>Labels</span>
          <input bind:value={form.labels} placeholder="ui, api" />
        </label>

        <label>
          <span>Branch</span>
          <input bind:value={form.branch_name} maxlength="255" />
        </label>

        <label>
          <span>URL</span>
          <input bind:value={form.url} type="url" maxlength="2048" />
        </label>

        <div class="actions">
          <button disabled={saving} type="submit">{saving ? 'Saving' : 'Save issue'}</button>
          <button type="button" class="secondary" onclick={() => navigate(route.id ? `/issues/${route.id}` : '/')}>
            Cancel
          </button>
        </div>
      </form>
    {:else if detailLoading}
      <LoadingState message="Loading issue" />
    {:else if route.mode === 'detail' && selectedIssue}
      <article class="issue-detail">
        {#if error !== ''}
          <p class="form-error">{error}</p>
        {/if}
        <div>
          <h2>{selectedIssue.title}</h2>
          <p>{selectedIssue.id} · {selectedIssue.issue_type} · P{selectedIssue.priority}</p>
        </div>
        <p class="status-pill">{selectedIssue.state}</p>
        <p>{selectedIssue.description || 'No description.'}</p>
        <div class="label-row">
          {#each selectedIssue.labels as label}
            <span>{label}</span>
          {/each}
        </div>
        <dl>
          <div><dt>Created</dt><dd>{new Date(selectedIssue.created_at).toLocaleString()}</dd></div>
          <div><dt>Updated</dt><dd>{new Date(selectedIssue.updated_at).toLocaleString()}</dd></div>
        </dl>
        <section class="dependency-editor" aria-label="Dependencies">
          <div>
            <h3>Blocked by</h3>
            <p>Issues that must close before this work is ready.</p>
          </div>

          {#if dependencyError !== ''}
            <p class="form-error" role="alert">{dependencyError}</p>
          {/if}

          {#if selectedIssue.blocked_by.length === 0}
            <p class="muted">No blockers.</p>
          {:else}
            <ul class="dependency-list" aria-label="Current blockers">
              {#each selectedIssue.blocked_by as blockedById}
                <li>
                  <span>{issueLabel(blockedById)}</span>
                  <button
                    type="button"
                    class="secondary"
                    disabled={dependencySaving}
                    aria-label={`Remove blocker ${issueLabel(blockedById)}`}
                    onclick={() => removeDependency(blockedById)}
                  >
                    Remove
                  </button>
                </li>
              {/each}
            </ul>
          {/if}

          <form class="dependency-form" onsubmit={addDependency}>
            <label>
              <span>Add blocker</span>
              <select bind:value={dependencyInput} disabled={dependencyOptions.length === 0 || dependencySaving}>
                {#if dependencyOptions.length === 0}
                  <option value="">No available issues</option>
                {:else}
                  {#each dependencyOptions as issue}
                    <option value={issue.id}>{issue.title} ({issue.id})</option>
                  {/each}
                {/if}
              </select>
            </label>
            <button type="submit" disabled={dependencyOptions.length === 0 || dependencySaving}>
              {dependencySaving ? 'Updating' : 'Add blocker'}
            </button>
          </form>
        </section>
        <div class="actions">
          <button type="button" onclick={() => startEdit(selectedIssue!)}>Edit</button>
          <button type="button" class="secondary" onclick={() => closeIssue(selectedIssue!)}>Close</button>
          <button type="button" class="danger" onclick={() => deleteIssue(selectedIssue!)}>Delete</button>
        </div>
      </article>
    {:else if error !== ''}
      <ErrorState title="Could not load issue" message={error} />
    {:else}
      <EmptyState title="Select an issue" message="Choose an issue from the list or create a new one." />
    {/if}
  </div>
</section>
