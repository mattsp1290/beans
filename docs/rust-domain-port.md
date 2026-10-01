# Rust domain port

WP3 is in progress. The current parser/editing primitives are implemented in
`src/domain/frontmatter.rs`, with YAML event and source-position adaptation in
`src/domain/yaml.rs`. Issue reading and body mutation semantics are in
`src/domain/issue.rs`; stored log rules are in `src/domain/log.rs`. Existing-issue encoding is in `src/domain/issue_encode.rs`, with owned YAML
presentation in `src/domain/yaml_render.rs` and source-provenance planning in
`src/domain/splicing.rs`. New-file construction uses the same owned-field renderer. New unknown Extra
node construction, request codecs, other note kinds, plans, configuration,
resolution and indexing remain to be ported. Go is still the
default implementation, and no Go regression has been retired.

The parser retains original UTF-8 text and derives absolute byte ranges from
line offsets. Semantic YAML nodes are separate from retained text; duplicate
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
