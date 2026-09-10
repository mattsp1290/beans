import { describe, expect, it, vi } from 'vitest'

import { ApiClient, ApiError } from './client'

describe('ApiClient', () => {
  it('uses /api as the default base URL', async () => {
    const fetcher = mockFetch({ status: 'ok', hub: '/hub', project: 'beans', ahead: 0, behind: 0 })
    const client = new ApiClient({ fetch: fetcher })

    await client.health()

    expect(fetcher).toHaveBeenCalledWith('/api/health', {
      method: 'GET',
      headers: { Accept: 'application/json' },
      body: undefined,
    })
  })

  it('lists projects, including each project workflow', async () => {
    const fetcher = mockFetch([
      {
        name: 'beans',
        prefix: 'bn',
        counts: { open: 1, in_progress: 0, closed: 2 },
        workflow: { statuses: ['open', 'in_progress', 'closed'], active: ['open', 'in_progress'], terminal: ['closed'] },
      },
    ])
    const client = new ApiClient({ baseUrl: '/custom/', fetch: fetcher })

    const projects = await client.listProjects()

    expect(projects).toEqual([
      {
        name: 'beans',
        prefix: 'bn',
        counts: { open: 1, in_progress: 0, closed: 2 },
        workflow: { statuses: ['open', 'in_progress', 'closed'], active: ['open', 'in_progress'], terminal: ['closed'] },
      },
    ])
    expect(projects[0].workflow.terminal).toEqual(['closed'])
    expect(fetcher).toHaveBeenCalledWith('/custom/projects', {
      method: 'GET',
      headers: { Accept: 'application/json' },
      body: undefined,
    })
  })

  it('lists project issues with filters, omitting unset params', async () => {
    const fetcher = mockFetch([issue()])
    const client = new ApiClient({ fetch: fetcher })

    await client.listIssues('beans', { status: 'open', type: 'bug', archived: true })

    expect(fetcher).toHaveBeenCalledWith(
      '/api/projects/beans/issues?status=open&type=bug&archived=true',
      {
        method: 'GET',
        headers: { Accept: 'application/json' },
        body: undefined,
      },
    )
  })

  it('routes "_all" through the projects path for cross-project listing', async () => {
    const fetcher = mockFetch([])
    const client = new ApiClient({ fetch: fetcher })

    await client.listIssues('_all')

    expect(fetcher).toHaveBeenCalledWith('/api/projects/_all/issues', {
      method: 'GET',
      headers: { Accept: 'application/json' },
      body: undefined,
    })
  })

  it('loads the ready queue for a project', async () => {
    const fetcher = mockFetch([issue()])
    const client = new ApiClient({ fetch: fetcher })

    await expect(client.ready('beans')).resolves.toEqual([issue()])
    expect(fetcher).toHaveBeenCalledWith('/api/projects/beans/ready', {
      method: 'GET',
      headers: { Accept: 'application/json' },
      body: undefined,
    })
  })

  it('fetches an issue detail with URL-encoded ids', async () => {
    const fetcher = mockFetch({ ...issue(), html: '<p>hi</p>', workflow: { statuses: ['open'], active: ['open'], terminal: [] } })
    const client = new ApiClient({ fetch: fetcher })

    const detail = await client.getIssue('bn/1')

    expect(detail.html).toBe('<p>hi</p>')
    expect(fetcher).toHaveBeenCalledWith('/api/issues/bn%2F1', {
      method: 'GET',
      headers: { Accept: 'application/json' },
      body: undefined,
    })
  })

  it('creates an issue under a project', async () => {
    const fetcher = mockFetch({ id: 'bn-1', path: 'issues/bn-1.md', commit: 'abc', pushed: true, message: 'ok' })
    const client = new ApiClient({ fetch: fetcher })

    const result = await client.createIssue('beans', { title: 'New', priority: 2 })

    expect(result.id).toBe('bn-1')
    expect(fetcher).toHaveBeenCalledWith('/api/projects/beans/issues', {
      method: 'POST',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: JSON.stringify({ title: 'New', priority: 2 }),
    })
  })

  it('patches an issue with a partial update', async () => {
    const fetcher = mockFetch({ commit: 'abc', pushed: true, message: 'ok' })
    const client = new ApiClient({ fetch: fetcher })

    await client.updateIssue('bn-1', { status: 'in_progress' })

    expect(fetcher).toHaveBeenCalledWith('/api/issues/bn-1', {
      method: 'PATCH',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: JSON.stringify({ status: 'in_progress' }),
    })
  })

  it('posts a note, closes, and reopens an issue', async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(mutation()))
      .mockResolvedValueOnce(jsonResponse(mutation()))
      .mockResolvedValueOnce(jsonResponse(mutation()))
    const client = new ApiClient({ fetch: fetcher })

    await client.addNote('bn-1', 'note text')
    expect(fetcher).toHaveBeenNthCalledWith(1, '/api/issues/bn-1/notes', {
      method: 'POST',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: JSON.stringify({ text: 'note text' }),
    })

    await client.closeIssue('bn-1', 'done')
    expect(fetcher).toHaveBeenNthCalledWith(2, '/api/issues/bn-1/close', {
      method: 'POST',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: JSON.stringify({ reason: 'done' }),
    })

    await client.reopenIssue('bn-1')
    expect(fetcher).toHaveBeenNthCalledWith(3, '/api/issues/bn-1/reopen', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      body: undefined,
    })
  })

  it('adds and removes dependencies with a type query param on removal', async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(mutation()))
      .mockResolvedValueOnce(jsonResponse(mutation()))
    const client = new ApiClient({ fetch: fetcher })

    await client.addDependency('bn-2', { target: 'bn-1', type: 'blocks' })
    expect(fetcher).toHaveBeenNthCalledWith(1, '/api/issues/bn-2/deps', {
      method: 'POST',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: JSON.stringify({ target: 'bn-1', type: 'blocks' }),
    })

    await client.removeDependency('bn-2', 'bn-1', 'blocks')
    expect(fetcher).toHaveBeenNthCalledWith(2, '/api/issues/bn-2/deps/bn-1?type=blocks', {
      method: 'DELETE',
      headers: { Accept: 'application/json' },
      body: undefined,
    })
  })

  it('addChild posts the dependency on the CHILD id with the parent as the target', async () => {
    const fetcher = mockFetch(mutation())
    const client = new ApiClient({ fetch: fetcher })

    await client.addChild('bn-parent', 'bn-child')

    expect(fetcher).toHaveBeenCalledWith('/api/issues/bn-child/deps', {
      method: 'POST',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: JSON.stringify({ target: 'bn-parent', type: 'parent-child' }),
    })
  })

  it('removeChild deletes the dependency on the CHILD id with the parent as the target segment', async () => {
    const fetcher = mockFetch(mutation())
    const client = new ApiClient({ fetch: fetcher })

    await client.removeChild('bn-parent', 'bn-child')

    expect(fetcher).toHaveBeenCalledWith('/api/issues/bn-child/deps/bn-parent?type=parent-child', {
      method: 'DELETE',
      headers: { Accept: 'application/json' },
      body: undefined,
    })
  })

  it('loads the graph, dropping the project param for cross-project requests', async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse({ nodes: [], edges: [] }))
      .mockResolvedValueOnce(jsonResponse({ nodes: [], edges: [] }))
    const client = new ApiClient({ fetch: fetcher })

    await client.graph({ project: 'beans' })
    expect(fetcher).toHaveBeenNthCalledWith(1, '/api/graph?project=beans', {
      method: 'GET',
      headers: { Accept: 'application/json' },
      body: undefined,
    })

    await client.graph({ all: true })
    expect(fetcher).toHaveBeenNthCalledWith(2, '/api/graph?all=true', {
      method: 'GET',
      headers: { Accept: 'application/json' },
      body: undefined,
    })
  })

  it('loads the docs tree and a doc page, URL-encoding each path segment', async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse({ docs: [{ path: 'a/b.md', title: 'B', project: 'beans' }] }))
      .mockResolvedValueOnce(
        jsonResponse({ path: 'a/b.md', title: 'B', frontmatter: {}, html: '<p>b</p>', toc: [], backlinks: [], outlinks: [] }),
      )
    const client = new ApiClient({ fetch: fetcher })

    await client.docsTree('beans')
    expect(fetcher).toHaveBeenNthCalledWith(1, '/api/docs/tree?project=beans', {
      method: 'GET',
      headers: { Accept: 'application/json' },
      body: undefined,
    })

    await client.getDoc('a folder/b.md')
    expect(fetcher).toHaveBeenNthCalledWith(2, '/api/docs/a%20folder/b.md', {
      method: 'GET',
      headers: { Accept: 'application/json' },
      body: undefined,
    })
  })

  it('searches with q, kind, and project query params', async () => {
    const fetcher = mockFetch([{ kind: 'issue', id: 'bn-1', basename: 'bn-1', title: 'Issue', project: 'beans', path: 'issues/bn-1.md', score: 1.5 }])
    const client = new ApiClient({ fetch: fetcher })

    await client.search({ q: 'hello', kind: 'issue', project: 'beans' })

    expect(fetcher).toHaveBeenCalledWith('/api/search?q=hello&kind=issue&project=beans', {
      method: 'GET',
      headers: { Accept: 'application/json' },
      body: undefined,
    })
  })

  it('builds the events URL from the configured base', () => {
    const client = new ApiClient({ baseUrl: '/custom' })
    expect(client.eventsUrl()).toBe('/custom/events')
  })

  it('throws ApiError with the nested server error envelope', async () => {
    const fetcher = mockFetch(
      { error: { code: 'not_found', message: 'issue not found' } },
      { status: 404 },
    )
    const client = new ApiClient({ fetch: fetcher })

    await expect(client.getIssue('bn-missing')).rejects.toMatchObject({
      status: 404,
      code: 'not_found',
      message: 'issue not found',
    })
  })

  it('falls back to request_error for malformed error payloads', async () => {
    const fetcher = mockFetch({ unexpected: true }, { status: 502 })
    const client = new ApiClient({ fetch: fetcher })

    const error = await client.health().catch((err: unknown) => err)
    expect(error).toBeInstanceOf(ApiError)
    expect(error).toMatchObject({ status: 502, code: 'request_error' })
  })
})

function mockFetch(payload: unknown, init: ResponseInit = {}): typeof fetch {
  return vi.fn().mockResolvedValue(jsonResponse(payload, init))
}

function jsonResponse(payload: unknown, init: ResponseInit = {}): Response {
  return new Response(JSON.stringify(payload), {
    status: init.status ?? 200,
    headers: { 'Content-Type': 'application/json' },
  })
}

function mutation(): Record<string, unknown> {
  return { commit: 'abc123', pushed: true, message: 'committed' }
}

function issue(overrides: Partial<Record<string, unknown>> = {}): Record<string, unknown> {
  return {
    id: 'bn-1',
    title: 'Issue',
    type: 'task',
    status: 'open',
    priority: 2,
    labels: [],
    assignee: '',
    parent: '',
    blocked_by: [],
    url: '',
    created: '2026-06-14T12:00:00Z',
    updated: '2026-06-14T12:00:00Z',
    project: 'beans',
    path: 'issues/bn-1.md',
    archived: false,
    ...overrides,
  }
}
