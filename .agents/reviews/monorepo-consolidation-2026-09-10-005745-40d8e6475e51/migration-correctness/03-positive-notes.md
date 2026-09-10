# Positive Notes

## P1. The `ListFilter` adaptation is exactly behavior-preserving — verified, not assumed

This was the single highest-risk item in the branch and it is right. The pinned library
at `e52dce57b52c` scoped unconditionally:

```go
// store/store.go @ e52dce57b52c
func (s *Store) ReadyIssues(ctx context.Context, prefix string, ...) ([]Issue, error) {
	q := db.WithContext(ctx).
		Where("prefix = ?", prefix).
		Where("state IN ?", active).
```

`libs/beans/store/store.go` at HEAD gates the same clause on `AllRepos`:

```go
// libs/beans/store/store.go:525-529
	q := db.WithContext(ctx).
		Where("state IN ?", active)
	if !f.AllRepos {
		q = q.Where("prefix = ?", f.Prefix)
	}
```

`ListFilter{Prefix: p}` leaves `AllRepos` at its zero value `false`, so the clause still
applies with the identical bound value. Everything downstream of that clause — the
`issue_type <> 'epic'` exclusion, the `NOT EXISTS` blocker subqueries in both the empty-
and non-empty-terminal-set branches, and the `priority ASC, created_at ASC` ordering — is
byte-identical between the two versions. `ListBlockingDeps` has the same shape
(`libs/beans/store/store.go:1000-1004` vs. the old single `Where("i.prefix = ? AND d.dep_type = ?", …)`),
and the reordering into two chained `Where` calls composes to the same `AND`.

The two fields that could have changed meaning silently are handled by the library's own
contract rather than by luck: `ReadyIssues`' doc comment at `libs/beans/store/store.go:502-504`
states outright that "Only `f.Prefix` and `f.AllRepos` are consulted; `f.States` and
`f.Limit` are ignored", so the zero values the migration leaves in those fields cannot
change the result set. All three adapted call sites
(`internal/store/adapter.go:140`, `internal/handlers/deps/deps.go:40`,
`internal/handlers/graph/graph.go:38`) use the same keyed-literal form, and it matches how
`graph.go:34` already called `ListIssues` before the migration — so the change makes the
file internally consistent rather than introducing a second idiom.

I also diffed the full exported surface of `store.go` between the pinned commit and HEAD
to confirm no *other* signature bean-counter touches drifted. Only `ReadyIssues`,
`ListBlockingDeps`, `ListDeps`, `ListMembers` and `ListParents` changed shape, and
bean-counter calls none of the last three. `CreateIssueInput`, `IssueRepoInput` gained
fields (`ParentID`, `RemoteURL`, `CreationCommit`) — additive only, and every
bean-counter construction site uses keyed literals, so nothing was silently repositioned.

## P2. `go.sum` correctly drops the hashes for the replaced module

`apps/bean-counter/go.sum` no longer carries any `github.com/mattsp1290/…` line
(`grep -n mattsp1290 apps/bean-counter/go.sum` returns nothing). That is the correct
outcome for a filesystem `replace` — a stale `h1:`/`go.mod` hash pair for the old pinned
version left behind would be dead weight that a future `tidy` would strip, producing
mystery churn. `git show 14da22e -- apps/bean-counter/go.sum` shows exactly the two lines
removed and nothing else touched.

## P3. Both `tidy` and `go work sync` are genuinely idempotent

I ran `go work sync` at the root, then `GOWORK=off go mod tidy` in each module, and
`git status --porcelain` came back empty after each. This matters because
`ci-workspace.yml` gates on it and both module Makefiles' `tidy-check` gates on it — three
independent CI jobs would fail on any drift. The `GOWORK=off` choice in both `tidy-check`
targets is the right one and the comment explains why in one sentence:

```make
# GOWORK=off is deliberate: a workspace-active tidy can resolve through a
# sibling module and write a go.sum that is incomplete for a standalone
# build, which is exactly how the container builds this module.
```

