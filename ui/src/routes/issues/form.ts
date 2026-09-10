import type { CreateIssueRequest, Issue, IssueType, UpdateIssueRequest } from '../../lib/api'

export interface IssueForm {
  title: string
  description: string
  priority: number
  issue_type: IssueType
  labels: string
  branch_name: string
  url: string
}

export function emptyIssueForm(): IssueForm {
  return {
    title: '',
    description: '',
    priority: 2,
    issue_type: 'task',
    labels: '',
    branch_name: '',
    url: '',
  }
}

export function issueToIssueForm(issue: Issue): IssueForm {
  return {
    title: issue.title,
    description: issue.description,
    priority: issue.priority,
    issue_type: issue.issue_type,
    labels: issue.labels.join(', '),
    branch_name: issue.branch_name ?? '',
    url: issue.url ?? '',
  }
}

export function issueFormToCreateRequest(value: IssueForm): CreateIssueRequest {
  const branchName = value.branch_name.trim()
  const url = value.url.trim()
  return {
    title: value.title.trim(),
    description: value.description,
    priority: Number(value.priority),
    issue_type: value.issue_type,
    labels: labelsFromIssueForm(value),
    branch_name: branchName || undefined,
    url: url || undefined,
  }
}

export function issueFormToUpdateRequest(value: IssueForm): UpdateIssueRequest {
  return {
    title: value.title.trim(),
    description: value.description,
    priority: Number(value.priority),
    labels: labelsFromIssueForm(value),
    branch_name: value.branch_name.trim(),
    url: value.url.trim(),
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
  const labels = value.labels.split(',').map((label) => label.trim())
  if (labels.filter(Boolean).length > 100) {
    return 'Use at most 100 labels.'
  }
  if (labels.some((label) => label.length > 100)) {
    return 'Labels must be at most 100 characters.'
  }
  if (value.branch_name.length > 255) {
    return 'Branch name must be at most 255 characters.'
  }
  if (value.url.trim() !== '' && !/^https?:\/\//.test(value.url.trim())) {
    return 'URL must start with http:// or https://.'
  }
  return ''
}

function labelsFromIssueForm(value: IssueForm): string[] {
  return value.labels
    .split(',')
    .map((label) => label.trim())
    .filter(Boolean)
}
