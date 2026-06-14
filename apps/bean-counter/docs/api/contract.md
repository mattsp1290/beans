# bean-counter API contract

Base path: `/api/v1`

This document reflects the shipped Fiber handlers and DTOs in `internal/api`,
`internal/handlers`, and `internal/server`.

## Conventions

- JSON is used for every request and response body except `204 No Content`.
- Timestamps are Go `time.Time` values encoded as RFC 3339 strings.
- Issue IDs must use the configured project prefix, for example `bean-counter-123abc`.
- Public priorities are integers from `0` through `4`; `2` is the default medium priority.
- Issue types are `bug`, `feature`, `task`, `epic`, and `chore`.
- Issue states are `open`, `in_progress`, `blocked`, `closed`, and `done`.
- Dependency direction is `issue_id` is blocked by `blocked_by_id`.
- Graph edges use `source = blocked_by_id` and `target = issue_id`.
- Empty arrays are encoded as `[]`.
- Optional response fields using `omitempty` are absent when empty: `branch_name`, `url`, `repo`, and optional nested `repo` fields.

## Validation Limits

| Field | Limit |
| --- | ---: |
| `id`, `blocked_by_id` | required, max 200 chars |
| `title` | required on create, max 300 chars |
| `description` | max 20000 chars |
| `labels` | max 100 labels, each non-blank and max 100 chars |
| `branch_name` | max 255 chars |
| `url` | max 2048 chars, absolute `http` or `https` URL when provided |
| `reason` | max 1000 chars |
| `repo.repo_slug` | required when `repo` is present, max 200 chars |
| repo refs | max 255 chars and valid git ref names |
| `repo.worktree_subdir` | max 255 chars, relative path without parent traversal |

For updates, omitted fields keep their current values. `labels: []` clears labels. `labels: null` is invalid. An update body must include at least one updatable field.

## Error Envelope

All handler errors are returned as:

```json
{
  "error": "validation_error",
  "message": "invalid request",
  "fields": [
    {"field": "title", "message": "is required"}
  ]
}
```

`fields` is present only for validation errors with field-level detail.

| Error source | Status | `error` |
| --- | ---: | --- |
| Malformed JSON or query | 400 | `bad_request` |
| Validation failure | 400 | `validation_error` |
| `store.ErrNotFound` | 404 | `not_found` |
| `store.ErrCycle` | 409 | `conflict` |
| `store.ErrDuplicateDep` | 409 | `conflict` |
| `store.ErrConflict` | 409 | `conflict` |
| `store.ErrDisabled` | 409 | `conflict` |
| `store.ErrEmptyDSN` | 500 | `store_configuration_error` |
| `store.ErrUnsupportedDriver` | 500 | `store_configuration_error` |
| Unhandled error | 500 | `internal_error` |

## DTO Reference

### Issue

```json
{
  "id": "bean-counter-123abc",
  "identifier": "bean-counter-123abc",
  "title": "Add issue list",
  "description": "Render issues from beans",
  "priority": 2,
  "issue_type": "feature",
  "state": "open",
  "labels": ["ui"],
  "blocked_by": ["bean-counter-100aaa"],
  "branch_name": "feature/bean-counter-123abc",
  "url": "https://tracker.example/bean-counter-123abc",
  "repo": {
    "id": "repo-1",
    "slug": "bean-counter",
    "remote_url": "git@example.com:mattsp1290/bean-counter.git",
    "default_branch": "main",
    "requested_ref": "main",
    "base_ref": "main",
    "work_branch": "feature/bean-counter-123abc",
    "worktree_subdir": "services/api",
    "clone_strategy": "full",
    "auth_ref": "default",
    "metadata": {"team": "core"}
  },
  "created_at": "2026-06-14T12:00:00Z",
  "updated_at": "2026-06-14T13:00:00Z"
}
```

### CreateIssueRequest

