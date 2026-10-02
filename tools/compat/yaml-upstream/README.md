# Upstream YAML production-reader census

Run from the repository root with Go 1.25.7 and locked Rust 1.98.1:

```
python3 tools/compat/yaml_upstream_reference.py --check tests/contract/yaml-upstream.json
python3 tools/compat/yaml_upstream_report.py
cargo test --locked --test domain yaml_upstream
```

The report command exits nonzero for every difference; the first census slice
intentionally leaves production mismatches for beans-htch. Its dynamic Rust test
is an observation executable, not a claim of parity. The standalone anchored
edit regression always runs without environment variables. Full reports live in
`.compat/yaml-upstream-report.json`; stdout includes counts and concrete examples.

`extract.go.txt` parses the pinned dependency's Go AST. It walks the complete
`unmarshalTests` and `nodeTests` composite literals in source order and evaluates
only string literals, concatenation, `strings.Repeat`, and integer multiplication.
Unsupported expressions fail capture. It records whole original target/input
expressions, source location, table/index identity, source and license hashes,
and raw byte arrays. Targets are provenance, never arbitrary Go type conformance.
There are 172 unmarshal rows and 74 node rows, with ten node `[encode]` rows
accounted separately using the upstream test's explicit decode=false annotation.
No mismatch causes exclusions. The 64 other node rows include `[decode]` cases.
Their upstream encoding expectations are not applied to Beans's lossless no-op.

The Python generator retains original upstream bytes separately from each
projection's exact input bytes and recipe. Each decode row enters all seven real
Beans readers. Notes and manifests receive raw frontmatter and an indented
unknown-field envelope; the manifest rejects unknown keys according to its
production policy. Owned scalar/list/timestamp/numeric/link fields receive the
original byte fragment, removing only an exact leading `v: ` or `a: ` prefix
when present. This lexical rule is independent of parse success and does not
strip tags, anchors, comments, malformed text, or trailing documents. Workflow
projections indent continuation lines under owned keys. Graph projections use
actual bn-change-graph fences. The deterministic envelope contains real required
fields, fixed timestamps, and a complete manifest summary. There are 9,205
observations including the first anchored issue edit/readback demo.

Canonical models encode every textual value as raw byte arrays, normalize only
nil collections where the Rust production model has Vec rather than Option,
and preserve optional workflow/graph collections. Workflow keys use ordered
byte-key entries. YamlString values use as_bytes, not display serialization.
Unknown fields retain ordered key/node trees, node kind, line, scalar spelling,
null semantics, tag and anchor/alias spelling. Alias targets remain references
rather than expanded trees; no cycle is traversed. Styles/comments not exposed
by Rust node models are covered by exact retained/no-op input bytes, rather than
an invented upstream node-observation conformance requirement.

Parse and encode diagnostic bytes are separate, retaining complete error order;
successful note/manifest output is reread through production readers and
re-encoded. Graph's Rust production API takes UTF-8 `&str` text even in
`parse_graph_bytes` (only the path argument is bytes). Invalid UTF-8 graph inputs
are retained as explicit API-boundary failures rather than silently replaced,
reinterpreted, or excluded. This boundary is part of the reported compatibility
work for the dependent production slice.

The report checks complete observation identity sets, regenerates all projection
bytes, validates independent row cardinalities/indices, and rejects changed
expectations and removed-row negative controls. Independent immutable Go
recapture uses git archive of 718726a580c19becd5fb57513be9e76fda40ea26, temporary
package-local test harnesses (including private strict workflow decoding), and
never production fallback calls from Rust. No prior contract corpus is modified.

The upstream Canonical copyright notice and full Apache 2.0/MIT dependency
license are embedded in the corpus alongside all copied fixture expressions.
The source SHA and corpus/report hashes make observations independently auditable.
The initial local Rust build reused CARGO_TARGET_DIR=/home/punk1290/git/beans/target;
Cargo's source fingerprints rebuilt beans and beans-kernel for this worktree.
