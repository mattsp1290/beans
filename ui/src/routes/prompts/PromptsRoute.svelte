<script lang="ts">
  import { api, PROMPT_STATUSES, type PromptDetail, type PromptStatus, type PromptSummary } from '../../lib/api'
  import EmptyState from '../../lib/components/EmptyState.svelte'
  import ErrorState from '../../lib/components/ErrorState.svelte'
  import LoadingState from '../../lib/components/LoadingState.svelte'
  import { backlinkHref, handleNavigationClick, handleRenderedContentClick } from '../../lib/nav'

  interface Props {
    project: string
    id: string | null
    reloadKey: number
    navigate: (path: string) => void
  }

  let { project, id, reloadKey, navigate }: Props = $props()
  let prompts = $state<PromptSummary[]>([])
  let detail = $state<PromptDetail | null>(null)
  let loading = $state(true)
  let error = $state('')
  let query = $state('')
  let status = $state<PromptStatus | ''>('')
  let recipient = $state('')
  let includeTerminal = $state(false)

  const statusLabels: Record<PromptStatus, string> = {
    open: 'Open',
    in_progress: 'In progress',
    completed: 'Completed',
    declined: 'Declined',
    cancelled: 'Cancelled',
  }
  const statuses = PROMPT_STATUSES.map((value) => ({ value, label: statusLabels[value] }))

  $effect(() => {
    const requestedProject = project
    const requestedId = id
    reloadKey
    const requestedQuery = query
    const requestedStatus = status
    const requestedRecipient = recipient
    const requestedTerminal = includeTerminal
    let cancelled = false
    let timer: ReturnType<typeof setTimeout> | undefined
    let controller: AbortController | undefined
    loading = true
    error = ''
    detail = null
    prompts = []
    const settle = (request: Promise<PromptDetail | PromptSummary[]>) => request
        .then((value) => {
          if (cancelled) return
          if (requestedId) detail = value as PromptDetail
          else prompts = value as PromptSummary[]
        })
        .catch((cause) => {
          if (!cancelled && !(cause instanceof DOMException && cause.name === 'AbortError')) {
            error = cause instanceof Error ? cause.message : 'Could not load prompts'
          }
        })
        .finally(() => {
          if (!cancelled) loading = false
        })
    if (requestedId) {
      void settle(api.getPrompt(requestedId))
    } else {
      controller = new AbortController()
      timer = setTimeout(() => {
        void settle(api.listPrompts(requestedProject, {
          q: requestedQuery || undefined,
          status: requestedStatus || undefined,
          to: requestedRecipient || undefined,
          terminal: requestedTerminal,
        }, controller?.signal))
      }, 150)
    }
    return () => {
      cancelled = true
      if (timer !== undefined) clearTimeout(timer)
      controller?.abort()
    }
  })

  function goTo(path: string) {
    return (event: MouseEvent) => {
      handleNavigationClick(event, navigate, path)
    }
  }

  function formatTime(value: string): string {
    const instant = new Date(value)
    return Number.isNaN(instant.getTime()) ? value : instant.toLocaleString()
  }
</script>

