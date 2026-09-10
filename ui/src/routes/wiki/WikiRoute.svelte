<script lang="ts">
  import { ALL_PROJECTS, ApiError, api, type DocPage, type DocTreeEntry } from '../../lib/api'
  import EmptyState from '../../lib/components/EmptyState.svelte'
  import ErrorState from '../../lib/components/ErrorState.svelte'
  import LoadingState from '../../lib/components/LoadingState.svelte'
  import { backlinkHref, handleRenderedContentClick } from '../../lib/nav'

  interface Props {
    project: string
    path: string
    navigate: (path: string) => void
    reloadKey: number
  }

  let { project, path, navigate, reloadKey }: Props = $props()

  interface TreeRow {
    type: 'folder' | 'file'
    depth: number
    label: string
    entry?: DocTreeEntry
  }

  let docs = $state<DocTreeEntry[]>([])
  let treeLoading = $state(false)
  let treeError = $state('')

  let doc = $state<DocPage | null>(null)
  let docLoading = $state(false)
  let docError = $state('')

  const rows = $derived.by(() => buildRows(docs))

  $effect(() => {
    project
    reloadKey
    let cancelled = false
    loadTree(() => cancelled)
    return () => {
      cancelled = true
    }
  })

  $effect(() => {
    path
    reloadKey
    if (path === '') {
      doc = null
      docError = ''
      return
    }
    let cancelled = false
    loadDoc(() => cancelled)
    return () => {
      cancelled = true
    }
  })

  async function loadTree(isCancelled: () => boolean = () => false) {
    treeLoading = true
    treeError = ''
    try {
      const response = await api.docsTree(project === ALL_PROJECTS ? undefined : project)
      if (isCancelled()) return
      docs = response.docs
    } catch (err) {
      if (isCancelled()) return
      treeError = errorMessage(err)
    } finally {
      if (!isCancelled()) treeLoading = false
    }
  }

  async function loadDoc(isCancelled: () => boolean = () => false) {
    docLoading = true
    docError = ''
    try {
      const result = await api.getDoc(path)
      if (isCancelled()) return
      doc = result
    } catch (err) {
      if (isCancelled()) return
      docError = errorMessage(err)
    } finally {
      if (!isCancelled()) docLoading = false
    }
  }

  function buildRows(entries: DocTreeEntry[]): TreeRow[] {
    const sorted = [...entries].sort((a, b) => a.path.localeCompare(b.path))
    const seenFolders = new Set<string>()
    const result: TreeRow[] = []
    for (const entry of sorted) {
      const segments = entry.path.split('/')
      let prefix = ''
      for (let i = 0; i < segments.length - 1; i += 1) {
        prefix = prefix === '' ? segments[i] : `${prefix}/${segments[i]}`
        if (!seenFolders.has(prefix)) {
          seenFolders.add(prefix)
          result.push({ type: 'folder', depth: i, label: segments[i] })
        }
      }
      result.push({ type: 'file', depth: segments.length - 1, label: entry.title || segments[segments.length - 1], entry })
    }
    return result
  }

  function errorMessage(err: unknown): string {
    if (err instanceof ApiError) {
      return err.message
    }
    return err instanceof Error ? err.message : 'Request failed.'
  }
</script>

<section class="workspace wiki-layout" aria-label="Wiki workspace">
  <nav class="wiki-tree" aria-label="Documentation tree">
    {#if treeLoading && docs.length === 0}
      <LoadingState message="Loading docs" />
    {:else if treeError !== '' && docs.length === 0}
      <ErrorState title="Could not load docs" message={treeError} />
    {:else if docs.length === 0}
      <EmptyState title="No docs" message="No documentation found for this project." />
    {:else}
      <ul>
        {#each rows as row, index (index)}
          {#if row.type === 'folder'}
            <li class="tree-folder" style={`padding-left: ${row.depth * 14}px`}>{row.label}</li>
          {:else if row.entry}
            <li style={`padding-left: ${row.depth * 14}px`}>
              <a
                href={`/wiki/${row.entry.path}`}
                class:active={row.entry.path === path}
                onclick={(event) => {
                  event.preventDefault()
                  navigate(`/wiki/${row.entry!.path}`)
                }}
              >
                {row.label}
              </a>
            </li>
          {/if}
        {/each}
      </ul>
    {/if}
  </nav>

  <div>
    {#if path === ''}
      <EmptyState title="Wiki" message="Pick a page from the tree to start reading." />
    {:else if docLoading && !doc}
      <LoadingState message="Loading page" />
    {:else if docError !== '' && !doc}
      <ErrorState title="Could not load page" message={docError} />
    {:else if doc}
      <div class="wiki-page">
        <div class="wiki-page-body">
          <h2>{doc.title}</h2>
          <p class="muted">{doc.path} · {doc.project}</p>
          <!-- svelte-ignore a11y_click_events_have_key_events -->
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div class="rendered" onclick={(event) => handleRenderedContentClick(event, navigate)}>{@html doc.html}</div>

          {#if doc.backlinks.length > 0}
            <div class="detail-section">
              <h3>Backlinks</h3>
              <ul class="plain-list">
                {#each doc.backlinks as link, index (index)}
                  <li>
                    <a
                      href={backlinkHref(link)}
                      onclick={(event) => {
                        event.preventDefault()
                        navigate(backlinkHref(link))
                      }}
                    >
                      {link.title ?? link.from}
                    </a>
                    <span class="type-pill">{link.kind}</span>
                  </li>
                {/each}
              </ul>
            </div>
          {/if}
        </div>

        {#if doc.toc.length > 0}
          <nav class="wiki-toc" aria-label="Table of contents">
            {#each doc.toc as entry, index (index)}
              <a href={`#${entry.id}`} style={`padding-left: ${(entry.level - 1) * 10}px`}>{entry.text}</a>
            {/each}
          </nav>
        {/if}
      </div>
    {/if}
  </div>
</section>
