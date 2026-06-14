import { describe, expect, it } from 'vitest'

import { emptyIssueForm, issueFormToCreateRequest, issueFormToUpdateRequest, validateIssueForm } from './form'

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

  it('validates title and URL constraints', () => {
    const form = emptyIssueForm()
    expect(validateIssueForm(form)).toBe('Title is required.')

    form.title = 'Valid'
    form.url = 'ftp://example.test'
    expect(validateIssueForm(form)).toBe('URL must start with http:// or https://.')
  })
})
