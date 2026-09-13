<script lang="ts">
  import { ALL_PROJECTS, ApiError, api, type SearchResult } from '../../lib/api'
  import EmptyState from '../../lib/components/EmptyState.svelte'
  import ErrorState from '../../lib/components/ErrorState.svelte'
  import LoadingState from '../../lib/components/LoadingState.svelte'

  interface Props {
    project: string
    navigate: (path: string) => void
    reloadKey: number
    /** Pre-fills and runs a search, e.g. from a `/search?q=...` link (a memory backlink). */
    initialQuery?: string
  }

  let { project, navigate, reloadKey, initialQuery = '' }: Props = $props()

  let query = $state('')
  let kind = $state('')
  let includeArchivedHandoffs = $state(false)
  let results = $state<SearchResult[]>([])
  let loading = $state(false)
  let error = $state('')
  let searched = $state(false)

  // Not reactive state on purpose: this only needs to remember, across
  // renders, the last `initialQuery` we already applied, so that typing in
  // the query field (which also assigns to `query`) never gets clobbered by
  // this effect re-running.
  let appliedInitialQuery = ''

  $effect(() => {
    const q = initialQuery
    if (q === '' || q === appliedInitialQuery) {
      return
    }
    appliedInitialQuery = q
    query = q
    let cancelled = false
    runSearch(undefined, () => cancelled)
    return () => {
      cancelled = true
    }
  })

  $effect(() => {
    reloadKey
    if (searched && query.trim() !== '') {
      let cancelled = false
      runSearch(undefined, () => cancelled)
      return () => {
        cancelled = true
      }
    }
  })

  async function runSearch(event?: SubmitEvent, isCancelled: () => boolean = () => false) {
    event?.preventDefault()
    const q = query.trim()
    if (q === '') {
      results = []
      searched = false
      return
    }
    loading = true
    error = ''
    searched = true
    try {
      const result = await api.search({
        q,
        kind: kind || undefined,
        project: project === ALL_PROJECTS ? undefined : project,
        include_archived_handoffs: includeArchivedHandoffs,
      })
      if (isCancelled()) return
      results = result
    } catch (err) {
      if (isCancelled()) return
      error = errorMessage(err)
    } finally {
      if (!isCancelled()) loading = false
    }
  }

  function resultPath(result: SearchResult): string {
    if (result.kind === 'issue') {
      return `/issues/${encodeURIComponent(result.id)}`
    }
		if (result.kind === 'request') { return `/requests/${encodeURIComponent(result.id)}` }
    if (result.kind === 'memory') {
      return `/search?q=${encodeURIComponent(result.basename)}`
    }
    return `/wiki/${result.path.replace(/\.md$/i, '')}`
  }

  function errorMessage(err: unknown): string {
    if (err instanceof ApiError) {
      return err.message
    }
    return err instanceof Error ? err.message : 'Request failed.'
  }
</script>

<section class="workspace search-workspace" aria-label="Search workspace">
  <form class="toolbar" onsubmit={runSearch}>
    <label>
      Query
      <input type="text" bind:value={query} placeholder="Search issues and docs" />
    </label>
    <label>
      Kind
      <select bind:value={kind}>
        <option value="">All</option>
        <option value="issue">Issues</option>
			<option value="request">Requests</option>
        <option value="doc">Docs</option>
        <option value="memory">Memories</option>
        <option value="handoff">Handoffs</option>
      </select>
    </label>
    <label><input type="checkbox" bind:checked={includeArchivedHandoffs} /> Include archived handoffs</label>
    <button type="submit" disabled={loading}>{loading ? 'Searching…' : 'Search'}</button>
  </form>

  {#if loading && results.length === 0}
    <LoadingState message="Searching" />
  {:else if error !== ''}
    <ErrorState title="Search failed" message={error} />
  {:else if !searched}
    <EmptyState title="Search the hub" message="Find issues and docs by title, id, or body text." />
  {:else if results.length === 0}
    <EmptyState title="No results" message="Try a different query or clear the kind filter." />
  {:else}
    <ul class="search-results" aria-label="Search results">
      {#each results as result, index (index)}
        <li>
          <button
            type="button"
            class="search-result"
            onclick={() => navigate(resultPath(result))}
          >
            <span class="type-pill">{result.kind}</span>
            <span class="search-result-copy">
              <strong>{result.title || result.basename}</strong>
              <small>{result.id || result.path}{result.project ? ` · ${result.project}` : ''}</small>
            </span>
            <span class="muted">{result.score.toFixed(2)}</span>
          </button>
        </li>
      {/each}
    </ul>
  {/if}
</section>
