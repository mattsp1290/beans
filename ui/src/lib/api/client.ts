import type {
  AddDependencyRequest,
  CloseIssueRequest,
  CreateIssueRequest,
  Dependency,
  DependencyListResponse,
  ErrorEnvelope,
  GraphResponse,
  HealthResponse,
  Issue,
  IssueListResponse,
  ListIssuesParams,
  UpdateIssueRequest,
} from './types'

export class ApiError extends Error {
  readonly status: number
  readonly code: string
  readonly fields: ErrorEnvelope['fields']
  readonly envelope: ErrorEnvelope

  constructor(status: number, envelope: ErrorEnvelope) {
    super(envelope.message)
    this.name = 'ApiError'
    this.status = status
    this.code = envelope.error
    this.fields = envelope.fields
    this.envelope = envelope
  }
}

export interface ApiClientOptions {
  baseUrl?: string
  fetch?: typeof fetch
}

export class ApiClient {
  private readonly baseUrl: string
  private readonly fetcher: typeof fetch

  constructor(options: ApiClientOptions = {}) {
    this.baseUrl = trimTrailingSlash(options.baseUrl ?? '/api/v1')
    this.fetcher = options.fetch ?? globalThis.fetch.bind(globalThis)
  }

  health(): Promise<HealthResponse> {
    return this.request('/healthz')
  }

  listIssues(params: ListIssuesParams = {}): Promise<IssueListResponse> {
    return this.request(`/issues${listIssuesQuery(params)}`)
  }

  createIssue(input: CreateIssueRequest): Promise<Issue> {
    return this.request('/issues', {
      method: 'POST',
      body: input,
    })
  }

  getIssue(id: string): Promise<Issue> {
    return this.request(`/issues/${encodeURIComponent(id)}`)
  }

  updateIssue(id: string, input: UpdateIssueRequest): Promise<Issue> {
    return this.request(`/issues/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: input,
    })
  }

  closeIssue(id: string, input: CloseIssueRequest = {}): Promise<Issue> {
    return this.request(`/issues/${encodeURIComponent(id)}/close`, {
      method: 'POST',
      body: input,
    })
  }

  deleteIssue(id: string): Promise<void> {
    return this.request(`/issues/${encodeURIComponent(id)}`, {
      method: 'DELETE',
    })
  }

  listDependencies(): Promise<DependencyListResponse> {
    return this.request('/deps')
  }

  addDependency(issueId: string, input: AddDependencyRequest): Promise<Dependency> {
    return this.request(`/issues/${encodeURIComponent(issueId)}/deps`, {
      method: 'POST',
      body: input,
    })
  }

  removeDependency(issueId: string, blockedById: string): Promise<void> {
    return this.request(
      `/issues/${encodeURIComponent(issueId)}/deps/${encodeURIComponent(blockedById)}`,
      { method: 'DELETE' },
    )
  }

  ready(): Promise<IssueListResponse> {
    return this.request('/ready')
  }

  graph(): Promise<GraphResponse> {
    return this.request('/graph')
  }

  private async request<T>(path: string, options: RequestOptions = {}): Promise<T> {
    const response = await this.fetcher(`${this.baseUrl}${path}`, {
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

interface RequestOptions {
  method?: 'GET' | 'POST' | 'PATCH' | 'DELETE'
  body?: unknown
}

export const api = new ApiClient()

function trimTrailingSlash(value: string): string {
  return value.endsWith('/') ? value.slice(0, -1) : value
}

function listIssuesQuery(params: ListIssuesParams): string {
  const query = new URLSearchParams()
  const states = Array.isArray(params.state)
    ? params.state
    : params.state === undefined
      ? []
      : [params.state]
  for (const state of states) {
    query.append('state', state)
  }
  if (params.limit !== undefined) {
    query.set('limit', String(params.limit))
  }
  const encoded = query.toString()
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
  return {
    error: 'request_error',
    message: 'request failed',
  }
}

function isErrorEnvelope(payload: unknown): payload is ErrorEnvelope {
  if (typeof payload !== 'object' || payload === null) {
    return false
  }
  const candidate = payload as Partial<ErrorEnvelope>
  return typeof candidate.error === 'string' && typeof candidate.message === 'string'
}
