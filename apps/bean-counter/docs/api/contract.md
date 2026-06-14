# bean-counter API contract

Base path: `/api/v1`

This contract describes the JSON API shared by the Go handlers and the Svelte
frontend. It reflects the DTOs in `internal/api/dto` and the `github.com/mattsp1290/beans/store`
surface currently wrapped by `internal/store`.

## Conventions

- Request and response bodies are JSON unless a response is `204 No Content`.
- Timestamps are RFC 3339 strings emitted by Go `time.Time`.
- Issue `state` is a free-form beans `IssueState` string. The active and
  terminal buckets are configured by the server, not hard-coded into the API.
- Issue `priority` is an integer from `0` to `4`; `2` is medium.
- Issue `issue_type` is a beans issue type string. Expected values are
  `bug`, `feature`, `task`, `epic`, and `chore`.
- Response arrays are stable arrays. Empty `labels`, `blocked_by`, `issues`,
  `dependencies`, `nodes`, and `edges` encode as `[]`.
- For issue updates, omitted fields keep their current values. `labels: []`
  clears labels. `labels: null` is invalid once write validation is enabled.
- Dependency direction is `issue_id` is blocked by `blocked_by_id`. Graph edges
  use `source = blocked_by_id` and `target = issue_id`.

## Error Envelope

All handler errors use the central server error response:

```json
{
  "error": "validation_error",
  "message": "validation failed",
  "fields": [
    {"field": "title", "message": "is required"}
  ]
}
```

`fields` is present only for validation failures with field-level detail.

| Error source | Status | `error` |
| --- | ---: | --- |
| Request parsing or malformed request | 400 | `bad_request` |
| Validation failure | 400 | `validation_error` |
| `store.ErrNotFound` | 404 | `not_found` |
| `store.ErrCycle` | 409 | `conflict` |
| `store.ErrDuplicateDep` | 409 | `conflict` |
| `store.ErrConflict` | 409 | `conflict` |
| `store.ErrEmptyDSN` | 500 | `store_configuration_error` |
| `store.ErrUnsupportedDriver` | 500 | `store_configuration_error` |
| Unhandled error | 500 | `internal_error` |

## DTOs

### Issue

```json
{
  "id": "bc-123",
  "identifier": "BC-123",
  "title": "Add issue list",
  "description": "Render issues from beans",
  "priority": 2,
  "issue_type": "feature",
  "state": "open",
  "labels": ["ui"],
  "blocked_by": ["bc-100"],
  "branch_name": "feature/bc-123",
  "url": "https://tracker.example/bc-123",
  "repo": {
    "id": "repo-1",
    "slug": "bean-counter",
    "remote_url": "git@example.com:mattsp1290/bean-counter.git",
    "default_branch": "main",
    "requested_ref": "main",
    "base_ref": "main",
    "work_branch": "feature/bc-123",
    "worktree_subdir": "",
    "clone_strategy": "full",
    "auth_ref": "default",
    "metadata": {"team": "core"}
  },
  "created_at": "2026-06-14T12:00:00Z",
  "updated_at": "2026-06-14T13:00:00Z"
}
```

Optional response fields using `omitempty`: `branch_name`, `url`, `repo`, and
optional fields inside `repo` except `slug`.

### CreateIssueRequest

```json
{
  "title": "Add issue list",
  "description": "Render issues from beans",
  "priority": 2,
  "issue_type": "feature",
  "labels": ["ui"],
  "branch_name": "feature/bc-123",
  "url": "https://tracker.example/bc-123",
  "repo": {
    "repo_slug": "bean-counter",
    "requested_ref": "main",
    "base_ref": "main",
    "work_branch": "feature/bc-123",
    "worktree_subdir": "",
    "metadata": {"team": "core"}
  }
}
```

Handler mapping: `CreateIssueRequest.ToStoreInput(prefix, actor)` maps to
`store.CreateIssueInput`. The handler supplies configured project `prefix` and
configured mutation `actor`.

Required by validation: `title`, `priority`, and `issue_type`. `repo.repo_slug`
is required when `repo` is present.

### UpdateIssueRequest

```json
{
  "title": "Updated title",
  "description": "Updated body",
  "priority": 1,
  "state": "in_progress",
  "labels": [],
  "branch_name": "feature/updated",
  "url": "https://tracker.example/bc-123",
  "repo": {
    "repo_slug": "bean-counter",
    "requested_ref": "main",
    "base_ref": "main",
    "work_branch": "feature/updated",
    "worktree_subdir": "",
    "metadata": {"team": "core"}
  }
}
```

Every field is optional. Omitted pointer fields map to `nil` and leave beans
state unchanged. Non-nil `labels` replaces the full label set.

### CloseIssueRequest

```json
{
  "reason": "completed"
}
```

`reason` is optional. The handler supplies the configured mutation actor and
calls `store.CloseIssue(ctx, id, actor, reason)`.

### Dependency

