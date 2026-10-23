use beans::{
    markdown,
    vault::{
        GraphNote, Index, LinkKind, NoteData, NoteGraph, NoteKind, PromptFilter, SearchOptions,
        classify,
    },
};
use std::path::{Path, PathBuf};

fn write(root: &Path, relative: &str, contents: impl AsRef<[u8]>) -> PathBuf {
    let path = root.join(relative);
    std::fs::create_dir_all(path.parent().unwrap()).unwrap();
    std::fs::write(&path, contents).unwrap();
    path
}

fn claimant_line(value: Option<&str>) -> String {
    value.map_or(String::new(), |value| format!("claimed_by: {value}\n"))
}

fn issue_line(value: Option<&str>) -> String {
    value.map_or(String::new(), |value| format!("issue: \"[[{value}]]\"\n"))
}

macro_rules! prompt {
    (
        $id:expr,
        $title:expr,
        $status:expr,
        $priority:expr,
        $from:expr,
        $to:expr,
        $claimed_by:expr,
        $issue:expr,
        $created:expr,
        $labels:expr,
        $body:expr $(,)?
    ) => {{
        let claimant = claimant_line($claimed_by);
        let issue = issue_line($issue);
        format!(
            "---\nid: {}\naliases: [{}-alias]\ntitle: {}\nstatus: {}\npriority: {}\nlabels: [{}]\nfrom: {}\nto: {}\n{claimant}{issue}created: {}\nupdated: {}\n---\n{}\n",
            $id, $id, $title, $status, $priority, $labels, $from, $to, $created, $created, $body
        )
    }};
}

fn project(root: &Path, name: &str) {
    write(
        root,
        &format!("projects/{name}/beans.toml"),
        format!("name='{name}'\nprefix='{name}'\n"),
    );
}

fn ids(notes: Vec<&beans::vault::Note>) -> Vec<String> {
    notes
        .into_iter()
        .map(|note| String::from_utf8(note.graph.id.clone().unwrap()).unwrap())
        .collect()
}

#[test]
fn prompt_index_classifies_exact_paths_loads_fixture_and_reloads_changes_and_removal() {
    assert_eq!(
        classify(b"projects/alpha/prompts/alpha-p-a1.md"),
        Some((NoteKind::Prompt, b"alpha".to_vec()))
    );
    assert!(classify(b"projects/alpha/prompts/nested/alpha-p-a1.md").is_none());
    assert!(classify(b"prompts/alpha-p-a1.md").is_none());

    let hub = super::index::hub("prompt-index-domain", false);
    let path = hub
        .0
        .join("projects/p/prompts/alpha-p-a1b2-review-contract.md");
    std::fs::create_dir_all(path.parent().unwrap()).unwrap();
    std::fs::copy(
        "tests/fixtures/hub/projects/alpha/prompts/alpha-p-a1b2-review-contract.md",
        &path,
    )
    .unwrap();
    let mut index = Index::load(&hub.0).unwrap();
    let loaded = index.note_by_id(NoteKind::Prompt, b"alpha-p-a1b2").unwrap();
    assert_eq!(loaded.project, b"p");
    assert_eq!(
        loaded.tags.as_ref().unwrap(),
        &vec![b"contracts".to_vec(), b"review".to_vec()]
    );
    let NoteData::Prompt(document) = &loaded.data else {
        panic!("expected prompt data")
    };
    assert_eq!(document.log.len(), 1);

    let changed = std::fs::read_to_string(&path)
        .unwrap()
        .replace("Review the prompt contract", "Changed prompt title");
    std::fs::write(&path, changed).unwrap();
    index.reload(std::slice::from_ref(&path)).unwrap();
    assert_eq!(
        index
            .note_by_id(NoteKind::Prompt, b"alpha-p-a1b2")
            .unwrap()
            .title,
        b"Changed prompt title"
    );
    std::fs::remove_file(&path).unwrap();
    index.reload(&[path]).unwrap();
    assert!(
        index
            .note_by_id(NoteKind::Prompt, b"alpha-p-a1b2")
            .is_none()
    );
}

