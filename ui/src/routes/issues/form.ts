import type { CreateIssueRequest } from '../../lib/api'

/** Editable form model backing the "new issue" panel on the issues board. */
export interface IssueForm {
  title: string
  description: string
  priority: number
  type: string
  labels: string
  parent: string
  assignee: string
  blocked_by: string
  url: string
}

export function emptyIssueForm(): IssueForm {
  return {
    title: '',
    description: '',
    priority: 2,
    type: 'task',
    labels: '',
    parent: '',
    assignee: '',
    blocked_by: '',
    url: '',
  }
}

export function issueFormToCreateRequest(value: IssueForm): CreateIssueRequest {
  const description = value.description.trim()
  const parent = value.parent.trim()
  const assignee = value.assignee.trim()
  const url = value.url.trim()
  const labels = splitList(value.labels)
  const blockedBy = splitList(value.blocked_by)
  return {
    title: value.title.trim(),
    description: description === '' ? undefined : description,
    priority: Number(value.priority),
    type: value.type,
    labels: labels.length > 0 ? labels : undefined,
    parent: parent === '' ? undefined : parent,
    assignee: assignee === '' ? undefined : assignee,
    blocked_by: blockedBy.length > 0 ? blockedBy : undefined,
    url: url === '' ? undefined : url,
  }
}

export function validateIssueForm(value: IssueForm): string {
  if (value.title.trim() === '') {
    return 'Title is required.'
  }
  if (value.title.length > 300) {
    return 'Title must be at most 300 characters.'
  }
  if (value.description.length > 20000) {
    return 'Description must be at most 20000 characters.'
  }
  if (value.priority < 0 || value.priority > 4) {
    return 'Priority must be between 0 and 4.'
  }
  const labels = splitList(value.labels)
  if (labels.length > 100) {
    return 'Use at most 100 labels.'
  }
  if (labels.some((label) => label.length > 100)) {
    return 'Labels must be at most 100 characters.'
  }
  if (value.url.trim() !== '' && !/^https?:\/\//.test(value.url.trim())) {
    return 'URL must start with http:// or https://.'
  }
  return ''
}

function splitList(value: string): string[] {
  return value
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean)
}
