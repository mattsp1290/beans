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
