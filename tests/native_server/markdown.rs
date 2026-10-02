use super::*;

#[test]
fn real_http_handoff_wiki_archived_search_and_markdown_embeds() {
    let f = Fixture::new();
    f.write("projects/p/handoffs/p-handoff1-live.md","---\nid: p-handoff1\ntitle: Live handoff\ncreated: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\n---\n# Live continuation\n\nneedle live\n");
    f.write("projects/p/handoffs/archive/2026/p-handoff2-old.md","---\nid: p-handoff2\ntitle: Archived handoff\ncreated: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\n---\n# Archived continuation\n\nneedle archive\n");
    f.write(
        "projects/p/docs/embed.md",
        "# Embed\n\n![[guide#Section]]\n\n`![[img.png]]`\n\n```\n![[guide]]\n```\n",
    );
    let s = Server::new(&f);
    let page = s.ok("/api/docs/projects/p/handoffs/p-handoff1-live");
    assert_eq!(page["kind"], "handoff");
    assert!(page["html"].as_str().unwrap().contains("Live continuation"));
    assert!(!page["html"].as_str().unwrap().contains("created:"));
    assert_eq!(
        s.ok("/api/search?q=needle+archive")
            .as_array()
            .unwrap()
            .len(),
        0
    );
    assert_eq!(
        s.ok("/api/search?q=needle+archive&include_archived_handoffs=true")[0]["id"],
        "p-handoff2"
    );
    let html = s.ok("/api/docs/projects/p/docs/embed")["html"]
        .as_str()
        .unwrap()
        .to_owned();
    assert!(html.contains("class=\"embed\""));
    assert!(html.contains("<mark>highlight</mark>"));
    assert!(html.contains("<code>![[img.png]]</code>"));
    assert!(html.contains("![[guide]]\n</code>"));
}
#[test]
fn native_markdown_semantic_features_escaping_aliases_and_toc() {
    let f = Fixture::new();
    f.write("projects/p/docs/nested.md", "# Nested\n\n![[guide]]\n");
    let app = f.app();
    let index = app.index.read().unwrap();
    let source = "# Same\n\n# Same\n\n## Héading *emphasis*\n\n[[p-aaaa-first|First alias]] [[missing|Missing alias]] [[guide#Section]]\n\n![[guide#Section]]\n\n> [!warning]+ Be Careful\n> body\n\n> [!example]- *Steps* to follow\n> 1. first\n> 2. second\n>\n> > [!note]\n> > nested\n\n==mark==\n\n| A | B |\n|---|---|\n| 1 | 2 |\n\n- [x] done\n\nFootnote.[^1]\n\n[^1]: retained footnote.\n\n`[[p-aaaa]]`\n\n```\n![[guide]]\n```\n\n<script>bad()</script>\n\n[x](javascript:alert(1))\n\n![x](data:text/html,evil)\n";
    let rendered = beans::markdown::render(source, &index, "projects/p/docs/features.md");
    let html = rendered.html;
    assert_eq!(rendered.toc[0].id, "same");
    assert_eq!(rendered.toc[1].id, "same-1");
    assert_eq!(rendered.toc[2].text, "Héading emphasis");
    for snippet in [
        "/issues/p-aaaa",
        "/wiki/projects/p/docs/guide#section",
        "First alias",
        "/wiki/new?title=missing",
        "class=\"wikilink\"",
        "class=\"embed\"",
        "<mark>mark</mark>",
        "callout-title",
        "Be Careful",
        "Steps to follow",
        "nested",
        "<table>",
        "type=\"checkbox\"",
        "retained footnote",
        "<code>[[p-aaaa]]</code>",
        "![[guide]]\n</code>",
    ] {
        assert!(html.contains(snippet), "missing {snippet}: {html}");
    }
    assert!(!html.contains("<script>"));
    assert!(!html.contains("href=\"javascript:"));
    assert!(!html.contains("src=\"data:"));
    let recursion = beans::markdown::render("![[guide]]", &index, "projects/p/docs/embed.md");
    assert!(recursion.html.contains("<h1 id=\"guide\""));
    let bounded = beans::markdown::render("![[nested]]", &index, "projects/p/docs/example.md");
    assert_eq!(bounded.html.matches("class=\"embed\"").count(), 1);
    assert!(
        bounded
            .html
            .contains("href=\"/wiki/projects/p/docs/guide\"")
    );
}

