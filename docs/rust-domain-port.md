# Rust domain port

WP3 is in progress. The current parser/editing primitives are implemented in
`src/domain/frontmatter.rs`, with YAML event and source-position adaptation in
`src/domain/yaml.rs`. Issue reading and body mutation semantics are in
`src/domain/issue.rs`; stored log rules are in `src/domain/log.rs`. Existing-issue encoding is in `src/domain/issue_encode.rs`, with owned YAML
presentation in `src/domain/yaml_render.rs` and source-provenance planning in
`src/domain/splicing.rs`. New-file construction uses the same owned-field renderer. New unknown Extra
construction, other note codecs, plans, configuration, resolution, indexing and
queries are covered by the later sections below. Wider WP3 qualification remains
in progress. Go is still the default implementation; all original Go tests
remain in place.

The parser retains original bytes and a separate Unicode view, deriving absolute
frontmatter ranges from line offsets. Semantic YAML nodes are separate from retained text; duplicate
unknown keys, comments, block styles and Markdown are not serialized again.
Owned scalar/list decoding preserves yaml.v3's scalar spellings and null rules;
aliases are separate nodes and fail scalar/list coercion. The typed issue reader
checks owned duplicates and required keys, decodes
metadata, derives Linux project/archive paths, and retains original bytes and
body sections. It preserves Go's permissive issue values, signed 64-bit priority
bounds, and timestamp semantics including one-digit hours, comma fractions and
zone hour 24. The corpus adds 33 generated metadata/validation cases.

The pinned yaml-rust2 low-level event API passed the prototype gate across all
21 original issue/request roundtrip inputs plus 16 added syntax inputs. Node
positions need adaptation: nonempty block scalars report content positions,
anchored/tagged nodes can report positions after their prefixes, and implicit
empty values can report the next key's line. The adapter reconstructs these
positions from scanner tokens and original lines. It never treats a marker's
index as an authoritative byte offset. The additional inputs include Unicode,
anchors, tags, null coercion, nested/flow maps, block-header variants and
structural errors and body-fence variants. `src/domain/text.rs` ports literal
link parsing/creation and the distinct issue/request body splits. Closed fences
hide headings; unterminated fences become inert; four-space fences stay inert.
Raw log sections and trailing sections remain separate byte slices. Thirteen
Go-derived link cases preserve bare IDs and original link spelling.

`tests/contract/frontmatter-primitives.json` records nodes, original body and
YAML text, byte ranges, body sections, links, issue metadata, log entries, body
mutations and errors from the immutable Go revision
`718726a580c19becd5fb57513be9e76fda40ea26`. Capture uses a development-only Go
test injected into an archived tree. `make compat-frontmatter` independently
recreates that record and compares it without changing the committed corpus.
Rust is never a source for these expected results.

The actual replacement path constructs spans from parsed fields, calls
`valid_splices`, obtains unchanged intervals through `preserved_interval`, and
computes destination ends through `translate_offset`. It copies original bytes
directly and returns copy ranges for translation/checking. Invalid or duplicate
replacement requests return errors while leaving the original untouched.
`cargo test --test domain --locked` compares the entire primitive corpus and
checks actual non-ASCII edits and source/destination copy geometry.

Log parsing retains opaque list entries and indented continuations. The parser
uses Go's ASCII regex whitespace rules, while formatting normalizes Unicode
whitespace in actor/repo/branch tokens. Timestamp formatting uses UTC whole
seconds. The corpus captures 28 standalone parses, 10 format/line cases and
7 section parses/appends, alongside all valid issue files' parsed logs.
The time dependency enables extended dates so valid Go inputs that cross into
UTC year 10000 format correctly. Description replacements match Go's
trailing-newline behavior. AppendLog
compares instants including nanoseconds and sets a later Updated to UTC. Body
rendering appends only entries beyond the original log length, preserving the
original section even if an existing semantic log entry is edited. The 270 differential
body mutations include earlier/equal/later appends and opaque original edits.
`IssueDocument::encode` now renders full documents, including changed timestamps
and appended logs, using the real verified byte-copy path. CLI and filesystem
writes still await the later operation/CLI packages.

The fixed-Go corpus compares 532 complete owned-field edits across 28 valid
issue inputs: every owned field, optional removals and combined updates. It also
compares all 270 body mutations as complete documents and 93 scalar inputs in
plain/commented/flow/link presentations. Known fields are rendered independently;
unknown YAML is never serialized. Untouched metadata, including missing aliases,
stays untouched. Rendering aliases adds the current ID only when aliases are
changed. Changed timestamp equality ignores offset spelling but includes
nanoseconds; rendered timestamps use UTC whole seconds.