```json
{
  "title": "Add issue list",
  "description": "Render issues from beans",
  "priority": 2,
  "issue_type": "feature",
  "labels": ["ui"],
  "branch_name": "feature/bean-counter-123abc",
  "url": "https://tracker.example/bean-counter-123abc",
  "repo": {
    "repo_slug": "bean-counter",
    "requested_ref": "main",
    "base_ref": "main",
    "work_branch": "feature/bean-counter-123abc",
    "worktree_subdir": "services/api",
    "metadata": {"team": "core"}
  }
}
```

Required: `title`, `priority`, and `issue_type`.

### UpdateIssueRequest

```json
{
  "title": "Updated title",
  "description": "Updated body",
  "priority": 1,
  "state": "in_progress",
  "labels": [],
  "branch_name": "",
  "url": "",
  "repo": {
    "repo_slug": "bean-counter",
    "requested_ref": "main",
    "base_ref": "main",
    "work_branch": "feature/updated",
    "worktree_subdir": "services/api",
    "metadata": {"team": "core"}
  }
}
```

Every field is optional, but at least one must be present. Empty strings for `branch_name` and `url` clear those fields.

### CloseIssueRequest

```json
{
  "reason": "completed"
}
```

`reason` is optional.

### Dependency

```json
{
  "issue_id": "bean-counter-123abc",
  "blocked_by_id": "bean-counter-100aaa"
}
```

### GraphResponse

```json
{
  "nodes": [
    {
      "id": "bean-counter-100aaa",
      "title": "Parent",
      "state": "closed",
      "priority": 1,
      "labels": []
    },
    {
      "id": "bean-counter-123abc",
      "title": "Child",
      "state": "open",
      "priority": 2,
      "labels": ["ui"]
    }
  ],
  "edges": [
    {"source": "bean-counter-100aaa", "target": "bean-counter-123abc"}
  ]
}
```

## Endpoints

### Health

`GET /api/v1/healthz`

Returns process liveness only.

Response `200 OK`:

```json
{"status": "ok"}
```

### List Issues

`GET /api/v1/issues`

Query parameters:

| Name | Type | Meaning |
| --- | --- | --- |
| `state` | repeated or comma-separated string | Optional state filter. Empty means all states. |
| `limit` | non-negative integer | Optional max rows. `0` or omitted means no limit. |

Example:

```http
GET /api/v1/issues?state=open&state=blocked&limit=20
```

Response `200 OK`:

```json
{
  "issues": [
    {
      "id": "bean-counter-123abc",
      "identifier": "bean-counter-123abc",
      "title": "Add issue list",
      "description": "Render issues from beans",
      "priority": 2,
      "issue_type": "feature",
      "state": "open",
      "labels": ["ui"],
      "blocked_by": [],
      "created_at": "2026-06-14T12:00:00Z",
      "updated_at": "2026-06-14T12:00:00Z"
    }
  ]
}
```

Common errors: `400 bad_request` for malformed query or invalid `limit`, `400 validation_error` for invalid `state`.

### Create Issue

`POST /api/v1/issues`

Request:

```json
{
  "title": "Add ready queue",
  "description": "Show unblocked issues",
  "priority": 1,
  "issue_type": "feature",
  "labels": ["ui", "ready"]
}
```

Response `201 Created`:

```json
{
  "id": "bean-counter-456def",
  "identifier": "bean-counter-456def",
  "title": "Add ready queue",
  "description": "Show unblocked issues",
  "priority": 1,
  "issue_type": "feature",
  "state": "open",
  "labels": ["ui", "ready"],
  "blocked_by": [],
  "created_at": "2026-06-14T12:00:00Z",
  "updated_at": "2026-06-14T12:00:00Z"
}
```

Common errors: `400 bad_request` for malformed JSON, `400 validation_error`, `404 not_found` for unknown `repo.repo_slug`, `409 conflict` for disabled repos or store conflicts.

### Get Issue

`GET /api/v1/issues/{id}`

