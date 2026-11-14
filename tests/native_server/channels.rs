use super::*;

#[test]
fn channel_search_and_epic_backlinks_open_read_only_wiki_documents() {
    let fixture = Fixture::new();
    let created = fixture.cli(&[
        "--actor",
        "coordinator",
        "channel",
        "create",
        "agentui-build",
        "--participant",
        "mac-agent",
        "--issue",
        "p-aaaa",
    ]);
    let channel_id = created["id"].as_str().unwrap();
    let sent = fixture.cli(&[
        "--actor",
        "coordinator",
        "channel",
        "send",
        channel_id,
        "--key",
        "mac-evidence",
        "--to",
        "mac-agent",
        "--reference",
        "[[p-p-a1b2-review]]",
        "--description",
        "**Mac evidence** for [[p-bbbb]] and [[p-p-a1b2-review]].",
    ]);
    let message_id = sent["id"].as_str().unwrap();
    let head = git(&fixture.hub, &["rev-parse", "HEAD"]);
    let clean = git(&fixture.hub, &["status", "--porcelain"]);
    let server = Server::new(&fixture);

    let channels = server.ok("/api/search?q=agentui-build");
    let channel = channels
        .as_array()
        .unwrap()
        .iter()
        .find(|hit| hit["kind"] == "channel" && hit["id"] == channel_id)
        .unwrap();
    let channel_path = channel["path"].as_str().unwrap();
    let channel_doc = server.ok(&format!(
        "/api/docs/{}",
        channel_path.trim_end_matches(".md")
    ));
    assert_eq!(channel_doc["kind"], "channel");
    assert_eq!(channel_doc["title"], "agentui-build");
    assert_eq!(channel_doc["path"], channel_path);
    assert!(
        channel_doc["outlinks"]
            .as_array()
            .unwrap()
            .iter()
            .any(|link| link["to"] == "p-aaaa-first")
    );

    let epic = server.ok("/api/issues/p-aaaa");
    let backlink = epic["backlinks"]
        .as_array()
        .unwrap()
        .iter()
        .find(|link| link["note_kind"] == "channel" && link["from"] == channel_id)
        .unwrap();
    assert_eq!(backlink["path"], channel_path);
    assert_eq!(
        server.ok(&format!("/api/docs/{channel_path}"))["path"],
        channel_path
    );

    let messages = server.ok("/api/search?q=Mac%20evidence");
    let message = messages
        .as_array()
        .unwrap()
        .iter()
        .find(|hit| hit["kind"] == "channel_message" && hit["id"] == message_id)
        .unwrap();
    let message_path = message["path"].as_str().unwrap();
    let message_doc = server.ok(&format!(
        "/api/docs/{}",
        message_path.trim_end_matches(".md")
    ));
    assert_eq!(message_doc["kind"], "channel_message");
    let html = message_doc["html"].as_str().unwrap();
    assert!(html.contains("<strong>Mac evidence</strong>"));
    assert!(html.contains("/issues/p-bbbb"));
    assert!(html.contains("/prompts/p-p-a1b2"));
    assert!(
        message_doc["outlinks"]
            .as_array()
            .unwrap()
            .iter()
            .any(|link| link["to"] == "p-p-a1b2-review")
    );

    assert_eq!(git(&fixture.hub, &["rev-parse", "HEAD"]), head);
    assert_eq!(git(&fixture.hub, &["status", "--porcelain"]), clean);
}