#[test]
fn every_authored_markdown_fixture_has_native_semantic_assertions() {
    let f = Fixture::new();
    f.write(
        "docs/note.md",
        "# Note\n\nbefore section\n\n## Heading\n\nsection only\n\n## Later\n\nnot selected\n",
    );
    f.write("docs/my image.PNG", "PNG");
    f.write(
        "docs/tagged.md",
        "---\ntitle: Tagged native doc\ntags: [project]\n---\nIndependent body\n",
    );
    let app = f.app();
    let index = app.index.read().unwrap();
    let directory = PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("tests/fixtures/native-baseline/markdown/testdata");
    let cases: &[(&str, &[&str], &[&str])] = &[
        (
            "blockquote-not-callout",
            &["<blockquote>", "plain quote", "second line"],
            &["class=\"callout"],
        ),
        (
            "callout-case",
            &["data-callout=\"warning\"", "Loud", "body"],
            &[],
        ),
        (
            "callout-nested-list",
            &[
                "Steps",
                "<ol>",
                "first",
                "<code>code</code>",
                "nested callout",
                "[[not a link]]",
            ],
            &["/wiki/new?title=not"],
        ),
        (
            "callout-no-title",
            &["data-callout=\"tip\"", "tip", "Just a tip"],
            &[],
        ),
        (
            "callout-not-first-line",
            &["<blockquote>", "intro line", "[!note] not first"],
            &["class=\"callout"],
        ),
        (
            "callout-title-fold",
            &[
                "data-callout=\"warning\"",
                "Be Careful",
                "It spans two lines",
            ],
            &["[!warning]"],
        ),
        (
            "code-no-links",
            &["<code>[[not a link]]</code>", "[[not a link]]\n</code>"],
            &["class=\"wikilink\""],
        ),
        (
            "dangerous-urls",
            &["x", "y"],
            &["href=\"javascript:", "src=\"data:"],
        ),
        (
            "embed-image-space",
            &[
                "/api/assets/docs/my%20image.PNG",
                "alt=\"my image.PNG\"",
                "/search?q=tag%2Fwith",
                " space",
            ],
            &[],
        ),
        (
            "embed-image",
            &["/api/assets/projects/p/docs/img.png", "alt=\"img.png\""],
            &[],
        ),
        (
            "embed-note-fragment",
            &["class=\"embed\"", "section only"],
            &["before section", "not selected"],
        ),
        (
            "embed-note",
            &[
                "class=\"embed\"",
                "before section",
                "section only",
                "not selected",
            ],
            &[],
        ),
        (
            "footnote",
            &["The footnote text", "footnote", "href=\"#"],
            &[],
        ),
        (
            "frontmatter",
            &["Body text after frontmatter"],
            &["title: Hello", "tags:"],
        ),
        (
            "hashtag",
            &[
                "/search?q=project",
                ">#project</a>",
                "/search?q=another-tag",
            ],
            &[],
        ),
        (
            "headings",
            &[
                "id=\"title-one\"",
                "id=\"sub-heading\"",
                "id=\"sub-sub-heading\"",
                "id=\"not-in-toc\"",
            ],
            &[],
        ),
        ("highlight-runs", &["=d=", "<mark>ok</mark>"], &[]),
        (
            "highlight",
            &["<mark>highlighted text</mark>", "lone = sign"],
            &[],
        ),
        ("raw-html", &["raw html"], &["<strong>", "<div>A raw block"]),
        ("table", &["<table>", "<th>A</th>", "<td>2</td>"], &[]),
        (
            "tasklist",
            &[
                "type=\"checkbox\"",
                "checked=\"\"",
                "Todo item",
                "Done item",
            ],
            &[],
        ),
        (
            "wikilinks",
            &[
                "/wiki/docs/note",
                "My Note",
                "#section-two",
                "/wiki/new?title=missing%20page",
            ],
            &[],
        ),
    ];
    let names = fs::read_dir(&directory)
        .unwrap()
        .map(Result::unwrap)
        .filter(|e| e.path().extension().is_some_and(|e| e == "md"))
        .map(|e| e.path().file_stem().unwrap().to_str().unwrap().to_owned())
        .collect::<std::collections::BTreeSet<_>>();
    assert_eq!(
        names,
        cases.iter().map(|c| c.0.to_owned()).collect(),
        "each fixture requires an explicit semantic disposition"
    );
    for (name, required, forbidden) in cases {
        let source = fs::read_to_string(directory.join(format!("{name}.md"))).unwrap();
        let rendered = beans::markdown::render(&source, &index, "docs/features.md");
        for snippet in *required {
            assert!(
                rendered.html.contains(snippet),
                "{name}: missing {snippet}: {}",
                rendered.html
            );
        }
        for snippet in *forbidden {
            assert!(
                !rendered.html.contains(snippet),
                "{name}: unexpected {snippet}: {}",
                rendered.html
            );
        }
        if *name == "headings" {
            assert_eq!(
                rendered
                    .toc
                    .iter()
                    .map(|h| h.text.as_str())
                    .collect::<Vec<_>>(),
                ["Title One", "Sub Heading", "Sub Sub Heading", "Not In Toc"]
            );
        }
    }
    let protected = beans::markdown::render(
        "`#code` [#label](https://example.test) <script>#script</script> plain#suffix #雪\n",
        &index,
        "docs/features.md",
    );
    assert!(!protected.html.contains("/search?q=code"));
    assert!(!protected.html.contains("/search?q=label"));
    assert!(!protected.html.contains("/search?q=suffix"));
    assert!(protected.html.contains("/search?q=%E9%9B%AA"));
    let heading = beans::markdown::render(
        "# Tagged #project\n\n# Tagged #project\n",
        &index,
        "docs/features.md",
    );
    assert_eq!(heading.toc[0].text, "Tagged #project");
    assert_eq!(heading.toc[0].id, "tagged-project");
    assert_eq!(heading.toc[1].id, "tagged-project-1");
    assert!(heading.html.contains("id=\"tagged-project\""));
    assert!(heading.html.contains("href=\"/search?q=project\""));
    // Follow the rendered query through the same API used by the Search screen.
    // This catches links that render correctly but initialize an empty UI query.
    let rendered = beans::markdown::render("#project", &index, "docs/features.md");
    let href = rendered
        .html
        .split("href=\"")
        .nth(1)
        .unwrap()
        .split('"')
        .next()
        .unwrap();
    let query = href.strip_prefix("/search?").unwrap();
    assert!(query.split('&').any(|pair| pair == "q=project"));
    let server = Server::new(&f);
    let hits = server.ok(&format!("/api/search?{query}"));
    assert!(
        hits.as_array()
            .unwrap()
            .iter()
            .any(|hit| hit["title"] == "Tagged native doc")
    );
}

