<script lang="ts">
  import { Background, Controls, SvelteFlow, type Edge, type Node } from '@xyflow/svelte'
  import dagre from '@dagrejs/dagre'
  import type { PlanGraph } from '../api'

  interface Props { graph: PlanGraph }
  let { graph }: Props = $props()
  const layout = $derived.by(() => {
    const g = new dagre.graphlib.Graph()
    g.setGraph({ rankdir: 'LR', nodesep: 36, ranksep: 72 }); g.setDefaultEdgeLabel(() => ({}))
    for (const node of graph.nodes) g.setNode(node.id, { width: 180, height: 54 })
    for (const edge of graph.edges) if (graph.nodes.some((n) => n.id === edge.from) && graph.nodes.some((n) => n.id === edge.to)) g.setEdge(edge.from, edge.to)
    dagre.layout(g)
    const nodes: Node[] = graph.nodes.map((node) => { const p = g.node(node.id); return { id: node.id, position: { x: p?.x ?? 0, y: p?.y ?? 0 }, data: { label: node.label }, ariaLabel: `${node.kind}: ${node.label}${node.ref ? `, ${node.ref}` : ''}` } })
    const edges: Edge[] = graph.edges.filter((edge) => graph.nodes.some((n) => n.id === edge.from) && graph.nodes.some((n) => n.id === edge.to)).map((edge, i) => ({ id: `${edge.from}-${edge.to}-${i}`, source: edge.from, target: edge.to, label: edge.label || edge.kind }))
    return { nodes, edges }
  })
</script>

<div class="graph" aria-label="Plan change graph">
  <SvelteFlow nodes={layout.nodes} edges={layout.edges} fitView nodesDraggable={false} nodesConnectable={false} deleteKey={null} panOnDrag>
    <Background /><Controls />
  </SvelteFlow>
</div>

<style>.graph{height:360px;border:1px solid var(--border);border-radius:8px;overflow:hidden}</style>