#[test]
fn prompt_query_filters_searches_instruction_only_sorts_and_unions_inbox() {
    let hub = super::index::hub("prompt-query-domain", false);
    project(&hub.0, "q");
    let cases = [
        (
            "p-p-b",
            "open",
            1,
            "2026-01-02T00:00:00Z",
            "alice",
            None,
            "beta",
            "body beta",
        ),
        (
            "p-p-a",
            "in_progress",
            1,
            "2026-01-01T00:00:00Z",
            "alice",
            Some("alice"),
            "alpha",
            "instruction needle",
        ),
        (
            "p-p-c",
            "completed",
            0,
            "2026-01-01T00:00:00Z",
            "bob",
            Some("alice"),
            "alpha",
            "terminal body",
        ),
        (
            "p-p-d",
            "declined",
            2,
            "2026-01-01T00:00:00Z",
            "alice",
            None,
            "beta",
            "terminal declined",
        ),
    ];
    for (id, status, priority, created, to, claimant, label, body) in cases {
        write(
            &hub.0,
            &format!("projects/p/prompts/{id}.md"),
            prompt!(
                id,
                id,
                status,
                priority,
                "sender",
                to,
                claimant,
                None,
                created,
                label,
                &format!("{body}\n\n## Log\n- 2026-01-01T00:00:00Z sender: log-secret"),
            ),
        );
    }
    write(
        &hub.0,
        "projects/q/prompts/q-p-a.md",
        prompt!(
            "q-p-a",
            "other project",
            "open",
            0,
            "other",
            "alice",
            None,
            None,
            "2026-01-01T00:00:00Z",
            "alpha",
            "cross project",
        ),
    );
    let index = Index::load(&hub.0).unwrap();
    assert_eq!(
        ids(index.project_prompts(&PromptFilter::default())),
        ["q-p-a", "p-p-a", "p-p-b"]
    );
    assert_eq!(
        ids(index.project_prompts(&PromptFilter {
            project: b"p".to_vec(),
            terminal: true,
            ..Default::default()
        })),
        ["p-p-c", "p-p-a", "p-p-b", "p-p-d"]
    );
    assert_eq!(
        ids(index.project_prompts(&PromptFilter {
            status: b"completed".to_vec(),
            ..Default::default()
        })),
        ["p-p-c"]
    );
    let filters = [
        (
            PromptFilter {
                label: b"alpha".to_vec(),
                ..Default::default()
            },
            vec!["q-p-a", "p-p-a"],
        ),
        (
            PromptFilter {
                priority: Some(1),
                ..Default::default()
            },
            vec!["p-p-a", "p-p-b"],
        ),
        (
            PromptFilter {
                from: b"sender".to_vec(),
                project: b"p".to_vec(),
                ..Default::default()
            },
            vec!["p-p-a", "p-p-b"],
        ),
        (
            PromptFilter {
                to: b"alice".to_vec(),
                project: b"p".to_vec(),
                ..Default::default()
            },
            vec!["p-p-a", "p-p-b"],
        ),
        (
            PromptFilter {
                claimed_by: b"alice".to_vec(),
                ..Default::default()
            },
            vec!["p-p-a"],
        ),
        (
            PromptFilter {
                query: b"instruction needle".to_vec(),
                ..Default::default()
            },
            vec!["p-p-a"],
        ),
    ];
    for (filter, expected) in filters {
        assert_eq!(ids(index.project_prompts(&filter)), expected, "{filter:?}");
    }
    assert!(
        index
            .project_prompts(&PromptFilter {
                query: b"log-secret".to_vec(),
                terminal: true,
                ..Default::default()
            })
            .is_empty()
    );
    assert_eq!(
        ids(index.prompt_inbox(
            b"alice",
            &PromptFilter {
                terminal: true,
                ..Default::default()
            }
        )),
        ["p-p-c", "q-p-a", "p-p-a", "p-p-b", "p-p-d"]
    );
}

