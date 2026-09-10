import { describe, expect, it } from 'vitest'

import {
  emptyIssueForm,
  issueFormToCreateRequest,
  issueFormToUpdateRequest,
  issueToIssueForm,
  validateIssueForm,
} from './form'

describe('issue form helpers', () => {
  it('omits blank optional fields for create requests', () => {
    const form = emptyIssueForm()
    form.title = '  New issue  '
    form.labels = ' ui, , api '

    expect(issueFormToCreateRequest(form)).toEqual({
      title: 'New issue',
      description: '',
      priority: 2,
      issue_type: 'task',
      labels: ['ui', 'api'],
      branch_name: undefined,
      url: undefined,
    })
  })

  it('keeps blank optional fields for update requests so existing values can be cleared', () => {
    const form = emptyIssueForm()
    form.title = 'Existing issue'
    form.issue_type = 'bug'
    form.branch_name = '   '
    form.url = ''

    expect(issueFormToUpdateRequest(form)).toEqual({
      title: 'Existing issue',
      description: '',
      priority: 2,
      labels: [],
      branch_name: '',
      url: '',
    })
  })

  it('does not send immutable issue type in update requests', () => {
    const form = emptyIssueForm()
    form.title = 'Existing issue'
    form.issue_type = 'feature'

    expect(issueFormToUpdateRequest(form)).not.toHaveProperty('issue_type')
  })

  it('maps issue detail data into the editable form model', () => {
    expect(
      issueToIssueForm({
        id: 'bc-1',
        identifier: 'bc-1',
        title: 'Edit me',
        description: 'Details',
        priority: 3,
        issue_type: 'feature',
        state: 'open',
        labels: ['ui', 'api'],
        blocked_by: [],
        branch_name: 'feature/bc-1',
        url: 'https://tracker.example/bc-1',
        created_at: '2026-06-14T12:00:00Z',
        updated_at: '2026-06-14T12:00:00Z',
      }),
    ).toEqual({
      title: 'Edit me',
      description: 'Details',
      priority: 3,
      issue_type: 'feature',
      labels: 'ui, api',
      branch_name: 'feature/bc-1',
      url: 'https://tracker.example/bc-1',
    })
  })

  it('validates title and URL constraints', () => {
    const form = emptyIssueForm()
    expect(validateIssueForm(form)).toBe('Title is required.')

    form.title = 'Valid'
    form.url = 'ftp://example.test'
    expect(validateIssueForm(form)).toBe('URL must start with http:// or https://.')
  })
})