Go's line edits have coincident spans for flow-root mappings. The planner replays
the same descending line order, retaining each original line's byte provenance,
then derives ordered nonoverlapping byte patches for the kernel. The flow-root
fixture captures these historical quirks, including edits that leave duplicate
owned keys and thus do not reparse. Do not use output-byte parity as evidence
that every such generated document is valid. Closing-fence-at-EOF normalization
also remains an explicit compatibility question. Both need resolution before
complete codec acceptance.

A second 512-case property exercises the typed encoder directly, proving no-op
byte equality and preservation of every byte outside a changed status field,
including Unicode, comments, duplicate unknown keys and arbitrary bodies.

Remaining codec gates include other note schemas, complete yaml.v3 syntax-error
presentation and obscure Unicode error quoting, further YAML/comment variants,
new-file Extra handling and expanded cross-language parse/encode/edit validity. The real
issue encoder now reaches the verified helpers using parsed byte ranges; every
other typed codec must do so before WP3's whole-package proof-coupling gate is
accepted. Primitive parity
alone does not establish issue/request or whole-hub compatibility.


`IssueDocument::new` constructs an issue without original text or parsed log
history. Encoding writes owned fields in canonical order, applies aliases and
optional-field rules, emits UTC whole-second timestamps, and adds authored
body/log text. `frontmatter()` returns None for a new issue. Parsed documents
retain the original byte-preserving encoder path.

The new-file corpus has 128 Go-derived cases covering default/minimal/canonical
metadata, all 93 string presentations in titles, list/alias variations,
description/body/log separators and timestamp boundaries. Rust encoding equals
Go byte-for-byte, and the Rust reader matches Go's metadata, body and log views.
`make compat-domain-read` exports actual Rust-generated files from the tests and
reads those bytes with the archived fixed Go reader. Linux CI requires this
check; failure artifacts retain the files and reader results. The gate proves
124 accepted reads and four Go-matching timestamp boundary rejections, not
unconditional validity of every possible programmer-supplied timestamp.

The cross-read tests exposed YAML-version differences for raw Unicode quoted
line separators. The adapter locates the original quoted lexeme, trims adjacent
indentation around raw NEL/LS/PS and decodes that adapted lexeme independently.
Escaped \N/\L/\P keep their authored spaces. Diagnostic line accounting counts
raw Unicode separators separately from physical LF byte offsets. More Unicode,
quote/escape interactions and byte-span behavior still need differential cases
before broad vault use; this gate does not establish complete YAML compatibility.

## Markdown link extraction

`src/markdown/links.rs` provides raw-byte body-link extraction for the vault
loader. It preserves target, fragment and alias bytes, including invalid UTF-8,
last-`#` fragment splitting, embed flags and first-occurrence deduplication by
(target, fragment, embed). Frontmatter is excluded using Goldmark's delimiter
rules, including TOML and one initial blank line. Alias markup and entities
remain authored bytes. Ordinary Markdown link/image labels may contain
wikilinks; their destinations, angle autolinks, code and HTML do not. Table
cells split on unescaped pipes before parsing inline links.

Comrak supplies block/inline context, with raw-byte grammar and temporary
markers adapting its different wikilink rules. The context view masks lone CR
without altering returned fields because Goldmark's reader splits on LF. The
adapter never writes caller bytes. This is a trusted parser boundary, not a
verified kernel. It parses context per candidate; full-index load measurements
and pathological link-density measurements remain required before WP3
acceptance.

`make compat-markdown-links` independently recaptures 1,495 cases from the
immutable Go revision under Go 1.25.7. The corpus covers every byte in target and
alias positions, GFM tables, code/fences, HTML, ordinary links/images and
references, Unicode prefixes, escapes, CR/CRLF and frontmatter delimiters. Two
independent Rust assertions port `TestLinks` and `TestLinksAlias`. These tests
qualify link extraction for the recorded inputs; they do not qualify rendered
HTML, TOC, embeds, the disk index or end-to-end queries.

## Forgiving document metadata

`src/vault/document.rs` ports document frontmatter splitting, title fallback and
tag filtering. Its exact leading `---\n` rule differs from Markdown link
frontmatter and typed issue parsing; a missing closing fence keeps the original
body. A title must be a decoded string. Otherwise the first trimmed `# ` line
(including one inside a code fence) supplies the title, then the basename. Tags
accept a nonempty string or only string items from a sequence. Empty sequences
and absent/empty scalar tags retain their distinct non-nil/nil results.

