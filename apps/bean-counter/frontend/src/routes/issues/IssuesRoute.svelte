<script lang="ts">
  import { onMount } from 'svelte'

  import { ApiError, api, type CreateIssueRequest, type Issue, type IssueState, type IssueType } from '../../lib/api'
  import EmptyState from '../../lib/components/EmptyState.svelte'
  import ErrorState from '../../lib/components/ErrorState.svelte'
  import LoadingState from '../../lib/components/LoadingState.svelte'

  interface Props {
    pathname: string
    navigate: (path: string) => void
  }

  type Mode = 'list' | 'create' | 'detail' | 'edit'

  let { pathname, navigate }: Props = $props()

  let issues = $state<Issue[]>([])
  let selectedIssue = $state<Issue | null>(null)
  let listLoading = $state(false)
  let detailLoading = $state(false)
  let saving = $state(false)
  let error = $state('')
  let stateFilter = $state<'all' | IssueState>('all')
  let search = $state('')
  let formError = $state('')
  let form = $state({
    title: '',
    description: '',
    priority: 2,
    issue_type: 'task' as IssueType,
    labels: '',
    branch_name: '',
    url: '',
  })

  const route = $derived(parseIssueRoute(pathname))
  const visibleIssues = $derived(filterIssues(issues, search))

  onMount(() => {
    void loadIssues()
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
    } catch (err) {
      error = errorMessage(err)
    } finally {
      listLoading = false
    }
  }

  async function loadIssue(id: string) {
    detailLoading = true
    error = ''
    try {
      selectedIssue = await api.getIssue(id)
      if (route.mode === 'edit') {
        form = issueToForm(selectedIssue)
      }
    } catch (err) {
      error = errorMessage(err)
      selectedIssue = null
    } finally {
      detailLoading = false
    }
  }

  function startCreate() {
    form = emptyForm()
    formError = ''
    navigate('/issues/new')
  }

  function startEdit(issue: Issue) {
    form = issueToForm(issue)
    formError = ''
    navigate(`/issues/${issue.id}/edit`)
  }

  async function submitIssue(event: SubmitEvent) {
    event.preventDefault()
    formError = validateForm(form)
    if (formError !== '') {
      return
    }
    saving = true
    try {
      const input = formToRequest(form)
      const issue =
        route.mode === 'edit' && route.id !== ''
          ? await api.updateIssue(route.id, input)
          : await api.createIssue(input as CreateIssueRequest)
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

  function emptyForm() {
    return {
      title: '',
      description: '',
      priority: 2,
      issue_type: 'task' as IssueType,
      labels: '',
      branch_name: '',
      url: '',
    }
  }

  function issueToForm(issue: Issue) {
    return {
      title: issue.title,
      description: issue.description,
      priority: issue.priority,
      issue_type: issue.issue_type,
      labels: issue.labels.join(', '),
      branch_name: issue.branch_name ?? '',
      url: issue.url ?? '',
    }
  }

  function formToRequest(value: typeof form) {
    const labels = value.labels
      .split(',')
      .map((label) => label.trim())
      .filter(Boolean)
    return {
      title: value.title.trim(),
      description: value.description,
      priority: Number(value.priority),
      issue_type: value.issue_type,
      labels,
      branch_name: value.branch_name.trim() || undefined,
      url: value.url.trim() || undefined,
    }
  }

  function validateForm(value: typeof form): string {
    if (value.title.trim() === '') {
      return 'Title is required.'
    }
    if (value.title.length > 300) {
      return 'Title must be at most 300 characters.'
    }
    if (value.description.length > 20000) {
      return 'Description must be at most 20000 characters.'
    }
    if (value.priority < 0 || value.priority > 4) {
      return 'Priority must be between 0 and 4.'
    }
    const labels = value.labels.split(',').map((label) => label.trim())
    if (labels.filter(Boolean).length > 100) {
      return 'Use at most 100 labels.'
    }
    if (labels.some((label) => label.length > 100)) {
      return 'Labels must be at most 100 characters.'
    }
    if (value.branch_name.length > 255) {
      return 'Branch name must be at most 255 characters.'
    }
    if (value.url.trim() !== '' && !/^https?:\/\//.test(value.url.trim())) {
      return 'URL must start with http:// or https://.'
    }
    return ''
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
          <div><dt>Blocked by</dt><dd>{selectedIssue.blocked_by.join(', ') || 'None'}</dd></div>
          <div><dt>Created</dt><dd>{new Date(selectedIssue.created_at).toLocaleString()}</dd></div>
          <div><dt>Updated</dt><dd>{new Date(selectedIssue.updated_at).toLocaleString()}</dd></div>
        </dl>
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
