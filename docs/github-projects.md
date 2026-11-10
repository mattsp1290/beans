# Managed GitHub project contract

Managed projects retain Beans' stable `projects/<project_id>/` namespace.
The authored root `name` is editable display text. Renaming never changes the
namespace, issue prefix, note paths, or historical references.

## CLI boundary

`bn project apply --request-file FILE --json` accepts one JSON request.
`FILE` may be `-` for stdin. The limit is 1 MiB. Unknown fields fail.
The command does not infer a project from the working directory or environment.

```json
{
  "schema_version": 1,
  "project_id": "project-stable",
  "operation_id": "create-123",
  "expected_revision": null,
  "change": {
    "type": "create",
    "name": null,
    "repositories": [
      {"id":"123","full_name":"owner/first","html_url":"https://github.com/owner/first"}
    ]
  }
}
```

The caller persists `project_id`, `operation_id`, and the unchanged request
before invoking Beans. Operation identity is scoped to the stable project ID.
`project_id` uses lowercase letters, digits, and hyphens, at most 100 bytes.
`operation_id` uses ASCII letters, digits, hyphens, and underscores, at most 128 bytes.
Display names contain 1 to 200 printable UTF-8 bytes with nonblank content.

These tagged changes are supported.

| `change` | Behavior |
| --- | --- |
| `{"type":"create","name":null,"repositories":[...]}` | Creates a new identity. Null or absent name defaults to the first selected repository's short name. |
| `{"type":"rename","name":"New display name"}` | Updates display text. |
| `{"type":"add_repository","repository":{...}}` | Adds a repository or updates canonical metadata for the same stable GitHub ID. |
| `{"type":"remove_repository","repository_id":"123"}` | Removes that membership. Removing the last repository fails. |
| `{"type":"archive"}` | Tombstones the project from the active catalog. |

Create requires null `expected_revision` and an absent identity. Every other
change requires the observed `revision`. Revisions are SHA-256 digests of the
complete current config bytes, including retained authored comments.

Repository IDs are positive canonical decimal strings. Each repository includes
`id`, `full_name`, and its exact `https://github.com/<full_name>` URL. Membership
deduplicates by ID and preserves first selection order. Create accepts 1 to 100
selected entries. Membership holds at most 100 repositories. Later membership
changes never silently rename the project. Beans validates stored identity and
shape. Forge checks current GitHub access through its service connection.

The `github_project` field in `beans.toml` stores membership, archive state, and
caller operation receipts. Root `remotes` mirrors managed membership for the
existing resolver. Legacy `project create` and `project link` refuse managed
projects. The root `prefix`, workflow, unowned TOML values, and comments survive
managed edits.

`bn project catalog --json` returns `{"schema_version":1,"projects":[...]}`
for active managed projects. `bn project inspect PROJECT_ID --json` returns
`{"schema_version":1,"project":{...}}`, including archived projects.
These commands read the specified local hub without automatic refresh or
registration. They do not certify remote publication. A snapshot has `id`,
`name`, `revision`, `archived`, and `repositories`.

## Retry and publication

The operation receipt and authored effect share one atomic config replacement
inside the existing locked Git mutation. Same operation identity and same
canonical request produce a no-op, even if the original expected revision is
now old. Reusing that identity with different instructions fails. Receipts are
retained permanently within this contract.

Success returns `schema_version`, `operation_id`, `replayed`, `project`,
`commit`, `sha`, `pushed`, and `message`. `commit` equals `sha`. A repeated
operation returns the current snapshot, which can include later changes.
A receipt no-op returns empty commit fields. The existing Git boundary still
publishes outstanding local commits before reporting `pushed=true`.

A failed publication exits 1, emits no success JSON, and retains local history
and its receipt. The remote outcome can be unknown. Retrying the same request
uses the retained receipt and normal publication path. `bn sync` also reconciles
publication after its cause is resolved. `--no-sync` reports `pushed=false` and
retains its documented warning. A local receipt or read is not proof that
another device can observe the mutation.

## Archive boundary

Archive preserves project membership, note files, approvals, conversations,
GitHub repositories, and checkouts. Managed project edits refuse an archived
identity. Explicit historical reads remain available. Archive does not stop
workers or authorize deletion, purge, restore, merge, or deployment.

Before project execution is enabled, Forge Ready integration must own the
execution-state check and serialize archive with execution admission. It must
refuse archive while execution is active or termination is uncertain. The
current independent project/chat candidate admits no project execution.
Beans has no worker termination authority. Direct CLI archive records authored
state only and does not establish that workers have stopped.