`src/domain/yaml_value.rs` and its scalar module decode the entire generic
frontmatter map for indexing and later API use. Values retain raw strings,
booleans, signed/unsigned integers, float bits, timestamps, sequences and both
string-keyed and general maps. Aliases and merge precedence use YAML node
identities; duplicate mappings are skipped as recoverable errors, while fatal
value errors retain earlier top-level assignments. The decoder carries Go's
alias expansion counters and ratio limits. It does not serialize or rewrite
source YAML. Generic metadata is distinct from the typed codecs' permissive
scalar-to-string fields.

`make compat-doc-metadata` independently recaptures 995 immutable Go 1.25.7
cases. It compares original split bytes, body/title/tags and all typed metadata
values, including partially decoded results. Cases cover every byte in titles
and bodies, syntax/type errors, tags, aliases and alias keys, merges, duplicate
keys, BOM/Unicode, numeric boundaries, timestamps, and expanding alias trees.
An independent Rust regression checks code-fence heading fallback, mixed tag
types, fatal partial-map retention and CRLF opening-fence behavior. Whole-index
loading/reload/search, API JSON errors for generic maps/non-finite floats, and
wider YAML/reader/error-order qualification remain acceptance work. No existing
whole-index Go test has been retired or marked ported by this metadata slice.

## Disk index loading and reload

`src/vault/index.rs` and its note/walk/reload modules provide the production
filesystem index. Configuration and workflow load before traversal. Project
config discovery uses Linux Go-style globbing, including hidden and symlink
project directories; the sorted note walk skips hidden directories and
templates, handles assets without reading them, and distinguishes non-directory
plan roots from Markdown files. It invokes actual plan tree/temp recovery and
loads bundles with the existing size/path/section boundaries. Notes use the
production issue/request/memory/handoff/plan decoders, doc metadata and Markdown
link extraction, then rebuild the existing ownership/alias/backlink graph.

Incremental reload removes only requested note paths and appends successful
reparses in Go order. Invalid section edits retain the last valid aggregate;
deleted plan roots remove it. Config reload builds a fresh index and only
replaces the previous index after success. Parse/link warnings retain their
distinct ordering and path keys. Plan recovery can still affect disk before a
later load failure, as in Go. Mutable Rust borrowing supplies exclusive reload
access; shared server lock/watcher wiring remains WP7 work.

Original file bytes and extracted body/description/link bytes remain separate
from a read-only UTF-8 parser view with offset translation. Typed NoteData
now uses canonical byte bodies and original-byte frontmatter storage through the
shared codecs, as qualified in the raw-byte slice below. Wider raw-reader malformed-UTF-8/error-order and filesystem-fault
qualification remain mandatory; this adapter is not a lossless-codec waiver.

`make compat-index` recaptures 26 disposable filesystem scenarios (32 load/reload
stages) and 216 byte/Unicode filepath matching cases from Go 1.25.7. Snapshots
compare note order, metadata/body fields, raw/resolved links, ownership, aliases,
backlinks, configuration/workflow, warnings and exact filesystem effects. Eight
original index regressions have independent assertions, bringing the ledger to
82 ported and 143 pending at that checkpoint. These index tests do not
establish whole-vault parity.

`tools/compat/index_benchmark.py` and `examples/index_load.rs` measure the actual
Go and release Rust loaders on the same WP1 fixture (5,000 live issues plus one
archived issue). On Linux aarch64, three warmups and twenty measured samples
gave Go median 312.13 ms and Rust median 140.74 ms
(ratio 0.451). Counts are checked in both implementations. The full
samples, compiler pins, fixture and binary hashes are recorded in
`tests/contract/index-performance-linux-arm64.json`. This is in-process load
evidence, excluding CLI startup/output; end-to-end command measurements and
pathological link-density qualification remain separate gates.

## Vault queries and derived plan execution

`src/vault/query.rs` and its dependency/search/cycle/execution modules query the
actual disk index. Lists preserve project/archive filters and priority/time/ID
ordering, using timestamp instants rather than authored UTC offsets. Requests
retain their fixed workflow and field filters. Search reads original issue
body bytes, all plan sections and summaries, applies Go Unicode simple lowercase,
preserves stable equal-score/basename ties, and excludes archived handoffs by
default. Unknown kind filters match nothing. Iterative Tarjan traversal reports
sorted dependency SCCs and self-loops without using the native call stack.