#[test]
fn real_http_inline_ast_keeps_heading_anchors_attributes_and_authored_marker_literals() {
    let f = Fixture::new();
    let source = "# Tagged #project\n\n# Tagged project\n\n# Tagged #project\n\n# Quote \" & #project\n\n# Header ![[guide]]\n\n`BNHASHTAG0END` #project\n\n![BNHASHTAG0END](https://example.test/x) #project\n\n`BNEMBED0END` ![[guide]]\n\n![BNEMBED0END](https://example.test/x) ![[guide]]\n\nLiteral BNHASHTAG0END and BNEMBED0END.\n\n`#code ![[guide]]` [#link ![[guide]]](https://example.test/link)\n\n<script>bad()</script>\n";
    f.write("projects/p/docs/inline.md", source);
    let server = Server::new(&f);
    let response = server.ok("/api/docs/projects/p/docs/inline");
    let html = response["html"].as_str().unwrap();
    let headings = [
        ("tagged-project", "Tagged #project"),
        ("tagged-project-1", "Tagged project"),
        ("tagged-project-2", "Tagged #project"),
        ("quote---project", "Quote &quot; &amp; #project"),
        ("header-guide", "Header guide"),
    ];
    for (id, text) in headings {
        assert_eq!(
            html.matches(&format!("<h1 id=\"{id}\"")).count(),
            1,
            "{html}"
        );
        assert_eq!(
            html.matches(&format!("href=\"#{id}\"")).count(),
            1,
            "{html}"
        );
        assert!(html.contains(&format!("aria-label=\"Link to heading '{text}'\" data-heading-content=\"{text}\" class=\"anchor\"")), "{html}");
    }
    assert!(!html.contains("bnhashtag"));
    assert!(!html.contains("bnembed"));
    assert!(!html.contains("aria-label=\"Link to heading 'Tagged <a"));
    assert!(!html.contains("data-heading-content=\"Header <div"));
    assert!(html.contains("<code>BNHASHTAG0END</code>"));
    assert!(html.contains("<code>BNEMBED0END</code>"));
    assert!(html.contains("src=\"https://example.test/x\" alt=\"BNHASHTAG0END\""));
    assert!(html.contains("src=\"https://example.test/x\" alt=\"BNEMBED0END\""));
    assert!(html.contains("Literal BNHASHTAG0END and BNEMBED0END."));
    assert!(html.contains("<code>#code ![[guide]]</code>"));
    assert!(!html.contains("/search?q=code"));
    assert!(!html.contains("/search?q=link"));
    assert!(!html.contains("<script>"));
    assert!(html.contains("class=\"embed\""));
    assert!(html.contains("class=\"hashtag\" href=\"/search?q=project\""));
    let app = f.app();
    let index = app.index.read().unwrap();
    let embedded_first = beans::markdown::render(
        "![[guide]]\n\n# Outer Heading\n",
        &index,
        "projects/p/docs/inline.md",
    );
    assert!(embedded_first.html.contains("<h1 id=\"guide\""));
    assert!(embedded_first.html.contains("<h1 id=\"outer-heading\""));
    assert_eq!(embedded_first.toc.len(), 1);
    assert_eq!(embedded_first.toc[0].id, "outer-heading");
    assert_eq!(embedded_first.toc[0].text, "Outer Heading");
    assert!(embedded_first.html.contains("href=\"#outer-heading\""));
    // A heading embed remains phrasing content and keeps one formatter's IDs.
    let heading = html
        .split("<h1 id=\"header-guide\">")
        .nth(1)
        .unwrap()
        .split("</h1>")
        .next()
        .unwrap();
    assert!(heading.contains("href=\"/wiki/projects/p/docs/guide\""));
    assert!(!heading.contains("<div"));
    let toc = response["toc"].as_array().unwrap();
    let ids = toc
        .iter()
        .map(|h| h["id"].as_str().unwrap())
        .collect::<std::collections::BTreeSet<_>>();
    assert_eq!(ids.len(), toc.len());
    assert_eq!(toc[0]["text"], "Tagged #project");
    assert_eq!(toc[1]["id"], "tagged-project-1");
    assert_eq!(toc[2]["id"], "tagged-project-2");
}
