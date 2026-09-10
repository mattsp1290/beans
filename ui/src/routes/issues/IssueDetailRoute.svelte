<script lang="ts">
  import { ApiError, api, type DependencyKind, type IssueDetail } from '../../lib/api'
  import EmptyState from '../../lib/components/EmptyState.svelte'
  import ErrorState from '../../lib/components/ErrorState.svelte'
  import LoadingState from '../../lib/components/LoadingState.svelte'
  import { backlinkHref, handleRenderedContentClick } from '../../lib/nav'

  interface Props {
    id: string
    navigate: (path: string) => void
    reloadKey: number
    onBanner: (message: string) => void
  }

  let { id, navigate, reloadKey, onBanner }: Props = $props()

  let issue = $state<IssueDetail | null>(null)
  let loading = $state(false)
  let error = $state('')
  let saving = $state(false)

  let editingTitle = $state(false)
  let titleDraft = $state('')
  let newLabel = $state('')
  let newBlockedBy = $state('')
  let newChild = $state('')
  let noteText = $state('')

  $effect(() => {
    id
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
      const result = await api.getIssue(id)
      if (isCancelled()) return
      issue = result
    } catch (err) {
      if (isCancelled()) return
      error = errorMessage(err)
    } finally {
      if (!isCancelled()) loading = false
    }
  }

  async function patch(body: Parameters<typeof api.updateIssue>[1]) {
    saving = true
    try {
      const result = await api.updateIssue(id, body)
      onBanner(result.pushed ? '' : result.message)
      await load()
    } catch (err) {
      error = errorMessage(err)
    } finally {
      saving = false
    }
  }

  function startEditTitle() {
    if (!issue) return
    titleDraft = issue.title
    editingTitle = true
  }

  async function saveTitle() {
    editingTitle = false
    if (issue && titleDraft.trim() !== '' && titleDraft.trim() !== issue.title) {
      await patch({ title: titleDraft.trim() })
    }
  }

  async function addLabel() {
    const value = newLabel.trim()
    if (value === '') return
    newLabel = ''
    await patch({ add_labels: [value] })
  }

  async function removeLabel(label: string) {
    await patch({ remove_labels: [label] })
  }

  async function addBlocker(target: string) {
    const value = target.trim()
    if (value === '') return
    saving = true
    try {
      const result = await api.addDependency(id, { target: value, type: 'blocks' })
      onBanner(result.pushed ? '' : result.message)
      await load()
    } catch (err) {
      error = errorMessage(err)
    } finally {
      saving = false
    }
  }

  async function removeBlocker(target: string, type: DependencyKind) {
    saving = true
    try {
      const result = await api.removeDependency(id, target, type)
      onBanner(result.pushed ? '' : result.message)
      await load()
    } catch (err) {
      error = errorMessage(err)
    } finally {
      saving = false
    }
  }

  async function addChild(childId: string) {
    const value = childId.trim()
    if (value === '') return
    saving = true
    try {
      const result = await api.addChild(id, value)
      onBanner(result.pushed ? '' : result.message)
      await load()
    } catch (err) {
      error = errorMessage(err)
    } finally {
      saving = false
    }
  }

  async function removeChild(childId: string) {
    saving = true
    try {
      const result = await api.removeChild(id, childId)
      onBanner(result.pushed ? '' : result.message)
      await load()
    } catch (err) {
      error = errorMessage(err)
    } finally {
      saving = false
    }
  }

  async function submitNote(event: SubmitEvent) {
    event.preventDefault()
    const text = noteText.trim()
    if (text === '') return
    saving = true
    try {
      const result = await api.addNote(id, text)
      noteText = ''
      onBanner(result.pushed ? '' : result.message)
      await load()
    } catch (err) {
      error = errorMessage(err)
    } finally {
      saving = false
    }
  }

  async function closeIssue() {
    const input = window.prompt('Reason for closing (required):')
    if (input === null) {
      return
    }
    const reason = input.trim()
    if (reason === '') {
      error = 'A reason is required to close this issue.'
      return
    }
    saving = true
    try {
      const result = await api.closeIssue(id, reason)
      onBanner(result.pushed ? '' : result.message)
      await load()
    } catch (err) {
      error = errorMessage(err)
    } finally {
      saving = false
    }
  }

  async function reopenIssue() {
    saving = true
    try {
      const result = await api.reopenIssue(id)
      onBanner(result.pushed ? '' : result.message)
      await load()
    } catch (err) {
      error = errorMessage(err)
    } finally {
      saving = false
    }
  }

  function isTerminal(status: string): boolean {
    return issue?.workflow.terminal.includes(status) ?? false
  }

  function goTo(path: string) {
    return (event: MouseEvent) => {
      event.preventDefault()
      navigate(path)
    }
  }

  function errorMessage(err: unknown): string {
    if (err instanceof ApiError) {
      return err.message
    }
    return err instanceof Error ? err.message : 'Request failed.'
  }