#[test]
fn prompt_graph_links_issue_backlinks_without_dependency_edges_and_reports_missing() {
    let hub = super::index::hub("prompt-graph-domain", false);
    write(
        &hub.0,
        "projects/p/issues/p-i-a.md",
        "---\nid: p-i-a\ntitle: issue\ntype: task\nstatus: open\npriority: 2\ncreated: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\n---\n",
    );
    write(
        &hub.0,
        "projects/p/prompts/p-p-linked.md",
        prompt!(
            "p-p-linked",
            "linked",
            "open",
            1,
            "a",
            "b",
            None,
            Some("p-i-a"),
            "2026-01-01T00:00:00Z",
            "link",
            "[[p-i-a]]",
        ),
    );
    write(
        &hub.0,
        "projects/p/prompts/p-p-missing.md",
        prompt!(
            "p-p-missing",
            "missing",
            "open",
            1,
            "a",
            "b",
            None,
            Some("p-i-missing"),
            "2026-01-01T00:00:00Z",
            "link",
            "body",
        ),
    );
    let index = Index::load(&hub.0).unwrap();
    let linked = index.note_by_id(NoteKind::Prompt, b"p-p-linked").unwrap();
    assert!(
        linked
            .graph
            .outlinks
            .iter()
            .any(|link| link.kind == LinkKind::PromptIssue && link.to == b"p-i-a")
    );
    assert_eq!(
        index.graph.backlinks()[b"p-i-a".as_slice()]
            .iter()
            .filter(|link| link.from == b"p-p-linked")
            .count(),
        1
    );
    assert!(
        index.graph.backlinks()[b"p-i-a".as_slice()]
            .iter()
            .any(|link| link.kind == LinkKind::PromptIssue)
    );
    assert!(
        !linked
            .graph
            .outlinks
            .iter()
            .any(|link| link.kind == LinkKind::Body && link.to == b"p-i-a")
    );
    assert!(
        index
            .graph
            .warnings()
            .iter()
            .any(|warning| warning.error.to_string().contains("p-i-missing"))
    );
    let dependencies = index.dependency_graph(b"p", true);
    assert_eq!(dependencies.nodes.len(), 1);
    assert!(dependencies.edges.is_empty());
    assert!(
        index
            .ready(b"p", true)
            .iter()
            .all(|note| note.graph.kind == NoteKind::Issue)
    );
}

#[test]
fn prompt_issue_dedup_is_note_local_and_applies_to_unresolved_targets() {
    let hub = super::index::hub("prompt-graph-dedup-domain", false);
    write(
        &hub.0,
        "projects/p/docs/shared.md",
        "---\ntitle: shared doc\n---\n[[p-i-a]]\n",
    );
    write(
        &hub.0,
        "projects/p/issues/p-i-a.md",
        "---\nid: p-i-a\ntitle: issue\ntype: task\nstatus: open\npriority: 2\ncreated: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\n---\n",
    );
    write(
        &hub.0,
        "projects/p/prompts/shared.md",
        prompt!(
            "p-p-shared",
            "shared prompt",
            "open",
            1,
            "a",
            "b",
            None,
            Some("p-i-a"),
            "2026-01-01T00:00:00Z",
            "link",
            "[[p-i-a]]",
        ),
    );
    write(
        &hub.0,
        "projects/p/prompts/unresolved.md",
        prompt!(
            "p-p-unresolved",
            "unresolved",
            "open",
            1,
            "a",
            "b",
            None,
            Some("p-i-missing"),
            "2026-01-01T00:00:00Z",
            "link",
            "[[p-i-missing]]",
        ),
    );

    let index = Index::load(&hub.0).unwrap();
    let backlinks = &index.graph.backlinks()[b"p-i-a".as_slice()];
    assert_eq!(
        backlinks
            .iter()
            .filter(|link| link.from == b"shared" && link.kind == LinkKind::Body)
            .count(),
        1,
        "the doc backlink sharing the prompt basename must be retained"
    );
    assert_eq!(
        backlinks
            .iter()
            .filter(|link| link.from == b"shared" && link.kind == LinkKind::PromptIssue)
            .count(),
        1
    );
    let unresolved = index
        .note_by_id(NoteKind::Prompt, b"p-p-unresolved")
        .unwrap();
    assert_eq!(
        unresolved
            .graph
            .outlinks
            .iter()
            .filter(|link| link.to == b"p-i-missing")
            .count(),
        1
    );
    assert_eq!(
        index
            .graph
            .warnings()
            .iter()
            .filter(|warning| warning.error.to_string().contains("p-i-missing"))
            .count(),
        1
    );
}