Response `200 OK`: `Issue`

Example:

```http
GET /api/v1/issues/bean-counter-456def
```

Common errors: `400 validation_error` for an ID outside the configured prefix, `404 not_found`.

### Update Issue

`PATCH /api/v1/issues/{id}`

Request:

```json
{
  "state": "in_progress",
  "labels": ["ui"],
  "branch_name": "feature/ready-queue"
}
```

Response `200 OK`: updated `Issue`

Common errors: `400 bad_request`, `400 validation_error`, `404 not_found`, `409 conflict`.

### Close Issue

`POST /api/v1/issues/{id}/close`

Request:

```json
{"reason": "completed"}
```

An empty body is also accepted.

Response `200 OK`: closed `Issue`

Common errors: `400 bad_request`, `400 validation_error`, `404 not_found`, `409 conflict`.

### Delete Issue

`DELETE /api/v1/issues/{id}`

Response `204 No Content`.

Common errors: `400 validation_error`, `404 not_found`, `409 conflict`.

### List Dependencies

`GET /api/v1/deps`

Response `200 OK`:

```json
{
  "dependencies": [
    {"issue_id": "bean-counter-123abc", "blocked_by_id": "bean-counter-100aaa"}
  ]
}
```

Common errors: `500 store_configuration_error` for store configuration failures, `500 internal_error` for unexpected list failures.

### Add Dependency

`POST /api/v1/issues/{id}/deps`

`id` is the blocked issue. `blocked_by_id` is the blocker.

Request:

```json
{"blocked_by_id": "bean-counter-100aaa"}
```

Response `201 Created`:

```json
{"issue_id": "bean-counter-123abc", "blocked_by_id": "bean-counter-100aaa"}
```

Common errors: `400 bad_request`, `400 validation_error` for missing IDs or self-dependency, `404 not_found`, `409 conflict` for cycles or duplicate dependencies.

### Remove Dependency

`DELETE /api/v1/issues/{id}/deps/{blocked_by_id}`

Response `204 No Content`.

Common errors: `400 validation_error`, `404 not_found`, `409 conflict`.

### Ready Queue

`GET /api/v1/ready`

Returns unblocked issues for the configured project prefix. Terminal and active state buckets are configured by the server adapter.

Response `200 OK`:

```json
{
  "issues": [
    {
      "id": "bean-counter-456def",
      "identifier": "bean-counter-456def",
      "title": "Add ready queue",
      "description": "Show unblocked issues",
      "priority": 1,
      "issue_type": "feature",
      "state": "open",
      "labels": ["ui", "ready"],
      "blocked_by": [],
      "created_at": "2026-06-14T12:00:00Z",
      "updated_at": "2026-06-14T12:00:00Z"
    }
  ]
}
```

Common errors: `404 not_found` for store not-found errors, `500 store_configuration_error` for store configuration failures, `500 internal_error` for unexpected ready queue failures.

### Dependency Graph

`GET /api/v1/graph`

The handler lists all project issues and dependency edges, then maps them to graph nodes and edges.

Response `200 OK`:

```json
{
  "nodes": [
    {"id": "bean-counter-100aaa", "title": "Parent", "state": "closed", "priority": 1, "labels": []},
    {"id": "bean-counter-123abc", "title": "Child", "state": "open", "priority": 2, "labels": ["ui"]}
  ],
  "edges": [
    {"source": "bean-counter-100aaa", "target": "bean-counter-123abc"}
  ]
}
```

Common errors: `500 store_configuration_error` for store configuration failures, `500 internal_error` for unexpected list failures.

### CORS

The server allows the configured `BN_CORS_ORIGIN`. The code default is `http://localhost:5173`; the backend Docker image and full-stack compose default to `http://localhost:8080`.

Allowed methods: `GET`, `POST`, `PUT`, `PATCH`, `DELETE`, `OPTIONS`.

Allowed headers: `Accept`, `Authorization`, `Content-Type`.
