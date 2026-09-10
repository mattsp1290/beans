import { describe, expect, it } from 'vitest'

import { emptyIssueForm, issueFormToCreateRequest, validateIssueForm } from './form'

describe('issue form helpers', () => {
  it('omits blank optional fields and trims list fields for create requests', () => {
    const form = emptyIssueForm()
    form.title = '  New issue  '
    form.labels = ' ui, , api '
    form.blocked_by = ' bn-1, bn-2 '

    expect(issueFormToCreateRequest(form)).toEqual({
      title: 'New issue',
      description: undefined,
      priority: 2,
      type: 'task',
      labels: ['ui', 'api'],
      parent: undefined,
      assignee: undefined,
      blocked_by: ['bn-1', 'bn-2'],
      url: undefined,
    })
  })

  it('includes trimmed optional fields when set', () => {
    const form = emptyIssueForm()
    form.title = 'Existing issue'
    form.type = 'bug'
    form.description = 'Details'
    form.parent = ' bn-9 '
    form.assignee = ' matt '
    form.url = ' https://example.test/x '

    expect(issueFormToCreateRequest(form)).toMatchObject({
      title: 'Existing issue',
      type: 'bug',
      description: 'Details',
      parent: 'bn-9',
      assignee: 'matt',
      url: 'https://example.test/x',
    })
  })

  it('validates title, priority, and URL constraints', () => {
    const form = emptyIssueForm()
    expect(validateIssueForm(form)).toBe('Title is required.')

    form.title = 'Valid'
    form.priority = 9
    expect(validateIssueForm(form)).toBe('Priority must be between 0 and 4.')

    form.priority = 2
    form.url = 'ftp://example.test'
    expect(validateIssueForm(form)).toBe('URL must start with http:// or https://.')

    form.url = 'https://example.test'
    expect(validateIssueForm(form)).toBe('')
  })

  it('rejects more than 100 labels', () => {
    const form = emptyIssueForm()
    form.title = 'Valid'
    form.labels = Array.from({ length: 101 }, (_, i) => `label-${i}`).join(',')
    expect(validateIssueForm(form)).toBe('Use at most 100 labels.')
  })
})