</script>

{#if loading && !issue}
  <LoadingState message="Loading issue" />
{:else if error !== '' && !issue}
  <ErrorState title="Could not load issue" message={error} />
{:else if !issue}
  <EmptyState title="Issue not found" message="This issue may have been moved or removed." />
{:else}
  <section class="workspace issue-detail" aria-label="Issue detail">
    <button type="button" class="secondary" onclick={() => navigate('/issues')}>&larr; Back to issues</button>

    {#if error !== ''}
      <p class="form-error" role="alert">{error}</p>
    {/if}

    <div>
      {#if editingTitle}
        <input
          type="text"
          bind:value={titleDraft}
          onblur={saveTitle}
          onkeydown={(event) => {
            if (event.key === 'Enter') saveTitle()
            if (event.key === 'Escape') editingTitle = false
          }}
        />
      {:else}
        <div class="title-row">
          <h2>{issue.title}</h2>
          <button type="button" class="secondary" onclick={startEditTitle}>Edit</button>
        </div>
      {/if}
      <p class="muted">{issue.id} · {issue.project} · {issue.path}</p>
    </div>

    <div class="form-grid">
      <label>
        Status
        <select value={issue.status} disabled={saving} onchange={(e) => patch({ status: (e.target as HTMLSelectElement).value })}>
          {#each issue.workflow.statuses as status}
            <option value={status}>{status}</option>
          {/each}
        </select>
      </label>
      <label>
        Priority
        <input
          type="number"
          min="0"
          max="4"
          value={issue.priority}
          disabled={saving}
          onchange={(e) => patch({ priority: Number((e.target as HTMLInputElement).value) })}
        />
      </label>
      <label>
        Type
        <input
          type="text"
          value={issue.type}
          disabled={saving}
          onchange={(e) => patch({ type: (e.target as HTMLInputElement).value })}
        />
      </label>
      <label>
        Assignee
        <input
          type="text"
          value={issue.assignee}
          disabled={saving}
          onchange={(e) => patch({ assignee: (e.target as HTMLInputElement).value })}
        />
      </label>
      <label>
        Parent id
        <input
          type="text"
          value={issue.parent}
          disabled={saving}
          onchange={(e) => patch({ parent: (e.target as HTMLInputElement).value })}
        />
      </label>
    </div>

    <div class="label-row">
      {#each issue.labels as label (label)}
        <span class="removable">
          {label}
          <button type="button" aria-label={`Remove label ${label}`} onclick={() => removeLabel(label)}>&times;</button>
        </span>
      {/each}
      <input type="text" bind:value={newLabel} placeholder="add label" style="min-height: 28px; width: 120px;" />
      <button type="button" class="secondary" onclick={addLabel}>Add label</button>
    </div>

    <div class="detail-section">
      <h3>Description</h3>
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div class="rendered" onclick={(event) => handleRenderedContentClick(event, navigate)}>{@html issue.html}</div>
    </div>

    <div class="dependency-editor">
      <h3>Blockers</h3>
      {#if !issue.blockers || issue.blockers.length === 0}
        <p class="muted">No blockers.</p>
      {:else}
        <ul class="dependency-list">
          {#each issue.blockers as blocker (blocker.id)}
            <li>
              <span>
                {#if blocker.missing}
                  <span class="status-pill missing">missing</span>
                {:else}
                  <a href={`/issues/${encodeURIComponent(blocker.id)}`} onclick={goTo(`/issues/${encodeURIComponent(blocker.id)}`)}>
                    {blocker.title ?? blocker.id}
                  </a>
                  {#if blocker.project}<span class="project-pill">{blocker.project}</span>{/if}
                  {#if blocker.status}<span class="status-pill">{blocker.status}</span>{/if}
                {/if}
                <span>{blocker.id}</span>
              </span>
              <button type="button" class="secondary" disabled={saving} onclick={() => removeBlocker(blocker.id, 'blocks')}>
                Remove
              </button>
            </li>
          {/each}
        </ul>
      {/if}
      <div class="dependency-form">
        <label>
          Add blocker id
          <input type="text" bind:value={newBlockedBy} />
        </label>
        <button type="button" class="secondary" disabled={saving} onclick={() => { void addBlocker(newBlockedBy); newBlockedBy = '' }}>
          Add
        </button>
      </div>
    </div>

    <div class="detail-section">
      <h3>Children</h3>
      {#if !issue.children || issue.children.length === 0}
        <p class="muted">No child issues.</p>
      {:else}
        <ul class="dependency-list">
          {#each issue.children as child (child.id)}
            <li>
              <span>
                <a href={`/issues/${encodeURIComponent(child.id)}`} onclick={goTo(`/issues/${encodeURIComponent(child.id)}`)}>
                  {child.title}
                </a>
                <span class="project-pill">{child.project}</span>
                <span class="status-pill">{child.status}</span>
                <span>{child.id}</span>
              </span>
              <button type="button" class="secondary" disabled={saving} onclick={() => removeChild(child.id)}>
                Unlink
              </button>
            </li>
          {/each}
        </ul>
      {/if}
      <div class="dependency-form">
        <label>
          Add child id
          <input type="text" bind:value={newChild} />
        </label>
        <button type="button" class="secondary" disabled={saving} onclick={() => { void addChild(newChild); newChild = '' }}>
          Add
        </button>
      </div>
    </div>

    {#if issue.backlinks && issue.backlinks.length > 0}
      <div class="detail-section">
        <h3>Backlinks</h3>
        <ul class="plain-list">
          {#each issue.backlinks as link, index (index)}
            <li>
              <a href={backlinkHref(link)} onclick={goTo(backlinkHref(link))}>
                {link.title ?? link.from}
              </a>
              <span class="type-pill">{link.kind}</span>
            </li>
          {/each}
        </ul>
      </div>
    {/if}

    {#if issue.log && issue.log.length > 0}
      <div class="detail-section">
        <h3>Log</h3>
        <ul class="log-list">
          {#each issue.log as entry, index (index)}
            <li>
              <time datetime={entry.at}>{new Date(entry.at).toLocaleString()}</time>
              <span>{entry.actor} · {entry.event}{entry.raw ? ` · ${entry.raw}` : ''}</span>
            </li>
          {/each}
        </ul>
      </div>
    {/if}

    <div class="detail-section">
      <h3>Add a note</h3>
      <form onsubmit={submitNote}>
        <label>
          Note
          <textarea bind:value={noteText}></textarea>
        </label>
        <div class="actions">
          <button type="submit" disabled={saving}>Add note</button>
        </div>
      </form>
    </div>

    <div class="actions">
      {#if isTerminal(issue.status)}
        <button type="button" class="secondary" disabled={saving} onclick={reopenIssue}>Reopen</button>
      {:else}
        <button type="button" class="danger" disabled={saving} onclick={closeIssue}>Close issue</button>
      {/if}
    </div>
  </section>
{/if}
