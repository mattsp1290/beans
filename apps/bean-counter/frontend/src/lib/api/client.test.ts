import { describe, expect, it, vi } from 'vitest'

import { ApiClient, ApiError } from './client'

describe('ApiClient', () => {
  it('lists issues with repeated state filters through the default API base', async () => {
    const fetcher = mockFetch({ issues: [] })
    const client = new ApiClient({ fetch: fetcher })

    await client.listIssues({ state: ['open', 'blocked'], limit: 25 })

    expect(fetcher).toHaveBeenCalledWith('/api/v1/issues?state=open&state=blocked&limit=25', {
      method: 'GET',
      headers: { Accept: 'application/json' },
      body: undefined,
    })
  })

  it('sends JSON bodies for mutations', async () => {
    const fetcher = mockFetch({
      id: 'bc-1',
      identifier: 'bc-1',
      title: 'New',
      description: '',
      priority: 2,
      issue_type: 'feature',
      state: 'open',
      labels: [],
      blocked_by: [],
      created_at: '2026-06-14T12:00:00Z',
      updated_at: '2026-06-14T12:00:00Z',
    })
    const client = new ApiClient({ baseUrl: '/custom/', fetch: fetcher })

    const issue = await client.createIssue({
      title: 'New',
      priority: 2,
      issue_type: 'feature',
    })

    expect(issue.id).toBe('bc-1')
    expect(fetcher).toHaveBeenCalledWith('/custom/issues', {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({ title: 'New', priority: 2, issue_type: 'feature' }),
    })
  })

  it('returns undefined for no-content deletes', async () => {
    const fetcher = vi.fn().mockResolvedValue(new Response(null, { status: 204 }))
    const client = new ApiClient({ fetch: fetcher })

    await expect(client.deleteIssue('bc-1')).resolves.toBeUndefined()

    expect(fetcher).toHaveBeenCalledWith('/api/v1/issues/bc-1', {
      method: 'DELETE',
      headers: { Accept: 'application/json' },
      body: undefined,
    })
  })

  it('loads the ready queue from the ready endpoint', async () => {
    const fetcher = mockFetch({ issues: [] })
    const client = new ApiClient({ fetch: fetcher })

    await expect(client.ready()).resolves.toEqual({ issues: [] })

    expect(fetcher).toHaveBeenCalledWith('/api/v1/ready', {
      method: 'GET',
      headers: { Accept: 'application/json' },
      body: undefined,
    })
  })

  it('adds and removes issue dependencies', async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse({ issue_id: 'bc-2', blocked_by_id: 'bc-1' }))
      .mockResolvedValueOnce(new Response(null, { status: 204 }))
    const client = new ApiClient({ fetch: fetcher })

    await expect(client.addDependency('bc-2', { blocked_by_id: 'bc-1' })).resolves.toEqual({
      issue_id: 'bc-2',
      blocked_by_id: 'bc-1',
    })

    expect(fetcher).toHaveBeenCalledWith('/api/v1/issues/bc-2/deps', {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({ blocked_by_id: 'bc-1' }),
    })

    await expect(client.removeDependency('bc-2', 'bc-1')).resolves.toBeUndefined()

    expect(fetcher).toHaveBeenLastCalledWith('/api/v1/issues/bc-2/deps/bc-1', {
      method: 'DELETE',
      headers: { Accept: 'application/json' },
      body: undefined,
    })
  })

  it('throws ApiError with the server error envelope', async () => {
    const fetcher = mockFetch(
      {
        error: 'validation_error',
        message: 'validation failed',
        fields: [{ field: 'title', message: 'is required' }],
      },
      { status: 400 },
    )
    const client = new ApiClient({ fetch: fetcher })

    await expect(client.getIssue('bc-1')).rejects.toMatchObject({
      status: 400,
      code: 'validation_error',
      fields: [{ field: 'title', message: 'is required' }],
    })
  })

  it('falls back to request_error for malformed error payloads', async () => {
    const fetcher = mockFetch({ unexpected: true }, { status: 502 })
    const client = new ApiClient({ fetch: fetcher })

    const error = await client.health().catch((err: unknown) => err)
    expect(error).toBeInstanceOf(ApiError)
    expect(error).toMatchObject({
      status: 502,
      code: 'request_error',
    })
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
