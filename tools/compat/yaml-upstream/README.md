# Upstream YAML production-reader census

Run from the repository root with Go 1.25.7 and locked Rust 1.98.1:

```
python3 tools/compat/yaml_upstream_reference.py --check tests/contract/yaml-upstream.json
python3 tools/compat/yaml_upstream_report.py
cargo test --locked --test domain yaml_upstream
```

The report command exits nonzero for every difference. Its dynamic Rust test
is an observation executable; comparison uses independently captured Go results.
The anchored edit and shrunk regression tests always run without environment variables. Full reports live in
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
original observations including the first anchored issue edit/readback demo.
These cases and their expectations remain byte-for-byte unchanged. Supplemental
regressions and deterministic status/description edits bring the report to 12,534
observations. Every issue projection gets both edits, including honest initial
parse failures. The edit artifacts separate initial model/no-op, encoded edit
bytes, encode errors and full production reread. Each successfully parsed edit
records the intended owned byte range, unknown-field ranges, and exact untouched
source/destination prefix and suffix ranges, asserted independently in Go and
Rust. The status-anchor control retains Go's historical invalid edit (the encoder
drops the status anchor and the unknown alias fails reread); it is counted
separately from valid edits. The description control retains an existing body,
Log section and tail.

Canonical models encode every textual value as raw byte arrays, normalize only
nil collections where the Rust production model has Vec rather than Option,
and preserve optional workflow/graph collections. Workflow keys use ordered
byte-key entries. YamlString values use as_bytes, not display serialization.
Unknown fields retain ordered key/node trees, node kind, line, scalar spelling,
null semantics, tag and anchor/alias spelling. Alias targets remain references
rather than expanded trees; no cycle is traversed. Styles/comments not exposed
by Rust node models are covered by exact retained/no-op input bytes, rather than
an invented upstream node-observation conformance requirement. The report also
retains 88 immutable-Go manifest normalization outcomes (encoded no-op bytes
differ from the input); these remain exact Go/Rust parity cases, not claims of
byte-identical manifest encoding. All successful untouched note outputs and
initial issue edit controls retain byte-identical input.

Parse and encode diagnostic bytes are separate, retaining complete error order;
successful note/manifest output is reread through production readers and
re-encoded. The production `parse_graph_raw` API reads raw graph text while
retaining the existing `parse_graph` and `parse_graph_bytes` APIs. Raw graph and
workflow readers select UTF-16 only through a YAML BOM and check invalid raw
UTF-8 through the shared reader. Syntax diagnostics use native scanner marks
and yaml.v3's required-key opening marks; no Go fallback is present.


The report checks complete observation identity sets, regenerates all projection
bytes, validates independent row cardinalities/indices, and rejects changed
expectations and removed-row negative controls. It also verifies every original
9,205 case/input/expectation against the reviewed census commit
7d43671f5327aae08ec509e852bab092fe9b512c. Independent immutable Go
recapture uses git archive of 718726a580c19becd5fb57513be9e76fda40ea26, temporary
package-local test harnesses (including private strict workflow decoding), and
never production fallback calls from Rust. No prior contract corpus is modified.

The upstream Canonical copyright notice and full Apache 2.0/MIT dependency
license are embedded in the corpus alongside all copied fixture expressions.
The source SHA and corpus/report hashes make observations independently auditable.
The initial local Rust build reused CARGO_TARGET_DIR=/home/punk1290/git/beans/target;
Cargo's source fingerprints rebuilt beans and beans-kernel for this worktree.
