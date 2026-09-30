# Rust domain port

WP3 is in progress. The current parser/editing primitives are implemented in
`src/domain/frontmatter.rs`, with YAML event and source-position adaptation in
`src/domain/yaml.rs`. Issue frontmatter reading is in `src/domain/issue.rs`. Typed encoders and
request codecs, other note kinds, plans,
configuration, resolution and indexing remain to be ported. Go is still the
default implementation, and no Go regression has been retired.

The parser retains original UTF-8 text and derives absolute byte ranges from
line offsets. Semantic YAML nodes are separate from retained text; duplicate
unknown keys, comments, block styles and Markdown are not serialized again.
Owned scalar/list decoding preserves yaml.v3's scalar spellings and null rules;
aliases are separate nodes and fail scalar/list coercion. The typed issue reader checks owned duplicates and required keys, decodes
metadata, derives Linux project/archive paths, and retains original bytes and
body sections. It preserves Go's permissive issue values, signed 64-bit priority
bounds, and timestamp semantics including one-digit hours, comma fractions and
zone hour 24. The corpus adds 33 generated metadata/validation cases.

The pinned yaml-rust2 low-level event API passed the prototype gate across all
21 original issue/request roundtrip inputs plus 14 added syntax inputs. Node
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
YAML text, byte ranges, body sections, links, issue metadata and errors from the immutable Go revision
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

Remaining codec gates include other note schemas, complete
yaml.v3 syntax-error presentation and obscure Unicode error quoting, scalar/list rendering
and inline comments, note-specific body/log mutation and log entries, and cross-language
parse/encode/edit tests. Typed production encoders must use this real replacement
path before WP3's full proof-coupling gate can be accepted. Primitive parity
alone does not establish issue/request or whole-hub compatibility.
