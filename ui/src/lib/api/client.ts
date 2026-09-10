import type {
  AddDependencyRequest,
  CreateIssueRequest,
  CreateIssueResult,
  DependencyKind,
  DocPage,
  DocsTreeResponse,
  ErrorEnvelope,
  GraphResponse,
  HealthResponse,
  IssueDetail,
  Issue,
  ListIssuesParams,
  MutationResult,
  ProjectSummary,
  SearchParams,
  SearchResult,
  UpdateIssueRequest,
} from './types'

export class ApiError extends Error {
  readonly status: number
  readonly code: string
  readonly envelope: ErrorEnvelope

  constructor(status: number, envelope: ErrorEnvelope) {
    super(envelope.error.message)
    this.name = 'ApiError'
    this.status = status
    this.code = envelope.error.code
    this.envelope = envelope
  }
}

export interface ApiClientOptions {
  baseUrl?: string
  fetch?: typeof fetch
}

type QueryValue = string | boolean | undefined

interface RequestOptions {
  method?: 'GET' | 'POST' | 'PATCH' | 'DELETE'
  body?: unknown
  query?: Record<string, QueryValue>
}

export class ApiClient {
  private readonly baseUrl: string
  private readonly fetcher: typeof fetch

  constructor(options: ApiClientOptions = {}) {
    this.baseUrl = trimTrailingSlash(options.baseUrl ?? '/api')
    this.fetcher = options.fetch ?? globalThis.fetch.bind(globalThis)
  }

  health(): Promise<HealthResponse> {
    return this.request('/health')
  }

  listProjects(): Promise<ProjectSummary[]> {
    return this.request('/projects')
  }

  listIssues(project: string, params: ListIssuesParams = {}): Promise<Issue[]> {
    return this.request(`/projects/${encodeURIComponent(project)}/issues`, {
      query: {
        status: params.status,
        type: params.type,
        label: params.label,
        archived: params.archived,
        q: params.q,
      },
    })
  }

  ready(project: string): Promise<Issue[]> {
    return this.request(`/projects/${encodeURIComponent(project)}/ready`)
  }

  getIssue(id: string): Promise<IssueDetail> {
    return this.request(`/issues/${encodeURIComponent(id)}`)
  }

  createIssue(project: string, body: CreateIssueRequest): Promise<CreateIssueResult> {
    return this.request(`/projects/${encodeURIComponent(project)}/issues`, {
      method: 'POST',
      body,
    })
  }

  updateIssue(id: string, body: UpdateIssueRequest): Promise<MutationResult> {
    return this.request(`/issues/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body,
    })
  }

  addNote(id: string, text: string): Promise<MutationResult> {
    return this.request(`/issues/${encodeURIComponent(id)}/notes`, {
      method: 'POST',
      body: { text },
    })
  }

  /** The server requires a non-empty reason; callers must not invoke this with one. */
  closeIssue(id: string, reason: string): Promise<MutationResult> {
    return this.request(`/issues/${encodeURIComponent(id)}/close`, {
      method: 'POST',
      body: { reason },
    })
  }

  reopenIssue(id: string): Promise<MutationResult> {
    return this.request(`/issues/${encodeURIComponent(id)}/reopen`, {
      method: 'POST',
    })
  }

  addDependency(id: string, input: AddDependencyRequest): Promise<MutationResult> {
    return this.request(`/issues/${encodeURIComponent(id)}/deps`, {
      method: 'POST',
      body: input,
    })
  }

  removeDependency(id: string, target: string, type: DependencyKind): Promise<MutationResult> {
    return this.request(`/issues/${encodeURIComponent(id)}/deps/${encodeURIComponent(target)}`, {
      method: 'DELETE',
      query: { type },
    })
  }

  /**
   * Links `childId` under `parentId`. Dependency edges are recorded on the
   * child, so the URL id is the child and `target` is the parent — do not
   * call `addDependency` with the ids the other way around for this.
   */
  addChild(parentId: string, childId: string): Promise<MutationResult> {
    return this.request(`/issues/${encodeURIComponent(childId)}/deps`, {
      method: 'POST',
      body: { target: parentId, type: 'parent-child' } satisfies AddDependencyRequest,
    })
  }

  /** Unlinks `childId` from `parentId`. See {@link addChild} for the id direction. */
  removeChild(parentId: string, childId: string): Promise<MutationResult> {
    return this.request(`/issues/${encodeURIComponent(childId)}/deps/${encodeURIComponent(parentId)}`, {
      method: 'DELETE',
      query: { type: 'parent-child' },
    })
  }

  graph(params: { project?: string; all?: boolean } = {}): Promise<GraphResponse> {
    return this.request('/graph', { query: { project: params.project, all: params.all } })
  }

  docsTree(project?: string): Promise<DocsTreeResponse> {
    return this.request('/docs/tree', { query: { project } })
  }

  getDoc(path: string): Promise<DocPage> {
    return this.request(`/docs/${encodePathSegments(path)}`)
  }

  search(params: SearchParams): Promise<SearchResult[]> {
    return this.request('/search', {
      query: { q: params.q, kind: params.kind, project: params.project },
    })
  }

  /** URL for the server-sent `reload` event stream; consumed via `new EventSource(...)`. */
  eventsUrl(): string {
    return `${this.baseUrl}/events`
  }

  private async request<T>(path: string, options: RequestOptions = {}): Promise<T> {
    const response = await this.fetcher(`${this.baseUrl}${path}${buildQuery(options.query)}`, {
      method: options.method ?? 'GET',
      headers: requestHeaders(options.body),
      body: options.body === undefined ? undefined : JSON.stringify(options.body),
    })

    if (response.status === 204) {
      return undefined as T
    }

    const payload = await readJSON(response)
    if (!response.ok) {
      throw new ApiError(response.status, toErrorEnvelope(payload))
    }
    return payload as T
  }
}

export const api = new ApiClient()

function trimTrailingSlash(value: string): string {
  return value.endsWith('/') ? value.slice(0, -1) : value
}

function encodePathSegments(path: string): string {
  return path
    .split('/')
    .filter((segment) => segment !== '')
    .map(encodeURIComponent)
    .join('/')
}

function buildQuery(query: RequestOptions['query']): string {
  if (!query) {
    return ''
  }
  const search = new URLSearchParams()
  for (const [key, value] of Object.entries(query)) {
    if (value === undefined || value === '') {
      continue
    }
    search.set(key, String(value))
  }
  const encoded = search.toString()
  return encoded === '' ? '' : `?${encoded}`
}

function requestHeaders(body: unknown): HeadersInit {
  if (body === undefined) {
    return { Accept: 'application/json' }
  }
  return {
    Accept: 'application/json',
    'Content-Type': 'application/json',
  }
}

async function readJSON(response: Response): Promise<unknown> {
  const text = await response.text()
  if (text === '') {
    return undefined
  }
  return JSON.parse(text) as unknown
}

function toErrorEnvelope(payload: unknown): ErrorEnvelope {
  if (isErrorEnvelope(payload)) {
    return payload
  }
  return { error: { code: 'request_error', message: 'request failed' } }
}

function isErrorEnvelope(payload: unknown): payload is ErrorEnvelope {
  if (typeof payload !== 'object' || payload === null) {
    return false
  }
  const candidate = payload as { error?: unknown }
  if (typeof candidate.error !== 'object' || candidate.error === null) {
    return false
  }
  const err = candidate.error as { code?: unknown; message?: unknown }
  return typeof err.code === 'string' && typeof err.message === 'string'
}