The `go work sync` bumps it wrote into `libs/beans/go.mod` (`klauspost/compress`
1.18.5→1.18.6, `mattn/go-isatty` 0.0.21→0.0.22, `x/crypto` 0.50→0.51, `x/sys` 0.43→0.44,
`x/text` 0.36→0.37) are all indirect, all upgrades, and all survive a standalone
`GOWORK=off go mod tidy` without being downgraded — which is what makes the pair stable.

## P4. All six `deps.go` build-tag pins survived the rename and the tidy

`libs/beans/deps.go` carries `//go:build tools` imports for `glebarez/sqlite`,
`testcontainers-go/modules/mysql`, `gorm.io/datatypes`, `gorm.io/driver/mysql`,
`gorm.io/driver/postgres` and `gorm.io/gorm`. All six remain **direct** requires in
`libs/beans/go.mod` after the rename and tidy. This is the classic thing a module move
loses — a tools file whose package clause or build tag no longer matches, silently
demoting six deps to indirect and letting a future tidy drop them.

## P5. The Dockerfile copies the whole library tree because of `go:embed`, and says so

```dockerfile
# The whole library is copied, not just model/repo/store: libs/beans/schema
# embeds schema/migrations/*/*.sql via go:embed, and a partial copy breaks the
# embed at build time rather than at runtime.
COPY libs/beans ./libs/beans
```

`libs/beans/schema/schema.go:18` is `//go:embed migrations/*/*.sql` — a relative pattern,
so it survives the directory move unchanged, and all three dialect directories
(`mysql`, `postgres`, `sqlite`) are present under it. The dependency layer is still split
correctly: only the two `go.mod`/`go.sum` pairs are copied before `go mod download`,
because module-graph loading reads the replace target's `go.mod` but not its source.

`ENV GOWORK=off` in the build stage with `go.work` deliberately *not* copied is the right
call — it makes the image provably resolve through the `replace` alone, which is also
exactly what the two `GOWORK: 'off'` CI jobs prove.

## P6. The compose build contexts were re-derived per file depth, not copy-pasted

The two compose files sit at different depths and got different answers:

- `apps/bean-counter/docker-compose.stack.yml`: `context: ../..` → repository root
- `apps/bean-counter/deploy/docker-compose.prod.yml`: `context: ../../..` → repository root

and the prod file's UI context was corrected from `./frontend` (which had been resolving
to the never-existent `apps/bean-counter/deploy/frontend`) to `../frontend`. `a73abfe`'s
message says this was found by rendering with `docker compose config` rather than by
reading — that is the right method for relative-context bugs, which are invisible on the
page.

## P7. The deploy script's replace gate was inverted rather than deleted

The old gate rejected *any* beans `replace`; under the monorepo the replace is mandatory,
so the naive fix is to delete the check. Instead `check_sanctioned_replace`
(`apps/bean-counter/scripts/deploy-production.sh:227-247`) keeps the half that still has
value — a replace pointed anywhere else must stop the deploy — and the comment states
explicitly which half the target-SHA pin now covers instead. It also closes the obvious
bypass:

```bash
  # Block form (`replace (` ... `)`) would hide extra entries from the
  # single-line scan below, so it is rejected outright.
  if grep -qE '^[[:space:]]*replace[[:space:]]*\(' "$gomod"; then
```

and `test/scripts/deploy-production_test.sh:88-131` covers all six branches including
`check_sanctioned_replace "$SCRIPT_DIR/go.mod"` — asserting the *real* file passes, so the
gate cannot rot away from the module it guards.

## P8. `goimports` local-prefixes was updated with the module path

`libs/beans/.golangci.yml`:

```yaml
    goimports:
      local-prefixes:
        - github.com/mattsp1290/beans/libs/beans
```

An easy one to miss — a stale prefix produces no error, just silently mis-grouped import
blocks that drift for months. Note also that the import rewrite was correctly *not*
applied to genuine references to the GitHub repository (the `.beads` Dolt remote
`git+ssh://git@github.com/mattsp1290/beans.git`, and the `.agents/plans/` historical
record), which is exactly what the trailing-slash guard on the `sed` was for. `grep` for
`mattsp1290/beans/{model,store,repo,schema,version}` and for `mattsp1290/bean-counter`
across all `*.go` returns nothing.
