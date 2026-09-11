// Wire types for the bn serve JSON API (see
// .agents/plans/hub-vault-redesign/06-server-and-ui.md and cmd/bn/json.go
// `issueJSON` for the authoritative shapes this file mirrors).

export interface ApiErrorBody {
  code: string
  message: string
}

export interface ErrorEnvelope {
  error: ApiErrorBody
}

/** Common shape returned by every mutating endpoint. */
export interface MutationResult {
  commit: string
  pushed: boolean
  message: string
}

export interface CreateIssueResult {
  id: string
  path: string
  commit: string
  pushed: boolean
  message: string
}

export interface ProjectCounts {
  open: number
  in_progress: number
  closed: number
}

export interface ProjectSummary {
  name: string
  prefix: string
  counts: ProjectCounts
  workflow: WorkflowInfo
}

/** Sentinel project name meaning "every project", used with the project switcher. */
export const ALL_PROJECTS = '_all'

export interface IssueLogEntry {
  at: string
  actor: string
  repo?: string
  sha?: string
  branch?: string
  event: string
  raw?: string
}

/** What kind of note a backlink's `from` (and `path`, when set) refers to. */
export type NoteKind = 'issue' | 'doc' | 'memory' | 'plan' | 'request' | 'handoff'

export type RequestStatus = 'open' | 'accepted' | 'in_progress' | 'resolved' | 'declined'
export interface LinkedIssueSummary { id:string; title?:string; status?:string; priority?:number; project?:string; archived?:boolean; missing?:boolean }
export interface RequestSummary { id:string; title:string; status:RequestStatus; priority:number; labels:string[]; requested_by:string; created:string; updated:string; project:string; path:string; issue_count:number }
export interface RequestDetail extends RequestSummary { issues:LinkedIssueSummary[]; backlinks:Backlink[]; log:IssueLogEntry[]; toc:TocEntry[]; html:string }
export interface ListRequestsParams { status?:RequestStatus; label?:string; priority?:number; q?:string; terminal?:boolean }

export type PlanStatus = 'draft' | 'blocked' | 'ready' | 'complete'
export interface PlanNode { id: string; label: string; kind: string; ref?: string }
export interface PlanEdge { from: string; to: string; kind: string; label?: string }
export interface PlanGraph { version: number; nodes: PlanNode[]; edges: PlanEdge[] }
export interface PlanSummary { status: PlanStatus; outcome_html: string; affected_areas_html: string; execution_order_html: string; risks_html: string; graph: PlanGraph }
export interface PlanListItem { id:string; title:string; status:PlanStatus; project:string; path:string; created:string; updated:string; section_count:number }
export interface PlanDetail extends PlanListItem { summary: PlanSummary; sections: {path:string; html:string}[]; backlinks: Backlink[] }

/**
 * A reference to this note from elsewhere in the hub. `from` is the issue id
 * when `note_kind` is "issue", and the note basename otherwise. Shared by
 * issue detail responses and doc page responses; use `backlinkHref` (in
 * `src/lib/nav.ts`) to route one, rather than re-deriving the kind locally.
 */
export interface Backlink {
  from: string
  kind: string
  note_kind: NoteKind
  path?: string
  title?: string
}

/** @deprecated use {@link Backlink} */
export type IssueBacklink = Backlink

export interface IssueBlocker {
  id: string
  status?: string
  project?: string
  title?: string
  missing?: boolean
}

/** A child issue as summarized on its parent's detail response. */
export interface IssueChildSummary {
  id: string
  status: string
  project: string
  title: string
}

/** issueJSON, the stable --json shape of an issue (cmd/bn/json.go). */
export interface Issue {
  id: string
  title: string
  type: string
  status: string
  priority: number
  labels: string[]
  assignee: string
  parent: string
  blocked_by: string[]
  url: string
  created: string
  updated: string
  project: string
  path: string
  archived: boolean
  description?: string
  log?: IssueLogEntry[]
  children?: IssueChildSummary[]
  backlinks?: Backlink[]
	blockers?: IssueBlocker[]
	requests?: {id:string;title:string;status:RequestStatus;priority:number;project:string}[]
}

export interface WorkflowInfo {
  statuses: string[]
  active: string[]
  terminal: string[]
}

/** GET /api/issues/:id: issueJSON detail plus rendered HTML and workflow info. */
export interface IssueDetail extends Issue {
  html: string
  workflow: WorkflowInfo
}

export interface ListIssuesParams {
  status?: string
  type?: string
  label?: string
  archived?: boolean
  q?: string
}

export interface CreateIssueRequest {
  title: string
  description?: string
  priority?: number
  type?: string
  labels?: string[]
  parent?: string
  assignee?: string
  blocked_by?: string[]
  url?: string
}

export interface UpdateIssueRequest {
  status?: string
  title?: string
  description?: string
  priority?: number
  type?: string
  assignee?: string
  parent?: string
  add_labels?: string[]
  remove_labels?: string[]
  note?: string
  claim?: boolean
  force?: boolean
}

export type DependencyKind = 'blocks' | 'parent-child'

export interface AddDependencyRequest {
  target: string
  type: DependencyKind
}

// --- Graph --------------------------------------------------------------

/** GET /api/graph node shape, as returned by the server. */
export interface GraphApiNode {
  id: string
  title: string
  status: string
  priority: number
  type: string
  project: string
  archived: boolean
}

export type GraphEdgeKind = 'blocks' | 'parent'

/** GET /api/graph edge shape, as returned by the server. */
export interface GraphApiEdge {
  from: string
  to: string
  kind: GraphEdgeKind
}

export interface GraphResponse {
  nodes: GraphApiNode[]
  edges: GraphApiEdge[]
}

// src/lib/graph/layout.ts (and its test) are frozen by contract and import
// `GraphNode`/`GraphEdge` from this module using the field names below
// (`state` rather than `status`, `source`/`target` rather than `from`/`to`).
// GraphRoute.svelte adapts a GraphResponse into these before calling
// layoutDependencyGraph; the extra fields ride along for coloring/labeling.
export interface GraphNode {
  id: string
  title: string
  state: string
  priority: number
  labels: string[]
  status?: string
  type?: string
  project?: string
  archived?: boolean
}

export interface GraphEdge {
  source: string
  target: string
  kind?: GraphEdgeKind
}

// --- Docs / wiki ----------------------------------------------------------

export interface DocTreeEntry {
  path: string
  title: string
  project: string
}

export interface DocsTreeResponse {
  docs: DocTreeEntry[]
}

export interface TocEntry {
  level: number
  id: string
  text: string
}

export interface DocOutlink {
  to: string
  kind: string
}

/** @deprecated use {@link Backlink} */
export type DocBacklink = Backlink

export interface DocPage {
	kind?: NoteKind
  path: string
  title: string
  project: string
  frontmatter: Record<string, unknown>
  html: string
  toc: TocEntry[]
  backlinks: Backlink[]
  outlinks: DocOutlink[]
}

// --- Search -----------------------------------------------------------------

export interface SearchParams {
  q: string
  kind?: string
	project?: string
	include_archived_handoffs?: boolean
}

export interface SearchResult {
	kind: NoteKind
  id: string
  basename: string
  title: string
  project: string
  path: string
  score: number
}

// --- Misc ---------------------------------------------------------------

export interface HealthResponse {
  status: string
  hub: string
  project: string
  ahead: number
  behind: number
}
