export type IssueState = 'open' | 'in_progress' | 'blocked' | 'closed' | 'done'

export type IssueType = 'bug' | 'feature' | 'task' | 'epic' | 'chore'

export type ApiErrorCode =
  | 'bad_request'
  | 'validation_error'
  | 'not_found'
  | 'conflict'
  | 'store_configuration_error'
  | 'internal_error'
  | 'request_error'

export interface FieldError {
  field: string
  message: string
}

export interface ErrorEnvelope {
  error: ApiErrorCode
  message: string
  fields?: FieldError[]
}

export interface RepoTarget {
  id?: string
  slug: string
  remote_url?: string
  default_branch?: string
  requested_ref?: string
  base_ref?: string
  work_branch?: string
  worktree_subdir?: string
  clone_strategy?: string
  auth_ref?: string
  metadata?: Record<string, unknown>
}

export interface Issue {
  id: string
  identifier: string
  title: string
  description: string
  priority: number
  issue_type: IssueType
  state: IssueState
  labels: string[]
  blocked_by: string[]
  branch_name?: string
  url?: string
  repo?: RepoTarget
  created_at: string
  updated_at: string
}

export interface IssueListResponse {
  issues: Issue[]
}

export interface IssueRepoInput {
  repo_slug: string
  requested_ref?: string
  base_ref?: string
  work_branch?: string
  worktree_subdir?: string
  metadata?: Record<string, unknown>
}

export interface CreateIssueRequest {
  title: string
  description?: string
  priority: number
  issue_type: IssueType
  labels?: string[]
  branch_name?: string
  url?: string
  repo?: IssueRepoInput
}

export interface UpdateIssueRequest {
  title?: string
  description?: string
  priority?: number
  state?: IssueState
  labels?: string[]
  branch_name?: string
  url?: string
  repo?: IssueRepoInput
}

export interface CloseIssueRequest {
  reason?: string
}

export interface ListIssuesParams {
  state?: IssueState | IssueState[]
  limit?: number
}

export interface AddDependencyRequest {
  blocked_by_id: string
}

export interface Dependency {
  issue_id: string
  blocked_by_id: string
}

export interface DependencyListResponse {
  dependencies: Dependency[]
}

export interface GraphResponse {
  nodes: GraphNode[]
  edges: GraphEdge[]
}

export interface GraphNode {
  id: string
  title: string
  state: IssueState
  priority: number
  labels: string[]
}

export interface GraphEdge {
  source: string
  target: string
}

export interface HealthResponse {
  status: 'ok'
}
