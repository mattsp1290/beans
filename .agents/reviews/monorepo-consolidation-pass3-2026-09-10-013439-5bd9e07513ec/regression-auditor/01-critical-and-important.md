# Critical and Important

## Critical

None. Nothing on this branch is a correctness, security, build or deploy blocker. Every
executable gate is green and the tree is clean.

---

## Important

### I1 — `apps/bean-counter/deploy/README.md:5` links to a directory the migration removed

```
Full design:
[`../.agents/plans/deploy/`](../.agents/plans/deploy/).
```

`apps/bean-counter/.agents/` does not exist. `01-target-layout-and-module-graph.md`
mandated hoisting bean-counter's plans out of the module and renaming them, and that was
done: the design now lives at `.agents/plans/bean-counter-deploy/` at the repository root.
The link was never updated.

**Executed.** I scanned every relative markdown link in every tracked `.md` file outside
`.agents/` (resolving each target against the file's own directory). This is the *only*
broken one on the branch — which is what makes it worth fixing rather than tolerating.

It also means the sweep commit `94da2fa` ("Sweep stale references to the pre-monorepo
layout") missed it, and neither `2ef178e` nor `5bd9e07` caught it even though `2ef178e`
edited this exact file to add the schema-parity section. This is precisely the "earlier
intent left half-applied" pattern this lane is looking for.

Correct target from `apps/bean-counter/deploy/`: `../../../.agents/plans/bean-counter-deploy/`.

### I2 — `README.md` and `AGENTS.md` claim the root `.dockerignore` covers every image build; it does not

Both files carry this line in their layout diagram:

- `README.md`: `├── .dockerignore                 shared by every image build in the repository`
- `AGENTS.md`: `├── .dockerignore                 shared by every image build in the repo`

Docker reads `.dockerignore` from the **build context root**. The API image's context is
the repository root, so the root file applies. The UI image's context is
`./apps/bean-counter/frontend`, so Docker reads `apps/bean-counter/frontend/.dockerignore`
and the root file is never consulted.

**Executed.** `docker compose config` on both `docker-compose.stack.yml` and
`deploy/docker-compose.prod.yml` resolves the `ui` service context to
`/Users/punk1290/git/beans/apps/bean-counter/frontend`, and that directory has its own
tracked `.dockerignore` (12 entries, including `node_modules` and `dist`). The CI `images`
job likewise builds the UI with `docker build -t bean-counter-ui:ci ./apps/bean-counter/frontend`.

The root `.dockerignore`'s own header already says the opposite of the README:

```
# Not a blanket `frontend` exclusion: the UI image's context is still the
# frontend directory, and a root-level exclusion would be wrong the moment a
# second application ships a UI.
```

The concrete hazard is small but real: someone adds an exclusion to the root file to keep
something out of an image, sees the API image honor it, and assumes the UI image did too.

Suggested wording: `shared by every image built from the repository root (the UI image uses frontend/.dockerignore)`.

### I3 — Two layout acceptance criteria and one success criterion now fail as written, because the plan was never amended

These are plan-document defects, not code defects. The code is right in all three cases;
the acceptance gates that are supposed to certify it are stale, so an implementer or a
later reviewer re-running the checklist gets three spurious failures.

**(a) Layout acceptance criterion 2** — `01-target-layout-and-module-graph.md`: "the
repository root contains exactly these tracked files at depth 1: `.gitignore`, `AGENTS.md`,
`CLAUDE.md`, `LICENSE`, `Makefile`, `README.md`, `go.work`, `go.work.sum`,
`setup-beads.sh`, `setup-multi-repo-beads.sh`."

Actual (executed, `git ls-files | grep -v /`):

```
.dockerignore  .gitignore  AGENTS.md  CLAUDE.md  LICENSE
Makefile  README.md  go.work  setup-beads.sh  setup-multi-repo-beads.sh
```

Two deltas, both correct as implemented:

- `.dockerignore` was added at the root by `a73abfe` when the API build context moved
  there. It has to be at the root — that is where Docker reads it from. The plan's mapping
  table still lists `.dockerignore` under "Kept at `apps/bean-counter/` unchanged", and
  `apps/bean-counter/.dockerignore` no longer exists.
- `go.work.sum` is listed by the plan as "new, tracked" but does not exist. **Executed:**
  `go work sync` at the root produces no `go.work.sum` and leaves the tree clean, so there
  is nothing to track. The plan over-specified.

**(b) Layout acceptance criterion 4** — "No file under `apps/bean-counter/` references a
path beginning `../../..` or an absolute path under `$HOME`, except the deliberately
unexpanded `'$HOME/...'` literals in `scripts/deploy-production.sh`."

Two references fall outside that exception (executed, `git grep`):

- `apps/bean-counter/deploy/docker-compose.prod.yml:37` — `context: ../../..`. Correct:
  from `apps/bean-counter/deploy/` that is the repository root, which is exactly the
  context the API image needs. Confirmed by rendering — `docker compose -p bean-counter -f
  apps/bean-counter/deploy/docker-compose.prod.yml config` resolves it to
  `/Users/punk1290/git/beans`.
- `apps/bean-counter/deploy/README.md:140` — `BN_DSN_SECRET=$HOME/bean-counter-secrets/bn_dsn`
  inside the rollback snippet. It is a remote-host path in an operator command, the same
  kind of literal the exception was written to permit.

The exception clause predates `a73abfe`, which is what gave the prod compose file a
root-relative build context.

**(c) Success criterion 9** — `00-overview.md`: "`scripts/deploy-production.sh --dry-run`
prints a plan whose compose path, repo directory, and image build contexts all reflect the
monorepo layout."

There is no `scripts/` directory at the repository root. The same plan's own layout mapping
puts the script at `apps/bean-counter/scripts/deploy-production.sh`, and `AGENTS.md`,
`CLAUDE.md` and `deploy/README.md` all invoke it from there. Verified at the real path
(executed, exit 0):

```
  repo-dir:      $HOME/git/beans
  compose:       -p bean-counter -f apps/bean-counter/deploy/docker-compose.prod.yml
  images:        bean-counter-api:prod , bean-counter-ui:prod
```

and both compose build contexts render to the repository root. The criterion is satisfied
in substance; only its path is pre-migration.