```json
{
  "issue_id": "bc-123",
  "blocked_by_id": "bc-100"
}
```

`AddDependencyRequest` accepts:

```json
{
  "blocked_by_id": "bc-100"
}
```

### GraphResponse

```json
{
  "nodes": [
    {"id": "bc-100", "title": "Parent", "state": "closed", "priority": 1, "labels": []},
    {"id": "bc-123", "title": "Child", "state": "open", "priority": 2, "labels": ["ui"]}
  ],
  "edges": [
    {"source": "bc-100", "target": "bc-123"}
  ]
}
```

### List Envelopes

Issue list and ready queue responses:

```json
{"issues": []}
```

Dependency list responses:

```json
{"dependencies": []}
```

## Endpoints

### Health

`GET /healthz`

Returns server liveness only.

Success:

- `200 OK`

```json
{"status": "ok"}
```

### List Issues

`GET /issues`

Query parameters:

| Name | Type | Meaning |
| --- | --- | --- |
| `state` | repeated string or comma-separated string | Optional state filter. Empty means all states. |
| `limit` | integer | Optional max rows. `0` or omitted means no limit. |

Handler mapping: `store.ListIssues(ctx, store.ListFilter{Prefix, States, Limit})`.

Success:

- `200 OK`

```json
{"issues": []}
```

### Create Issue

`POST /issues`

Body: `CreateIssueRequest`

Handler mapping: `store.CreateIssue(ctx, dto.ToStoreInput(prefix, actor))`.

Success:

- `201 Created`

Body: `Issue`

Errors:

- `400 validation_error` for invalid required fields, priority, type, labels, URL,
  or repo input.
- `409 conflict` for beans store conflicts.

### Get Issue

`GET /issues/{id}`

Handler mapping: `store.GetIssue(ctx, id)`.

Success:

- `200 OK`

Body: `Issue`

Errors:

- `404 not_found` when the issue ID is unknown.

### Update Issue

`PATCH /issues/{id}`

Body: `UpdateIssueRequest`

Handler mapping: `store.UpdateIssue(ctx, id, dto.ToStoreInput())`.

Success:

- `200 OK`

Body: `Issue`

Errors:

- `400 validation_error` for invalid provided fields.
- `404 not_found` when the issue ID is unknown.
- `409 conflict` for beans store conflicts.

### Close Issue

`POST /issues/{id}/close`

Body: `CloseIssueRequest`

Handler mapping: `store.CloseIssue(ctx, id, actor, reason)`, then return the
current issue from `store.GetIssue(ctx, id)` if the handler needs an updated
`Issue` response.

Success:

- `200 OK`

Body: `Issue`

Errors:

- `404 not_found` when the issue ID is unknown.
- `409 conflict` for beans store conflicts.

### Delete Issue

`DELETE /issues/{id}`

Handler mapping: `store.DeleteIssue(ctx, id)`.

Success:

- `204 No Content`

Errors:

- `404 not_found` when the issue ID is unknown.
- `409 conflict` when beans rejects deletion because of current state or
  dependency constraints.

### List Dependencies

`GET /deps`

Handler mapping: `store.ListDeps(ctx, prefix)`.

Success:

- `200 OK`

```json
{"dependencies": []}
```

### Add Dependency

`POST /issues/{id}/deps`

Body: `AddDependencyRequest`

`id` is the issue that is blocked. `blocked_by_id` is the issue it depends on.

Handler mapping: `store.AddDep(ctx, id, blocked_by_id)`.

Success:

- `201 Created`

Body: `Dependency`

Errors:

- `400 validation_error` for missing `blocked_by_id` or self-dependency.
- `404 not_found` when either issue ID is unknown.
- `409 conflict` for `store.ErrCycle`, `store.ErrDuplicateDep`, or other beans
  dependency conflicts.

### Remove Dependency

`DELETE /issues/{id}/deps/{blocked_by_id}`

Handler mapping: `store.RemoveDep(ctx, id, blocked_by_id)`.

Success:

- `204 No Content`

Errors:

- `404 not_found` when the dependency or issue ID is unknown.

### Ready Queue

`GET /ready`

Handler mapping:
`adapter.ReadyIssues(ctx)`, which calls
`store.ReadyIssues(ctx, prefix, terminalStates, activeStates)`.

Success:

- `200 OK`

```json
{"issues": []}
```

### Dependency Graph

`GET /graph`

Handler mapping:

1. `store.ListIssues(ctx, store.ListFilter{Prefix: prefix})`
2. `store.ListDeps(ctx, prefix)`
3. `dto.GraphResponseFromStore(issues, deps)`

Success:

- `200 OK`

Body: `GraphResponse`

Errors:

- `500 internal_error` for unexpected list failures.

## CORS

The server allows the configured frontend origin, defaulting to
`http://localhost:5173`, and permits `GET`, `POST`, `PUT`, `PATCH`, `DELETE`,
and `OPTIONS` with `Accept`, `Authorization`, and `Content-Type` headers.