Readiness and execution blockers resolve exact issue IDs before generic note
lookup. Blocked lists, parents, children and dependency graph edges retain Go's
separate generic lookup behavior, including colliding document basenames.
Blocker terminality uses the blocker's project workflow. Archived children can
hold epics, while archived handoffs only affect search/backlink visibility.
Issue/memory project derivation now uses original Linux filename bytes and the
last `projects` component, preserving workflow lookup for invalid-UTF-8 paths.

Plan execution derives each authored graph node's binding and work state from
live indexed issues. Terminal status precedes blockers, blockers precede archive
and epic holds, and literal `in_progress` precedes ordinary active/hold workflow
classification. Aggregate counts, distinct issue counts, execution precedence
and lifecycle mismatch flags are derived without changing the plan. Report JSON
retains omitted absent issues and Go's `null`/`[]` blocker distinctions.

`make compat-query` recaptures 39 real filesystem scenarios, including three
incremental reloads, from immutable Go 1.25.7. Queries cover all six note kinds,
custom workflows, unknown kinds, invalid body/path bytes, ID/basename/alias
collisions, duplicate ownership, sorting and offsets, unresolved/duplicate
blockers, cross-project cycles, archive opt-in and plan execution states. Fixture
preconditions assert that requests and plans actually load. Query identity lists
normalize nil-empty slices to arrays for this internal semantic comparison;
CLI/API transport nilness still requires their route/command gates. Execution
reports compare complete Go JSON directly. Fourteen independent original
regressions bring the ledger to 96 ported and 129 pending. This slice does not
qualify all vault filesystem faults, YAML reader/error ordering, watcher/server
concurrency, CLI/API query transport or the wider WP3 lossless mutation gates.

## Original-byte codec bodies

All four issue/request/memory/handoff documents now expose `parse_bytes` and
`original_bytes`. UTF-8 convenience readers delegate to the same path. The
shared Frontmatter owns original bytes alongside a separate Unicode parser
view; invalid body bytes map one-for-one to replacement runes with translated
slice boundaries. Valid UTF-8 inputs use direct byte offsets. Bodies and issue
descriptions use the existing byte-preserving YamlString value, so an authored
replacement rune remains distinguishable from an original invalid byte.

Encoders splice actual original bytes and canonical body bytes. Issue/request
byte renderers preserve original raw log and tail fragments, append new entries
without reserializing old logs, and retain surrounding blank lines. Byte-based
description/request-body setters follow existing newline normalization. The
older String render helpers are UTF-8 conveniences and return an error for a
non-UTF-8 result; writers use byte renderers. Memory/handoff whole-body writers
accept arbitrary byte values. Real index loading calls these same codecs and
reads their canonical body fields, replacing the earlier index-only adaptation.

`make compat-raw-codec` recaptures 1,056 immutable Go 1.25.7 inputs and 5,280
parse/encode outcomes: every byte in descriptions, bodies, logs, continuations
and tails, truncated/overlong encodings, closed and unterminated fences, absent
final newlines and CRLF rejection. No-op, metadata edits, UTF-8 and raw-byte body
replacements, and log append results compare complete output bytes and errors.
Generated raw no-op/metadata properties assert exact preservation and copy
geometry. `make compat-raw-codec-read` feeds 5,260 actual Rust outputs to the
fixed Go reader and encoder, requiring acceptance and exact byte roundtrip. A
real disk-index integration test encodes all four loaded payloads
back to the original file bytes and checks that replacing an invalid byte with
an authored replacement rune changes the file. All existing codec/index/query
corpora remain required; this does not qualify wider malformed-frontmatter
reader windows/error ordering, YAML syntax/aliases/coercion, new log argument
byte handling, every generated edit sequence, filesystem faults or the full WP3
production proof-coupling audit. No additional original Go regression is marked
ported solely by this new corpus (ledger remains 96 ported /129 pending).

The same 5,000-live-plus-one-archive fixture was remeasured after this change:
three warmups and twenty release samples on Linux aarch64 gave Go median
314.68 ms and Rust 137.39 ms (ratio 0.437). Full samples, toolchains and actual
fixture/binary hashes are in
`tests/contract/index-performance-raw-codec-linux-arm64.json`. This remains an
in-process load measurement; CLI startup/output and dense-link measurements
still require separate qualification.

