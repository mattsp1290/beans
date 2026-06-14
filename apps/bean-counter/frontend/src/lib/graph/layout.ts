import type { GraphEdge, GraphNode } from '../api'

export interface PositionedGraphNode extends GraphNode {
  x: number
  y: number
  level: number
  incoming: number
  outgoing: number
}

export interface PositionedGraphEdge extends GraphEdge {
  sourceNode: PositionedGraphNode
  targetNode: PositionedGraphNode
}

export interface GraphLayout {
  nodes: PositionedGraphNode[]
  edges: PositionedGraphEdge[]
  width: number
  height: number
}

const COLUMN_GAP = 220
const ROW_GAP = 104
const PADDING_X = 96
const PADDING_Y = 64

export function layoutDependencyGraph(nodes: GraphNode[], edges: GraphEdge[]): GraphLayout {
  const sortedNodes = [...nodes].sort(compareGraphNodes)
  const nodeByID = new Map(sortedNodes.map((node) => [node.id, node]))
  const validEdges = edges
    .filter((edge) => nodeByID.has(edge.source) && nodeByID.has(edge.target))
    .sort((left, right) => left.source.localeCompare(right.source) || left.target.localeCompare(right.target))
  const levels = assignLevels(sortedNodes, validEdges)
  const nodesByLevel = groupNodesByLevel(sortedNodes, levels)
  const maxRows = Math.max(1, ...Array.from(nodesByLevel.values(), (items) => items.length))
  const maxLevel = Math.max(0, ...Array.from(nodesByLevel.keys()))
  const width = PADDING_X * 2 + maxLevel * COLUMN_GAP
  const height = PADDING_Y * 2 + Math.max(0, maxRows - 1) * ROW_GAP
  const incoming = countEdges(validEdges, 'target')
  const outgoing = countEdges(validEdges, 'source')
  const positionedByID = new Map<string, PositionedGraphNode>()

  for (const [level, levelNodes] of nodesByLevel) {
    const groupHeight = Math.max(0, levelNodes.length - 1) * ROW_GAP
    const startY = (height - groupHeight) / 2
    levelNodes.forEach((node, index) => {
      positionedByID.set(node.id, {
        ...node,
        level,
        x: PADDING_X + level * COLUMN_GAP,
        y: startY + index * ROW_GAP,
        incoming: incoming.get(node.id) ?? 0,
        outgoing: outgoing.get(node.id) ?? 0,
      })
    })
  }

  return {
    nodes: Array.from(positionedByID.values()).sort((left, right) => left.level - right.level || compareGraphNodes(left, right)),
    edges: validEdges.map((edge) => ({
      ...edge,
      sourceNode: positionedByID.get(edge.source)!,
      targetNode: positionedByID.get(edge.target)!,
    })),
    width,
    height,
  }
}

export function graphEdgePath(edge: PositionedGraphEdge): string {
  const dx = Math.max(64, Math.abs(edge.targetNode.x - edge.sourceNode.x) / 2)
  return [
    `M ${edge.sourceNode.x + 58} ${edge.sourceNode.y}`,
    `C ${edge.sourceNode.x + dx} ${edge.sourceNode.y}`,
    `${edge.targetNode.x - dx} ${edge.targetNode.y}`,
    `${edge.targetNode.x - 58} ${edge.targetNode.y}`,
  ].join(' ')
}

function assignLevels(nodes: GraphNode[], edges: GraphEdge[]): Map<string, number> {
  const level = new Map(nodes.map((node) => [node.id, 0]))
  for (let pass = 0; pass < nodes.length; pass += 1) {
    let changed = false
    for (const edge of edges) {
      const next = (level.get(edge.source) ?? 0) + 1
      if (next > (level.get(edge.target) ?? 0)) {
        level.set(edge.target, next)
        changed = true
      }
    }
    if (!changed) {
      break
    }
  }
  return level
}

function groupNodesByLevel(nodes: GraphNode[], levels: Map<string, number>): Map<number, GraphNode[]> {
  const grouped = new Map<number, GraphNode[]>()
  for (const node of nodes) {
    const level = levels.get(node.id) ?? 0
    grouped.set(level, [...(grouped.get(level) ?? []), node])
  }
  return new Map(
    Array.from(grouped.entries())
      .sort(([left], [right]) => left - right)
      .map(([level, items]) => [level, items.sort(compareGraphNodes)]),
  )
}

function countEdges(edges: GraphEdge[], field: 'source' | 'target'): Map<string, number> {
  const counts = new Map<string, number>()
  for (const edge of edges) {
    counts.set(edge[field], (counts.get(edge[field]) ?? 0) + 1)
  }
  return counts
}

function compareGraphNodes(left: GraphNode, right: GraphNode): number {
  return left.priority - right.priority || left.title.localeCompare(right.title) || left.id.localeCompare(right.id)
}
