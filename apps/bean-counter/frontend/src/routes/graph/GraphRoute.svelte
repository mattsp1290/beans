<script lang="ts">
  import { onMount } from 'svelte'

  import { ApiError, api, type GraphResponse } from '../../lib/api'
  import { graphEdgePath, layoutDependencyGraph } from '../../lib/graph'
  import EmptyState from '../../lib/components/EmptyState.svelte'
  import ErrorState from '../../lib/components/ErrorState.svelte'
  import LoadingState from '../../lib/components/LoadingState.svelte'

  let graph = $state<GraphResponse>({ nodes: [], edges: [] })
  let loading = $state(false)
  let error = $state('')
  let selectedID = $state('')
  let refreshedAt = $state<Date | null>(null)

  const layout = $derived(layoutDependencyGraph(graph.nodes, graph.edges))
  const selectedNode = $derived(layout.nodes.find((node) => node.id === selectedID) ?? layout.nodes[0])

  onMount(() => {
    void loadGraph()
  })

  async function loadGraph() {
    loading = true
    error = ''
    try {
      graph = await api.graph()
      refreshedAt = new Date()
      if (selectedID !== '' && !graph.nodes.some((node) => node.id === selectedID)) {
        selectedID = ''
      }
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

  function nodeTitle(value: string): string {
    return value.length > 20 ? `${value.slice(0, 19)}...` : value
  }
</script>

<section class="workspace graph-workspace" aria-label="Dependency graph workspace">
  <div class="toolbar graph-toolbar">
    <div class="graph-summary">
      <strong>{graph.nodes.length}</strong>
      <span>{graph.nodes.length === 1 ? 'issue' : 'issues'}</span>
      <strong>{graph.edges.length}</strong>
      <span>{graph.edges.length === 1 ? 'dependency' : 'dependencies'}</span>
      {#if refreshedAt}
        <small>Refreshed {refreshedAt.toLocaleTimeString()}</small>
      {/if}
    </div>
    <button type="button" class="secondary" disabled={loading} onclick={loadGraph}>
      {loading ? 'Refreshing' : 'Refresh'}
    </button>
  </div>

  {#if loading && graph.nodes.length === 0}
    <LoadingState message="Loading dependency graph" />
  {:else if error !== '' && graph.nodes.length === 0}
    <ErrorState title="Could not load graph" message={error} />
  {:else if graph.nodes.length === 0}
    <EmptyState title="No graph data" message="Create issues and dependencies to build the graph." />
  {:else}
    <div class="graph-content">
      <div class="graph-canvas" aria-label="Dependency network">
        {#if error !== ''}
          <p class="form-error" role="alert">{error}</p>
        {/if}
        <svg viewBox={`0 0 ${layout.width} ${layout.height}`} role="img" aria-label="Dependency graph">
          <defs>
            <marker id="graph-arrow" viewBox="0 0 10 10" refX="8" refY="5" markerWidth="6" markerHeight="6" orient="auto">
              <path d="M 0 0 L 10 5 L 0 10 z"></path>
            </marker>
          </defs>
          {#each layout.edges as edge}
            <path class="graph-edge" d={graphEdgePath(edge)} marker-end="url(#graph-arrow)">
              <title>{edge.source} blocks {edge.target}</title>
            </path>
          {/each}
          {#each layout.nodes as node}
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
              <rect x="-70" y="-30" width="140" height="60" rx="8"></rect>
              <text y="-7" text-anchor="middle">{nodeTitle(node.title)}</text>
              <text y="14" text-anchor="middle">{node.state} · P{node.priority}</text>
            </g>
          {/each}
        </svg>
      </div>

      <aside class="graph-inspector" aria-label="Selected issue">
        {#if selectedNode}
          <div>
            <h2>{selectedNode.title}</h2>
            <p>{selectedNode.id}</p>
          </div>
          <div class="graph-pills">
            <span>{selectedNode.state}</span>
            <span>P{selectedNode.priority}</span>
            <span>{selectedNode.incoming} blockers</span>
            <span>{selectedNode.outgoing} blocked</span>
          </div>
          {#if selectedNode.labels.length > 0}
            <div class="label-row">
              {#each selectedNode.labels as label}
                <span>{label}</span>
              {/each}
            </div>
          {/if}
          <div class="edge-list">
            <h3>Relationships</h3>
            {#if layout.edges.length === 0}
              <p class="muted">No dependencies yet.</p>
            {:else}
              <ul>
                {#each layout.edges.filter((edge) => edge.source === selectedNode.id || edge.target === selectedNode.id) as edge}
                  <li>
                    <span>{edge.sourceNode.title}</span>
                    <small>blocks</small>
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

  .graph-summary {
    display: flex;
    align-items: baseline;
    flex-wrap: wrap;
    gap: 6px;
    color: #657166;
  }

  .graph-summary strong {
    color: #17211b;
    font-size: 24px;
  }

  .graph-summary small {
    flex-basis: 100%;
    color: #657166;
  }

  .graph-content {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 300px;
    min-height: 520px;
  }

  .graph-canvas {
    min-width: 0;
    border-right: 1px solid #d9ded4;
    overflow: auto;
    padding: 16px;
  }

  svg {
    display: block;
    min-width: 720px;
    width: 100%;
    min-height: 440px;
  }

  marker path {
    fill: #657166;
  }

  .graph-edge {
    fill: none;
    stroke: #9aa694;
    stroke-width: 2;
  }

  .graph-node {
    cursor: pointer;
    outline: none;
  }

  .graph-node rect {
    fill: #ffffff;
    stroke: #cbd3c7;
    stroke-width: 1.5;
  }

  .graph-node:hover rect,
  .graph-node:focus-visible rect,
  .graph-node.selected rect {
    fill: #f1f5ee;
    stroke: #245942;
    stroke-width: 2;
  }

  .graph-node text:first-of-type {
    fill: #17211b;
    font-size: 14px;
    font-weight: 700;
  }

  .graph-node text:last-of-type {
    fill: #657166;
    font-size: 12px;
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
    color: #657166;
  }

  .graph-pills {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }

  .graph-pills span {
    border-radius: 999px;
    padding: 4px 10px;
    color: #245942;
    background: #e4ebe1;
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
    border: 1px solid #d9ded4;
    border-radius: 6px;
    padding: 8px 10px;
  }

  @media (max-width: 900px) {
    .graph-content {
      grid-template-columns: 1fr;
    }

    .graph-canvas {
      border-right: 0;
      border-bottom: 1px solid #d9ded4;
    }
  }
</style>