## Raw UTF-8 YAML reader windows

The production YAML reader now validates original UTF-8 bytes as the parser
requests its 512-byte windows, checking trailing octets, minimum sequence length,
Unicode range and printability in Go order. Frontmatter no longer prechecks the
entire file. The parser follows Go's two-token comment lookahead for node-content
errors: a following key can satisfy lookahead before its value is read, while a
comment-only suffix requires scanning to its end. Explicit document-end behavior
continues to limit which bytes reach the reader. Ignored malformed UTF-8 after
that boundary can remain valid input and must retain its original bytes.

Frontmatter fields now expose actual raw offsets; encoding translates these
back to Unicode-view offsets only when inspecting inline comments. Frontmatter
and body replacement spans use actual byte ranges, so no-op and first/last
metadata edits preserve or replace the same regions as Go even when ignored
frontmatter suffixes expand in the Unicode view.

`make compat-raw-reader` recaptures 6,304 fixed-Go cases and 18,912 parse/edit
outcomes across all four codecs, including every byte, sequence/trailing/range
errors, 512-byte boundaries, early syntax faults with comments or later keys,
explicit document ends, first/last field changes and opaque raw bodies. There
are 2,076 accepted inputs. Tests compare exact errors and encoded bytes plus
copy geometry and Go re-reading of the edited file.
`make compat-raw-reader-read` tests 6,228 actual Rust outputs in Go: 5,911 accepted
byte roundtrips and 317 matching rejections. Go last-field spans can include a
document-end marker; replacing that span can make previously ignored malformed
suffix bytes active again. These outcomes remain part of the captured contract.
An independent regression verifies token-dependent precedence
and actual disk-index roundtrip of ignored invalid frontmatter. Existing codec,
workflow, generic-document, plan, index and query corpora remain required.

This qualifies the captured UTF-8 reader behavior, not the full WP3 format gate.
The following UTF-16 reader slice qualifies BOM-selected LE/BE frontmatter
against its own captured cases; broader YAML encoding cases remain a gate. Wider YAML syntax and aliases, other multifault combinations,
new raw log arguments, generated edit sequences, production proof-coupling
audit and filesystem/transport/performance gates remain. The original-test
ledger remains 96 ported and 129 pending.


### BOM-selected UTF-16 reader and physical edit spans

Frontmatter framing and field preservation still use the original raw ASCII LF
bytes. A separate BOM-selected UTF-16 LE/BE view supplies YAML semantics and
inline comments. Key lines from decoded YAML select the same raw physical lines
as Go; changed fields are emitted as UTF-8, retaining every untouched UTF-16 byte.
This intentionally preserves the mixed-encoding outputs of Go's line editor.
The BOM and original body bytes survive a no-op exactly. The reader validates
UTF-16 units and surrogate pairs as its 512-byte windows are requested, using
Go's low-surrogate, pair, incomplete-unit and control-character error order.
A short nonempty read does not report EOF until the next refill, so a truncated
unit beyond the scanner's demand can remain ignored after a document end.

`make compat-utf16-reader` recaptures 7,200 inputs and 21,600 parse/edit outcomes
from the immutable Go baseline. Both byte orders, all four note kinds, Unicode
and supplementary-plane scalars, blank/comment prefixes, early syntax faults,
explicit document ends, raw 510/512/514/1024/4096 boundaries, malformed and
truncated surrogate/unit sequences, control/noncharacter values and first/last
field edits are covered. Of these inputs, 2,048 are accepted. The corpus stores
lossless repeated-unit chunks to keep padding compact; tests expand the captured
bytes before calling production parsers and encoders.

`make compat-utf16-reader-read` checks 6,144 actual Rust outputs in fixed Go:
2,464 accepted byte roundtrips and 3,680 matching rejections. Successful Go
encoding does not promise a valid reread for these mixed-encoding edits; both
implementations retain those captured outcomes. Rust also checks reread errors,
accepted reread no-ops and retained-copy geometry. An independent regression
checks the Unicode title, raw physical title span, decoded inline comment,
actual splice output and disk-index preservation in both byte orders.

The full WP3 gate remains open: wider YAML schemas/aliases and multifault
ordering, raw new log arguments, generated mutation sequences, an explicit
production proof-coupling audit, operation-owned project file writes and broader
filesystem/transport/startup/link-density qualification are pending. The
original-test ledger remains 96 ported and 129 pending. Go remains the default
binary, and WP5 mutations stay held until the lossless format gate is complete.
