# beans

A monorepo. The `beans` issue-tracking library and its `bn` CLI live here
alongside the applications built on them.

```text
beans/
├── go.work                       tracked workspace: ./libs/beans, ./apps/bean-counter
├── Makefile                      fan-out only; each module owns its build rules
├── .dockerignore                 shared by every image build in the repository
├── .beads/                       the single issue tracker for the whole repository
├── .agents/plans/                plans and dated records, namespaced by project
├── .github/workflows/            ci-workspace, ci-libs-beans, ci-apps-bean-counter
├── libs/
│   └── beans/                    module github.com/mattsp1290/beans/libs/beans
└── apps/
    └── bean-counter/             module github.com/mattsp1290/beans/apps/bean-counter
```

## Components

| Component | Path | What it is |
| --- | --- | --- |
| beans | [`libs/beans/`](libs/beans/README.md) | The issue-tracking library — a multi-database (PostgreSQL, MySQL, SQLite) GORM store with embedded goose migrations — and the `bn` command-line client. |
| bean-counter | [`apps/bean-counter/`](apps/bean-counter/README.md) | A Go + Fiber v3 JSON API and Svelte UI over the beans store, for trusted local or private networks. |

One more Postgres-backed application will join under `apps/`. The conventions
it will follow are written down in
[`.agents/plans/monorepo-consolidation/07-third-app-slot.md`](.agents/plans/monorepo-consolidation/07-third-app-slot.md).

## Module layout

There is no Go module at the repository root; each component is its own module
under `libs/` or `apps/`. Applications depend on the library through a
filesystem `replace`:

```text
require github.com/mattsp1290/beans/libs/beans v0.0.0
replace github.com/mattsp1290/beans/libs/beans => ../../libs/beans
```

The `replace` is the mechanism: the container build and every `GOWORK=off`
invocation resolve through it. The tracked `go.work` is a convenience for
editors and cross-module work, not a dependency of any build.

Because the library moved to a nested module path, `go get
github.com/mattsp1290/beans` no longer resolves, and the pre-monorepo `v0.1.0`
and `v0.1.1` tags no longer describe a fetchable module. They remain as
historical git tags.

## Build

```bash
make ci               # vet, lint, test, build, tidy-check across every module
make ci-integration   # testcontainers suites; requires a running Docker daemon

make beans TARGET=build          # run one target in libs/beans
make bean-counter TARGET=test    # run one target in apps/bean-counter
```

Each module's own Makefile is authoritative; the root one only fans out. Lint
policy is per module by design — see `CLAUDE.md`.

## Issue tracking

One [beads](https://github.com/mattsp1290/beans) tracker at the repository
root, holding both projects' issues under their original `beans-` and
`bean-counter-` prefixes.

```bash
bd ready              # available work
bd show <id>          # issue detail
```

## Agents

`AGENTS.md` and `CLAUDE.md` at the root are the instructions for AI coding
agents. There is exactly one of each; applications add a section rather than a
file.

## License

MIT. See [LICENSE](LICENSE).