#[test]
fn issue_id_precedes_colliding_prompt_basename_and_alias() {
    let hub = super::index::hub("prompt-lookup-precedence-domain", false);
    for id in ["p-i-basename", "p-i-alias"] {
        write(
            &hub.0,
            &format!("projects/p/issues/{id}-owner.md"),
            format!(
                "---\nid: {id}\ntitle: {id} issue\ntype: task\nstatus: open\npriority: 2\ncreated: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\n---\n"
            ),
        );
    }
    write(
        &hub.0,
        "projects/p/prompts/p-i-basename.md",
        prompt!(
            "p-p-basename",
            "basename collision",
            "open",
            1,
            "a",
            "b",
            None,
            None,
            "2026-01-01T00:00:00Z",
            "x",
            "body",
        ),
    );
    let alias_prompt = prompt!(
        "p-p-alias",
        "alias collision",
        "open",
        1,
        "a",
        "b",
        None,
        None,
        "2026-01-01T00:00:00Z",
        "x",
        "body",
    )
    .replace("aliases: [p-p-alias-alias]", "aliases: [p-i-alias]");
    write(&hub.0, "projects/p/prompts/alias-owner.md", alias_prompt);

    let index = Index::load(&hub.0).unwrap();
    for id in [b"p-i-basename".as_slice(), b"p-i-alias"] {
        assert_eq!(index.lookup(id).unwrap().graph.kind, NoteKind::Issue);
        assert_eq!(index.lookup(id).unwrap().graph.id.as_deref(), Some(id));
    }
    assert_eq!(
        index.lookup(b"p-p-basename").unwrap().graph.kind,
        NoteKind::Prompt
    );
    assert_eq!(
        index.lookup(b"p-p-alias").unwrap().graph.kind,
        NoteKind::Prompt
    );
}

#[test]
fn cross_kind_identical_ids_are_typed_and_generic_lookup_is_issue_first() {
    let mut graph = NoteGraph::default();
    for (kind, name) in [
        (NoteKind::Issue, "issue"),
        (NoteKind::Request, "request"),
        (NoteKind::Prompt, "prompt"),
    ] {
        graph.register(GraphNote {
            kind,
            path: format!("{name}.md").into_bytes(),
            basename: name.as_bytes().to_vec(),
            id: Some(b"same-id".to_vec()),
            aliases: vec![],
            raw_out: vec![],
            outlinks: vec![],
        });
    }
    graph.rebuild();

    for kind in [NoteKind::Issue, NoteKind::Request, NoteKind::Prompt] {
        assert_eq!(graph.by_id(kind, b"same-id").unwrap().kind, kind);
    }
    assert_eq!(graph.lookup(b"same-id").unwrap().kind, NoteKind::Issue);
}

#[test]
fn duplicate_prompt_owner_and_diagnostics_match_clean_reload_after_incremental_reload() {
    let hub = super::index::hub("prompt-reload-parity-domain", false);
    let first_path = write(
        &hub.0,
        "projects/p/prompts/a.md",
        prompt!(
            "p-p-duplicate",
            "first",
            "open",
            1,
            "a",
            "b",
            None,
            None,
            "2026-01-01T00:00:00Z",
            "x",
            "first body",
        ),
    );
    write(
        &hub.0,
        "projects/p/prompts/z.md",
        prompt!(
            "p-p-duplicate",
            "second",
            "open",
            1,
            "a",
            "b",
            None,
            None,
            "2026-01-01T00:00:00Z",
            "x",
            "second body",
        ),
    );
    let mut index = Index::load(&hub.0).unwrap();
    let changed = std::fs::read_to_string(&first_path)
        .unwrap()
        .replace("title: first", "title: first changed");
    std::fs::write(&first_path, changed).unwrap();
    index.reload(std::slice::from_ref(&first_path)).unwrap();

    let incremental_owner = index
        .note_by_id(NoteKind::Prompt, b"p-p-duplicate")
        .map(|note| (note.graph.path.clone(), note.title.clone()));
    let incremental_query = index
        .project_prompts(&PromptFilter {
            terminal: true,
            ..Default::default()
        })
        .into_iter()
        .map(|note| (note.graph.path.clone(), note.title.clone()))
        .collect::<Vec<_>>();
    let incremental_warnings = index
        .graph
        .warnings()
        .iter()
        .map(|warning| (warning.path.clone(), warning.error.to_string()))
        .collect::<Vec<_>>();

    index.reload_all().unwrap();
    assert_eq!(
        incremental_owner,
        index
            .note_by_id(NoteKind::Prompt, b"p-p-duplicate")
            .map(|note| (note.graph.path.clone(), note.title.clone()))
    );
    assert_eq!(
        incremental_query,
        index
            .project_prompts(&PromptFilter {
                terminal: true,
                ..Default::default()
            })
            .into_iter()
            .map(|note| (note.graph.path.clone(), note.title.clone()))
            .collect::<Vec<_>>()
    );
    assert_eq!(
        incremental_warnings,
        index
            .graph
            .warnings()
            .iter()
            .map(|warning| (warning.path.clone(), warning.error.to_string()))
            .collect::<Vec<_>>()
    );
}

