<script lang="ts">
  import { ALL_PROJECTS, ApiError, api, type GraphEdge, type GraphNode } from '../../lib/api'
  import { graphEdgePath, layoutDependencyGraph } from '../../lib/graph'
  import EmptyState from '../../lib/components/EmptyState.svelte'
  import ErrorState from '../../lib/components/ErrorState.svelte'
  import LoadingState from '../../lib/components/LoadingState.svelte'

  interface Props {
    project: string
    reloadKey: number
  }

  let { project, reloadKey }: Props = $props()

  let nodes = $state<GraphNode[]>([])
  let edges = $state<GraphEdge[]>([])
  let loading = $state(false)
  let error = $state('')
  let selectedID = $state('')
  let refreshedAt = $state<Date | null>(null)

  const allProjects = $derived(project === ALL_PROJECTS)
  const layout = $derived(layoutDependencyGraph(nodes, edges))
  const selectedNode = $derived(layout.nodes.find((node) => node.id === selectedID) ?? layout.nodes[0])
  const selectedEdges = $derived(
    selectedNode ? layout.edges.filter((edge) => edge.source === selectedNode.id || edge.target === selectedNode.id) : [],
  )
  const projectColors = $derived.by(() => {
    const colors = new Map<string, string>()
    const seen = new Set(nodes.map((node) => node.project).filter((p): p is string => Boolean(p)))
    let i = 0
    for (const p of Array.from(seen).sort()) {
      colors.set(p, `hsl(${(i * 67) % 360} 55% 45%)`)
      i += 1
    }
    return colors
  })

  $effect(() => {
    project
    reloadKey
    let cancelled = false
    loadGraph(() => cancelled)
    return () => {
      cancelled = true
    }
  })

  async function loadGraph(isCancelled: () => boolean = () => false) {
    loading = true
    error = ''
    try {
      const response = allProjects ? await api.graph({ all: true }) : await api.graph({ project })
      if (isCancelled()) return
      nodes = response.nodes.map((node) => ({
        id: node.id,
        title: node.title,
        state: node.status,
        priority: node.priority,
        labels: [],
        status: node.status,
        type: node.type,
        project: node.project,
        archived: node.archived,
      }))
      edges = response.edges.map((edge) => ({ source: edge.from, target: edge.to, kind: edge.kind }))
      refreshedAt = new Date()
      if (selectedID !== '' && !nodes.some((node) => node.id === selectedID)) {
        selectedID = ''
      }
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

  function nodeTitle(value: string): string {
    return value.length > 20 ? `${value.slice(0, 19)}...` : value
  }

  function nodeFill(node: GraphNode): string | undefined {
    if (!allProjects || !node.project) {
      return undefined
    }
    return projectColors.get(node.project)
  }
</script>

<section class="workspace graph-workspace" aria-label="Dependency graph workspace">
  <div class="toolbar graph-toolbar">
    <div class="queue-summary">
      <strong>{layout.nodes.length}</strong>
      <span>{layout.nodes.length === 1 ? 'issue' : 'issues'}</span>
      <strong>{layout.edges.length}</strong>
      <span>{layout.edges.length === 1 ? 'dependency' : 'dependencies'}</span>
      {#if refreshedAt}
        <small>Refreshed {refreshedAt.toLocaleTimeString()}</small>
      {/if}
    </div>
    <button type="button" class="secondary" disabled={loading} onclick={() => loadGraph()}>
      {loading ? 'Refreshing' : 'Refresh'}
    </button>
  </div>

  {#if loading && layout.nodes.length === 0}
    <LoadingState message="Loading dependency graph" />
  {:else if error !== '' && layout.nodes.length === 0}
    <ErrorState title="Could not load graph" message={error} />
  {:else if layout.nodes.length === 0}
    <EmptyState title="No graph data" message="Create issues and dependencies to build the graph." />
  {:else}
    <div class="graph-content">
      <div class="graph-canvas" aria-label="Dependency network">
        {#if error !== ''}
          <p class="form-error" role="alert">{error}</p>
        {/if}
        {#if allProjects}
          <ul class="graph-legend" aria-label="Project colors">
            {#each Array.from(projectColors.entries()) as [name, color] (name)}
              <li><span class="graph-legend-swatch" style={`background:${color}`}></span>{name}</li>
            {/each}
          </ul>
        {/if}
        <svg
          viewBox={`0 0 ${layout.width} ${layout.height}`}
          aria-label="Dependency graph"
          style={`width: ${layout.width}px; height: ${layout.height}px;`}
        >
          <defs>
            <marker id="graph-arrow" viewBox="0 0 10 10" refX="8" refY="5" markerWidth="6" markerHeight="6" orient="auto">
              <path d="M 0 0 L 10 5 L 0 10 z"></path>
            </marker>
          </defs>
          {#each layout.edges as edge, index (index)}
            <path class="graph-edge" d={graphEdgePath(edge)} marker-end="url(#graph-arrow)">
              <title>{edge.source} {edge.kind ?? 'blocks'} {edge.target}</title>
            </path>
          {/each}
          {#each layout.nodes as node (node.id)}
            <g
              class:selected={selectedNode?.id === node.id}
              class="graph-node"
              transform={`translate(${node.x} ${node.y})`}
              role="button"
              tabindex="0"
              aria-label={`${node.title}, ${node.state}, priority ${node.priority}`}
              onclick={() => (selectedID = node.id)}
              onkeydown={(event) => {
                if (event.key === 'Enter' || event.key === ' ') {
                  event.preventDefault()
                  selectedID = node.id
                }
              }}
            >
              <title>{node.title} ({node.id})</title>
              <rect x="-70" y="-30" width="140" height="60" rx="8" style={nodeFill(node) ? `fill:${nodeFill(node)}` : undefined}
              ></rect>
              <text y="-7" text-anchor="middle" class:on-color={Boolean(nodeFill(node))}>{nodeTitle(node.title)}</text>
              <text y="14" text-anchor="middle" class:on-color={Boolean(nodeFill(node))}>{node.state} · P{node.priority}</text>
            </g>
          {/each}
        </svg>
      </div>

      <aside class="graph-inspector" aria-label="Selected issue">
        {#if selectedNode}
          <div>
            <h2>{selectedNode.title}</h2>
            <p>{selectedNode.id}{selectedNode.project ? ` · ${selectedNode.project}` : ''}</p>
          </div>
          <div class="graph-pills">
            <span>{selectedNode.state}</span>
            <span>P{selectedNode.priority}</span>
            <span>{selectedNode.incoming} blockers</span>
            <span>{selectedNode.outgoing} blocked</span>
          </div>
          <div class="edge-list">
            <h3>Relationships</h3>
            {#if selectedEdges.length === 0}
              <p class="muted">No dependencies yet.</p>
            {:else}
              <ul>
                {#each selectedEdges as edge, index (index)}
                  <li>
                    <span>{edge.sourceNode.title}</span>
                    <small>{edge.kind ?? 'blocks'}</small>
                    <span>{edge.targetNode.title}</span>
                  </li>
                {/each}
              </ul>
            {/if}
          </div>
        {/if}
      </aside>
    </div>
  {/if}
</section>

<style>
  .graph-toolbar {
    justify-content: space-between;
  }

  .graph-content {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 300px;
    min-height: 520px;
  }

  .graph-canvas {
    min-width: 0;
    border-right: 1px solid var(--border);
    overflow: auto;
    padding: 16px;
  }

  .graph-legend {
    display: flex;
    flex-wrap: wrap;
    gap: 10px;
    margin: 0 0 10px;
    padding: 0;
    list-style: none;
    color: var(--muted);
    font-size: 12px;
  }

  .graph-legend li {
    display: flex;
    align-items: center;
    gap: 4px;
  }

  .graph-legend-swatch {
    display: inline-block;
    width: 10px;
    height: 10px;
    border-radius: 2px;
  }

  svg {
    display: block;
    min-width: max(720px, 100%);
    min-height: 440px;
  }

  marker path {
    fill: var(--muted);
  }

  .graph-edge {
    fill: none;
    stroke: var(--border-strong);
    stroke-width: 2;
  }

  .graph-node {
    cursor: pointer;
    outline: none;
  }

  .graph-node rect {
    fill: var(--surface);
    stroke: var(--border-strong);
    stroke-width: 1.5;
  }

  .graph-node:hover rect,
  .graph-node:focus-visible rect,
  .graph-node.selected rect {
    stroke: var(--accent);
    stroke-width: 2;
  }

  .graph-node text:first-of-type {
    fill: var(--fg);
    font-size: 14px;
    font-weight: 700;
  }

  .graph-node text:last-of-type {
    fill: var(--muted);
    font-size: 12px;
  }

  .graph-node text.on-color {
    fill: #ffffff;
  }

  .graph-inspector {
    display: grid;
    align-content: start;
    gap: 14px;
    padding: 18px;
  }

  .graph-inspector h2 {
    font-size: 20px;
  }

  .graph-inspector p,
  .edge-list small {
    color: var(--muted);
  }

  .graph-pills {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }

  .graph-pills span {
    border-radius: 999px;
    padding: 4px 10px;
    color: var(--accent);
    background: var(--accent-soft);
    font-size: 13px;
  }

  .edge-list {
    display: grid;
    gap: 10px;
  }

  .edge-list h3 {
    font-size: 16px;
  }

  .edge-list ul {
    display: grid;
    gap: 8px;
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .edge-list li {
    display: grid;
    gap: 2px;
    border: 1px solid var(--border);
    border-radius: 6px;
    padding: 8px 10px;
  }

  @media (max-width: 900px) {
    .graph-content {
      grid-template-columns: 1fr;
    }

    .graph-canvas {
      border-right: 0;
      border-bottom: 1px solid var(--border);
    }
  }
</style>