{#if loading}
  <LoadingState label="Loading prompts…" message="Fetching prompts from the hub." />
{:else if error}
  <ErrorState title="Could not load prompts" message={error} />
{:else if id && !detail}
  <EmptyState title="Prompt not found" message="This prompt may have been moved or removed." />
{:else if id && detail}
  <article class="workspace prompt-detail" aria-label="Prompt detail">
    <button type="button" class="secondary back" onclick={() => navigate('/prompts')}>&larr; Back to prompts</button>
    <header>
      <p class="muted">{detail.id} · {detail.project}</p>
      <h2>{detail.title}</h2>
      <div class="pills" aria-label="Prompt status and priority">
        <span class="status-pill">{detail.status}</span>
        <span class="priority-pill">P{detail.priority}</span>
      </div>
    </header>

    <dl class="metadata">
      <div><dt>From</dt><dd>{detail.from}</dd></div>
      <div><dt>To</dt><dd>{detail.to}</dd></div>
      <div><dt>Claimant</dt><dd>{detail.claimed_by ?? 'Unclaimed'}</dd></div>
      <div><dt>Created</dt><dd><time datetime={detail.created}>{formatTime(detail.created)}</time></dd></div>
      <div><dt>Updated</dt><dd><time datetime={detail.updated}>{formatTime(detail.updated)}</time></dd></div>
      {#if detail.labels.length}<div><dt>Labels</dt><dd>{detail.labels.join(', ')}</dd></div>{/if}
    </dl>

    <section class="detail-section">
      <h3>Instructions</h3>
      <!-- Server rendering sanitizes Markdown; clicks on known internal routes stay in the SPA. -->
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div class="rendered" onclick={(event) => handleRenderedContentClick(event, navigate)}>{@html detail.html}</div>
    </section>

    <section class="detail-section">
      <h3>Linked issue</h3>
      {#if !detail.issue}
        <p class="muted">No issue is linked to this prompt.</p>
      {:else if detail.issue.missing}
        <p><span class="status-pill missing">Missing</span> {detail.issue.id}</p>
      {:else}
        <p><a href={`/issues/${encodeURIComponent(detail.issue.id)}`} onclick={goTo(`/issues/${encodeURIComponent(detail.issue.id)}`)}>{detail.issue.title || detail.issue.id}</a> <span class="muted">{detail.issue.id}</span></p>
      {/if}
    </section>

    {#if detail.backlinks.length}
      <section class="detail-section">
        <h3>Backlinks</h3>
        <ul class="plain-list">
          {#each detail.backlinks as link, index (index)}
            <li><a href={backlinkHref(link)} onclick={goTo(backlinkHref(link))}>{link.title ?? link.from}</a><span class="type-pill">{link.kind}</span></li>
          {/each}
        </ul>
      </section>
    {/if}

    {#if detail.log.length}
      <section class="detail-section">
        <h3>Log</h3>
        <ul class="log-list">
          {#each detail.log as entry, index (index)}
            <li><time datetime={entry.at}>{formatTime(entry.at)}</time><span>{entry.actor} · {entry.event}{entry.raw ? ` · ${entry.raw}` : ''}</span></li>
          {/each}
        </ul>
      </section>
    {/if}

    {#if detail.toc.length}
      <nav class="toc" aria-label="Prompt table of contents">
        {#each detail.toc as entry}<a href={`#${entry.id}`}>{entry.text}</a>{/each}
      </nav>
    {/if}
  </article>
{:else}
  <section class="workspace" aria-label="Prompts">
    <div class="toolbar">
      <label>Search <input bind:value={query} type="search" placeholder="title, id, label, sender, recipient, or body" /></label>
      <label>Status
        <select bind:value={status}>
          <option value="">Active prompts</option>
          {#each statuses as option}<option value={option.value}>{option.label}</option>{/each}
        </select>
      </label>
      <label>Recipient <input bind:value={recipient} placeholder="All recipients" /></label>
      <label class="checkbox-field"><input type="checkbox" bind:checked={includeTerminal} /> Include terminal</label>
    </div>
    <div class="prompt-cards">
      {#each prompts as prompt (prompt.id)}
        <a class="prompt-card" href={`/prompts/${encodeURIComponent(prompt.id)}`} onclick={goTo(`/prompts/${encodeURIComponent(prompt.id)}`)}>
          <span class="card-heading"><strong>{prompt.title}</strong><span class="priority-pill">P{prompt.priority}</span></span>
          <span class="muted">{prompt.id} · {prompt.status} · {prompt.project}</span>
          <span>{prompt.from} → {prompt.to}{prompt.claimed_by ? ` · claimed by ${prompt.claimed_by}` : ''}</span>
          {#if prompt.labels.length}<span class="labels">{prompt.labels.join(', ')}</span>{/if}
          <span class="card-footer"><span>{prompt.has_issue ? 'Linked issue' : 'No linked issue'}</span><time datetime={prompt.updated}>Updated {formatTime(prompt.updated)}</time></span>
        </a>
      {:else}
        <p class="muted empty">No prompts found.</p>
      {/each}
    </div>
  </section>
{/if}

<style>
  .prompt-cards { display:grid; gap:10px; padding:14px }
  .prompt-card { display:grid; gap:6px; border:1px solid var(--border); border-radius:8px; padding:12px; color:var(--fg); text-decoration:none }
  .prompt-card:hover { border-color:var(--accent); background:var(--surface-2) }
  .card-heading,.card-footer,.pills { display:flex; align-items:center; justify-content:space-between; flex-wrap:wrap; gap:8px }
  .card-footer { color:var(--muted); font-size:13px }
  .labels { color:var(--accent); font-size:13px }
  .empty { padding:18px }
  .prompt-detail { display:grid; gap:18px; padding:18px }
  .prompt-detail header { display:grid; gap:8px }
  .back { justify-self:start }
  .metadata { grid-template-columns:repeat(2,minmax(0,1fr)) }
  .toc { border-top:1px solid var(--surface-2); padding-top:14px }
  @media (max-width: 700px) {
    .metadata { grid-template-columns:1fr }
    .log-list li { grid-template-columns:1fr }
  }
</style>