#[test]
fn prompt_diagnostics_duplicate_ids_are_kind_scoped_and_malformed_files_warn() {
    let hub = super::index::hub("prompt-diagnostics-domain", false);
    for name in ["one", "two"] {
        write(
            &hub.0,
            &format!("projects/p/prompts/{name}.md"),
            prompt!(
                "p-p-same",
                name,
                "open",
                1,
                "a",
                "b",
                None,
                None,
                "2026-01-01T00:00:00Z",
                "x",
                "body",
            ),
        );
    }
    write(
        &hub.0,
        "projects/p/issues/same.md",
        "---\nid: p-p-same\ntitle: issue\ntype: task\nstatus: open\npriority: 2\ncreated: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\n---\n",
    );
    write(
        &hub.0,
        "projects/p/requests/same.md",
        "---\nid: p-r-same\naliases: [p-r-same]\ntitle: request\nstatus: open\npriority: 2\nrequested_by: a\ncreated: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\n---\n",
    );
    write(
        &hub.0,
        "projects/p/prompts/malformed.md",
        "---\nid: not-a-prompt\n---\n",
    );
    let index = Index::load(&hub.0).unwrap();
    let warnings = index.graph.warnings();
    assert_eq!(
        warnings
            .iter()
            .filter(|warning| warning.error.to_string().contains("duplicate prompt id"))
            .count(),
        1
    );
    assert!(!warnings.iter().any(
        |warning| warning.error.to_string().contains("duplicate issue id")
            || warning.error.to_string().contains("duplicate request id")
    ));
    assert!(
        warnings
            .iter()
            .any(|warning| warning.path == b"projects/p/prompts/malformed.md")
    );
    assert_eq!(
        index
            .note_by_id(NoteKind::Issue, b"p-p-same")
            .unwrap()
            .title,
        b"issue"
    );
    assert_eq!(
        index
            .note_by_id(NoteKind::Request, b"p-r-same")
            .unwrap()
            .title,
        b"request"
    );
    assert!(index.note_by_id(NoteKind::Prompt, b"p-p-same").is_some());
    assert_eq!(
        index.lookup(b"p-p-same").unwrap().graph.kind,
        NoteKind::Issue
    );
}

#[test]
fn prompt_search_kind_body_labels_and_markdown_wikilink_route() {
    let hub = super::index::hub("prompt-search-links-domain", false);
    write(
        &hub.0,
        "projects/p/prompts/p-p-route.md",
        prompt!(
            "p-p-route",
            "Route prompt",
            "open",
            1,
            "sender",
            "receiver",
            None,
            None,
            "2026-01-01T00:00:00Z",
            "search-label",
            "search-body",
        ),
    );
    let index = Index::load(&hub.0).unwrap();
    for query in [b"Route prompt".as_slice(), b"search-label", b"search-body"] {
        let hits = index.search(
            query,
            &SearchOptions {
                kinds: vec!["prompt".into()],
                ..Default::default()
            },
        );
        assert_eq!(hits.len(), 1);
        assert_eq!(hits[0].kind, NoteKind::Prompt);
        assert_eq!(hits[0].id.as_bytes(), b"p-p-route");
    }
    assert!(
        index
            .search(
                b"search-body",
                &SearchOptions {
                    kinds: vec!["issue".into()],
                    ..Default::default()
                }
            )
            .is_empty()
    );
    let rendered = markdown::render("[[p-p-route]]", &index, "docs/source.md");
    assert!(
        rendered.html.contains("href=\"/prompts/p-p-route\""),
        "{}",
        rendered.html
    );
}
