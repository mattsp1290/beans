use super::*;

#[test]
fn real_http_mutations_share_cli_files_and_git_history() {
    let f = Fixture::new();
    let s = Server::new(&f);
    assert_eq!(s.ok("/api/health")["status"], "ok");
    assert_eq!(s.ok("/api/projects")[0]["counts"]["open"], 2);
    assert_eq!(s.ok("/api/projects/p/ready").as_array().unwrap().len(), 1);
    let (code,created)=s.json("POST","/api/projects/p/issues",json!({"title":"Created through HTTP","priority":0,"description":"HTTP description","labels":["web"],"blocked_by":["p-bbbb"]}));
    assert_eq!(code, 201, "{created}");
    assert_eq!(created["pushed"], true);
    let id = created["id"].as_str().unwrap();
    let route = format!("/api/issues/{id}");
    assert_eq!(s.ok(&route)["blocked_by"], json!(["p-bbbb"]));
    assert_eq!(s.json("PATCH",&route,json!({"claim":true,"title":"Renamed","description":"Changed description","priority":1,"add_labels":["new"],"remove_labels":["web"],"note":"Change rationale"})).0,200);
    assert_eq!(
        s.json(
            "POST",
            &format!("{route}/notes"),
            json!({"text":"HTTP note"})
        )
        .0,
        200
    );
    let cli = f.cli(&["show", id]);
    assert_eq!(cli["title"], "Renamed");
    assert_eq!(cli["assignee"], "HTTP Tester");
    assert_eq!(cli["status"], "in_progress");
    assert_eq!(cli["labels"], json!(["new"]));
    assert!(
        cli["log"]
            .as_array()
            .unwrap()
            .iter()
            .any(|l| l["event"] == "HTTP note")
    );
    assert_eq!(
        s.json("POST", &format!("{route}/close"), json!({"reason":"done"}))
            .0,
        200
    );
    assert_eq!(f.cli(&["show", id])["status"], "closed");
    assert!(
        s.ok("/api/projects/p/issues")
            .as_array()
            .unwrap()
            .iter()
            .all(|v| v["id"] != id)
    );
    assert_eq!(s.ok("/api/projects/p/issues?status=closed")[0]["id"], id);
    assert_eq!(
        s.json("POST", &format!("{route}/reopen"), Value::Null).0,
        200
    );
    assert_eq!(s.ok(&route)["status"], "open");
    let log = git(&f.hub, &["log", "--format=%B"]);
    assert!(log.contains("Bn-Run:"));
    assert!(log.contains("bn: create"));
    assert_eq!(
        git(&f.hub, &["rev-parse", "HEAD"]),
        git(&f.root.join("remote.git"), &["rev-parse", "main"])
    );
}
#[test]
fn real_http_dependency_parent_child_cycle_validation_and_errors() {
    let f = Fixture::new();
    let s = Server::new(&f);
    assert_eq!(
        s.json(
            "POST",
            "/api/issues/p-bbbb/deps",
            json!({"target":"p-aaaa","type":"parent-child"})
        )
        .0,
        200
    );
    assert_eq!(s.ok("/api/issues/p-aaaa")["children"][0]["id"], "p-bbbb");
    assert_eq!(s.ok("/api/issues/p-bbbb")["parent"], "p-aaaa");
    assert_eq!(
        s.json(
            "DELETE",
            "/api/issues/p-bbbb/deps/p-aaaa?type=parent-child",
            Value::Null
        )
        .0,
        200
    );
    assert_eq!(s.ok("/api/issues/p-bbbb")["parent"], "");
    let (code, value) = s.json(
        "POST",
        "/api/issues/p-aaaa/deps",
        json!({"target":"p-bbbb","type":"blocks"}),
    );
    assert_eq!(code, 409, "{value}");
    assert_eq!(value["error"]["code"], "dependency_cycle");
    assert_eq!(
        s.json("DELETE", "/api/issues/p-bbbb/deps/p-aaaa", Value::Null)
            .0,
        200
    );
    assert_eq!(s.ok("/api/issues/p-bbbb")["blocked_by"], json!([]));
    let (code, response) = s.json(
        "PATCH",
        "/api/issues/p-aaaa",
        json!({"status":"blockedcycle"}),
    );
    assert_eq!(code, 400);
    assert_eq!(response["error"]["code"], "validation_error");
    for (method, path, body, status) in [
        ("PATCH", "/api/issues/nope", "{}", 404),
        ("POST", "/api/projects/p/issues", "{", 400),
        ("POST", "/api/projects/p/issues", "{\"title\":\"\"}", 400),
        ("POST", "/api/issues/p-aaaa/close", "{}", 400),
        (
            "PATCH",
            "/api/issues/p-aaaa",
            "{\"status\":\"recycle\"}",
            400,
        ),
        ("GET", "/api/projects/Bad%20Name/issues", "", 404),
        ("GET", "/api/projects/../issues", "", 404),
    ] {
        let (code, _, body) = s.request(method, path, body);
        assert_eq!(code, status, "{path}: {body}");
        let v: Value = serde_json::from_str(&body).unwrap();
        assert!(v["error"]["message"].is_string());
        if body.contains("recycle") {
            assert_eq!(v["error"]["code"], "validation_error");
        }
    }
}
#[test]
fn real_http_docs_graph_search_assets_spa_and_head_security() {
    let f = Fixture::new();
    let s = Server::new(&f);
    let page = s.ok("/api/docs/projects/p/docs/guide");
    let html = page["html"].as_str().unwrap();
    assert!(html.contains("/issues/p-aaaa"));
    assert!(html.contains("<mark>highlight</mark>"));
    assert!(html.contains("markdown-alert-note"));
    assert!(html.contains("/api/assets/projects/p/docs/img.png"));
    assert!(!html.contains("<script>"));
    assert!(!html.contains("href=\"javascript:"));
    assert!(!html.contains("src=\"data:"));
    assert_eq!(page["toc"][1]["id"], "section");
    assert_eq!(s.ok("/api/docs/tree")["docs"][0]["title"], "Guide");
    let graph = s.ok("/api/graph?all=true");
    assert_eq!(graph["nodes"].as_array().unwrap().len(), 3);
    assert_eq!(graph["edges"][0]["kind"], "blocks");
    assert_eq!(s.ok("/api/search?q=Guide&kind=doc")[0]["kind"], "doc");
    let (code, headers, body) = s.request("GET", "/api/assets/projects/p/docs/my%20image.png", "");
    assert_eq!(code, 200);
    assert!(headers.contains("image/png"));
    assert_eq!(body, "SPACEPNG");
    std::os::unix::fs::symlink(
        f.root.join("remote.git/config"),
        f.hub.join("projects/p/docs/private.png"),
    )
    .unwrap();
    fs::write(f.root.join("private-note.md"), "# privateleakmarker\n").unwrap();
    std::os::unix::fs::symlink(
        f.root.join("private-note.md"),
        f.hub.join("projects/p/docs/private-note.md"),
    )
    .unwrap();
    f.write(
        "projects/p/docs/transclusion.md",
        "# Public transclusion\n\n![[private-note]]\n",
    );
    s.app.refresh().unwrap();
    assert_eq!(s.ok("/api/search?q=privateleakmarker"), json!([]));
    assert_eq!(
        s.request("GET", "/api/docs/projects/p/docs/private-note", "")
            .0,
        404
    );
    assert!(
        !s.ok("/api/docs/projects/p/docs/transclusion")["html"]
            .as_str()
            .unwrap()
            .contains("privateleakmarker")
    );
    for path in [
        "/api/assets/projects/p/docs/private.png",
        "/api/assets/projects/p/issues/p-aaaa-first.md",
        "/api/assets/projects/p/beans.toml",
        "/api/assets/projects/p/docs/../../beans.toml",
        "/api/assets/projects/p/docs/%2e%2e/%2e%2e/beans.toml",
        "/api/assets/projects/p/docs/%252e%252e/beans.toml",
        "/api/nope",
        "/api",
        "/missing.css",
        "/assets/missing.js",
    ] {
        let (code, _, body) = s.request("GET", path, "");
        assert!(code == 400 || code == 404, "{path}: {code} {body}");
        assert!(!body.contains("<html"));
    }
    let (code, headers, body) = s.request("GET", "/wiki/projects/p/docs/guide", "");
    assert_eq!(code, 200);
    assert!(headers.contains("text/html"));
    assert!(body.contains("<html"));
    let (code, headers, body) = s.request("HEAD", "/api/assets/projects/p/docs/my%20image.png", "");
    assert_eq!(code, 200);
    assert!(body.is_empty());
    assert!(headers.contains("image/png"));
    let (code, _, body) = s.request("HEAD", "/issues", "");
    assert_eq!(code, 200);
    assert!(body.is_empty());
}
#[test]
fn real_http_request_relationships_plan_details_and_read_only_routes() {
    let f = Fixture::new();
    let s = Server::new(&f);
    let requests = s.ok("/api/projects/p/requests");
    assert_eq!(requests[0]["id"], "p-r-a3f2");
    assert_eq!(requests[0]["issue_count"], 2);
    let detail = s.ok("/api/requests/p-r-a3f2");
    assert_eq!(detail["issues"][1]["missing"], true);
    assert!(detail["html"].as_str().unwrap().contains("/issues/p-bbbb"));
    assert_eq!(s.ok("/api/issues/p-aaaa")["requests"][0]["id"], "p-r-a3f2");
    assert_eq!(
        s.request(
            "GET",
            "/api/docs/projects/p/requests/p-r-a3f2-first-request",
            ""
        )
        .0,
        404
    );
    let plans = s.ok("/api/projects/p/plans");
    assert_eq!(plans[0]["id"], "p-plan-a3f2");
    assert_eq!(plans[0]["section_count"], 1);
    let detail = s.ok("/api/plans/p-plan-a3f2");
    assert!(
        detail["sections"][0]["html"]
            .as_str()
            .unwrap()
            .contains("Plan details")
    );
    assert_eq!(detail["summary"]["graph"]["nodes"], json!([]));
    assert_eq!(detail["execution"]["plan_id"], "p-plan-a3f2");
    assert_eq!(s.request("POST", "/api/plans/p-plan-a3f2", "{}").0, 405);
    assert_eq!(s.request("POST", "/api/requests/p-r-a3f2", "{}").0, 405);
}
#[test]
fn prompt_http_list_filters_exact_summary_and_errors() {
    let f = Fixture::new();
    let s = Server::new(&f);
    let prompts = s.ok("/api/projects/p/prompts");
    assert_eq!(prompts.as_array().unwrap().len(), 2);
    assert_eq!(prompts[0]["id"], "p-p-a1b2");
    let keys = prompts[0]
        .as_object()
        .unwrap()
        .keys()
        .cloned()
        .collect::<std::collections::BTreeSet<_>>();
    assert_eq!(
        keys,
        [
            "claimed_by",
            "created",
            "from",
            "has_issue",
            "id",
            "labels",
            "path",
            "priority",
            "project",
            "status",
            "title",
            "to",
            "updated"
        ]
        .into_iter()
        .map(str::to_owned)
        .collect()
    );
    assert!(prompts[0]["claimed_by"].is_null());
    assert_eq!(prompts[0]["has_issue"], true);
    assert_eq!(prompts[0]["created"], "2026-01-02T03:04:05.123+01:30");
    assert_eq!(
        s.ok("/api/projects/p/prompts?to=Worker%20Agent")
            .as_array()
            .unwrap()
            .len(),
        2
    );
    assert_eq!(
        s.ok("/api/projects/p/prompts?from=Reviewer")[0]["id"],
        "p-p-c3d4"
    );
    assert_eq!(
        s.ok("/api/projects/p/prompts?claimed_by=Reviewer")[0]["id"],
        "p-p-c3d4"
    );
    assert_eq!(
        s.ok("/api/projects/p/prompts?status=completed")[0]["id"],
        "p-p-b2c3"
    );
    assert_eq!(
        s.ok("/api/projects/p/prompts?terminal=true")
            .as_array()
            .unwrap()
            .len(),
        3
    );
    assert_eq!(
        s.ok("/api/projects/p/prompts?label=urgent")[0]["id"],
        "p-p-a1b2"
    );
    assert_eq!(
        s.ok("/api/projects/p/prompts?priority=2")[0]["id"],
        "p-p-c3d4"
    );
    assert_eq!(
        s.ok("/api/projects/p/prompts?q=NEEDLE")[0]["id"],
        "p-p-c3d4"
    );
    assert_eq!(
        s.ok("/api/projects/_all/prompts?q=other")[0]["project"],
        "q"
    );
    for path in [
        "/api/projects/p/prompts?status=bogus",
        "/api/projects/p/prompts?priority=9",
    ] {
        let (code, value) = s.json("GET", path, Value::Null);
        assert_eq!(code, 400);
        assert_eq!(value["error"]["code"], "validation_error");
    }
    for path in ["/api/projects/nope/prompts", "/api/prompts/nope"] {
        let (code, value) = s.json("GET", path, Value::Null);
        assert_eq!(code, 404);
        assert_eq!(value["error"]["code"], "not_found");
    }
}

#[test]
fn prompt_http_detail_issue_reverse_rendering_search_and_malformed_warning() {
    let f = Fixture::new();
    let s = Server::new(&f);
    let detail = s.ok("/api/prompts/p-p-a1b2");
    assert_eq!(detail["title"], "Review HTTP projection");
    let detail_keys = detail
        .as_object()
        .unwrap()
        .keys()
        .map(String::as_str)
        .collect::<std::collections::BTreeSet<_>>();
    assert_eq!(
        detail_keys,
        [
            "backlinks",
            "claimed_by",
            "created",
            "from",
            "has_issue",
            "html",
            "id",
            "issue",
            "labels",
            "log",
            "outlinks",
            "path",
            "priority",
            "project",
            "status",
            "title",
            "to",
            "toc",
            "updated",
        ]
        .into_iter()
        .collect()
    );
    assert_eq!(
        detail["issue"],
        json!({"id":"p-aaaa","title":"First","missing":false})
    );
    assert!(detail["html"].as_str().unwrap().contains("/issues/p-bbbb"));
    assert!(
        detail["html"]
            .as_str()
            .unwrap()
            .contains("/api/assets/projects/p/docs/img.png")
    );
    assert!(!detail["html"].as_str().unwrap().contains("<script>"));
    assert!(
        detail["toc"]
            .as_array()
            .unwrap()
            .iter()
            .any(|entry| entry["id"] == "prompt-instructions")
    );
    assert!(detail["log"][0]["at"].as_str().unwrap().ends_with('Z'));
    assert_eq!(
        s.ok("/api/prompts/p-p-b2c3")["issue"],
        json!({"id":"missing-a1b2","title":"","missing":true})
    );
    assert!(s.ok("/api/prompts/p-p-c3d4")["issue"].is_null());
    assert_eq!(
        s.ok("/api/issues/p-p-a1b2")["title"],
        "Issue sharing a prompt ID"
    );
    let issue = s.ok("/api/issues/p-aaaa");
    assert_eq!(issue["prompts"][0]["id"], "p-p-a1b2");
    assert!(
        issue["backlinks"]
            .as_array()
            .unwrap()
            .iter()
            .all(|link| link["note_kind"] != "prompt")
    );
    assert_eq!(
        s.ok("/api/search?q=searchable&kind=prompt&project=p")
            .as_array()
            .unwrap()
            .len(),
        1
    );
    assert_eq!(
        s.ok("/api/search?q=searchable&kind=prompt&project=p")[0]["kind"],
        "prompt"
    );
    assert_eq!(
        s.ok("/api/search?q=searchable&kind=prompt&project=q")[0]["project"],
        "q"
    );
    assert!(
        detail["outlinks"]
            .as_array()
            .unwrap()
            .iter()
            .any(|link| link["to"] == "p-bbbb-second")
    );
    assert!(
        s.app
            .index
            .read()
            .unwrap()
            .graph
            .warnings()
            .iter()
            .any(|warning| String::from_utf8_lossy(&warning.path).contains("p-p-bad-malformed.md"))
    );
    let doctor = Command::new(env!("CARGO_BIN_EXE_bn"))
        .args([
            "--hub",
            f.hub.to_str().unwrap(),
            "--json",
            "--no-fetch",
            "doctor",
            "--all-projects",
        ])
        .env("BEANS_HOME", f.root.join("doctor-home"))
        .env_remove("BN_CONFIG")
        .output()
        .unwrap();
    assert!(
        !doctor.status.success(),
        "doctor stdout={} stderr={}",
        String::from_utf8_lossy(&doctor.stdout),
        String::from_utf8_lossy(&doctor.stderr)
    );
    let doctor: Value = serde_json::from_slice(&doctor.stdout).unwrap();
    assert_eq!(doctor["ok"], false);
    assert!(
        doctor["warnings"]
            .as_array()
            .unwrap()
            .iter()
            .any(|warning| {
                warning["path"]
                    .as_str()
                    .is_some_and(|path| path.contains("p-p-bad-malformed.md"))
            })
    );
    assert!(
        s.ok("/api/projects/p/prompts?terminal=true")
            .as_array()
            .unwrap()
            .iter()
            .all(|prompt| prompt["id"] != "p-p-bad")
    );
}

#[test]
fn archived_issue_backlinks_preserve_prompt_deduplication_and_resolution() {
    let f = Fixture::new();
    f.write(
        "projects/p/issues/p-aaaa-first.md",
        "---\nid: p-aaaa\naliases: [p-aaaa, first-alias]\ntitle: First\ntype: task\nstatus: open\npriority: 1\ncreated: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\n---\n",
    );
    f.write(
        "projects/p/prompts/p-p-d4e5-alias.md",
        "---\nid: p-p-d4e5\naliases: [p-p-d4e5]\ntitle: Alias relationship\nstatus: open\npriority: 0\nlabels: [resolution]\nfrom: Lead Agent\nto: Worker Agent\nissue: \"[[first-alias]]\"\ncreated: 2026-02-01T00:00:00Z\nupdated: 2026-02-02T00:00:00Z\n---\nAlias instructions.\n",
    );
    f.write(
        "projects/p/prompts/p-p-e5f6-path.md",
        "---\nid: p-p-e5f6\naliases: [p-p-e5f6]\ntitle: Path relationship\nstatus: open\npriority: 2\nfrom: Lead Agent\nto: Worker Agent\nissue: \"[[projects/p/issues/p-aaaa-first.md]]\"\ncreated: 2026-02-03T00:00:00Z\nupdated: 2026-02-04T00:00:00Z\n---\nPath instructions.\n",
    );
    // This duplicate-ID prompt is indexed and produces a real PromptIssue
    // backlink, but is not an owned prompt and therefore has no derived summary.
    // Its generic relationship must not disappear merely because of its kind.
    f.write(
        "projects/p/prompts/z-shadowed-p-p-c3d4.md",
        "---\nid: p-p-c3d4\naliases: [shadowed-prompt]\ntitle: Shadowed relationship\nstatus: open\npriority: 4\nfrom: Lead Agent\nto: Worker Agent\nissue: \"[[p-aaaa]]\"\ncreated: 2026-02-05T00:00:00Z\nupdated: 2026-02-06T00:00:00Z\n---\nShadowed instructions.\n",
    );
    f.write(
        "projects/p/handoffs/archive/2026/p-handoff1-old.md",
        "---\nid: p-handoff1\ntitle: Archived issue handoff\ncreated: 2026-02-07T00:00:00Z\nupdated: 2026-02-08T00:00:00Z\n---\nContinue [[p-aaaa]].\n",
    );

    let s = Server::new(&f);
    let issue = s.ok("/api/issues/p-aaaa?include_archived_handoffs=true");
    assert_eq!(
        issue["prompts"],
        json!([
            {
                "id": "p-p-d4e5", "title": "Alias relationship", "status": "open",
                "priority": 0, "labels": ["resolution"], "from": "Lead Agent",
                "to": "Worker Agent", "claimed_by": null,
                "created": "2026-02-01T00:00:00Z", "updated": "2026-02-02T00:00:00Z",
                "project": "p", "path": "projects/p/prompts/p-p-d4e5-alias.md",
                "has_issue": true
            },
            {
                "id": "p-p-a1b2", "title": "Review HTTP projection", "status": "open",
                "priority": 1, "labels": ["urgent", "api"], "from": "Lead Agent",
                "to": "Worker Agent", "claimed_by": null,
                "created": "2026-01-02T03:04:05.123+01:30", "updated": "2026-01-03T04:05:06Z",
                "project": "p", "path": "projects/p/prompts/p-p-a1b2-review.md",
                "has_issue": true
            },
            {
                "id": "p-p-e5f6", "title": "Path relationship", "status": "open",
                "priority": 2, "labels": [], "from": "Lead Agent", "to": "Worker Agent",
                "claimed_by": null, "created": "2026-02-03T00:00:00Z",
                "updated": "2026-02-04T00:00:00Z", "project": "p",
                "path": "projects/p/prompts/p-p-e5f6-path.md", "has_issue": true
            }
        ])
    );

    assert!(issue["backlinks"].as_array().unwrap().iter().any(|link| {
        link["from"] == "p-handoff1"
            && link["note_kind"] == "handoff"
            && link["path"] == "projects/p/handoffs/archive/2026/p-handoff1-old.md"
    }));

    let prompt_backlinks = issue["backlinks"]
        .as_array()
        .unwrap()
        .iter()
        .filter(|link| link["kind"] == "prompt_issue")
        .cloned()
        .collect::<Vec<_>>();
    assert_eq!(
        prompt_backlinks,
        vec![json!({
            "from": "p-p-c3d4",
            "kind": "prompt_issue",
            "note_kind": "prompt",
            "path": "projects/p/prompts/z-shadowed-p-p-c3d4.md",
            "title": "Shadowed relationship",
        })]
    );

    assert_eq!(
        s.ok("/api/prompts/p-p-b2c3")["issue"],
        json!({"id":"missing-a1b2","title":"","missing":true})
    );
}

#[test]
fn prompt_http_wikilinks_and_all_mutations_are_read_only_before_decoding() {
    let f = Fixture::new();
    f.write(
        "projects/p/docs/prompt-link.md",
        "# Prompt link\n\n[[p-p-c3d4]]\n",
    );
    let s = Server::new(&f);
    assert!(
        s.ok("/api/docs/projects/p/docs/prompt-link")["html"]
            .as_str()
            .unwrap()
            .contains("href=\"/prompts/p-p-c3d4\"")
    );
    for method in ["POST", "PATCH", "DELETE"] {
        for path in ["/api/projects/p/prompts", "/api/prompts/p-p-a1b2"] {
            for body in ["", "{"] {
                let (code, _, response) = s.request(method, path, body);
                assert_eq!(code, 405, "{method} {path}: {response}");
                let value: Value = serde_json::from_str(&response).unwrap();
                assert_eq!(
                    value,
                    json!({"error":{"code":"method_not_allowed","message":"resource is read-only"}})
                );
            }
        }
    }
}

#[test]
fn real_http_lock_contention_and_git_conflict_are_not_validation_errors() {
    use std::os::fd::AsRawFd;
    let f = Fixture::new();
    let s = Server::new(&f);
    fs::create_dir_all(s.app.hub.state_path()).unwrap();
    let lock = fs::OpenOptions::new()
        .create(true)
        .truncate(false)
        .write(true)
        .open(s.app.hub.state_path().join("hub.lock"))
        .unwrap();
    assert_eq!(
        unsafe { libc::flock(lock.as_raw_fd(), libc::LOCK_EX | libc::LOCK_NB) },
        0
    );
    let original = fs::read(f.hub.join("projects/p/issues/p-aaaa-first.md")).unwrap();
    let (code, response) = s.json(
        "POST",
        "/api/issues/p-aaaa/notes",
        json!({"text":"blocked note"}),
    );
    assert_eq!(code, 423, "{response}");
    assert_eq!(response["error"]["code"], "hub_locked");
    assert_eq!(
        original,
        fs::read(f.hub.join("projects/p/issues/p-aaaa-first.md")).unwrap()
    );
    drop(lock);
    git(&f.hub, &["checkout", "--detach"]);
    let (code, response) = s.json(
        "POST",
        "/api/issues/p-aaaa/notes",
        json!({"text":"detached note"}),
    );
    assert_eq!(code, 409, "{response}");
    assert_eq!(response["error"]["code"], "git_conflict");
    git(&f.hub, &["checkout", "main"]);
    fs::write(f.hub.join(".git/MERGE_HEAD"), "interrupted").unwrap();
    assert_eq!(
        s.json(
            "POST",
            "/api/issues/p-aaaa/notes",
            json!({"text":"merge note"})
        )
        .0,
        409
    );
}

#[test]
fn real_http_embedded_manifest_mime_head_and_missing_file_boundaries() {
    let f = Fixture::new();
    let s = Server::new(&f);
    assert!(
        beans::server::ASSETS
            .iter()
            .any(|(name, _)| *name == "index.html")
    );
    for (name, bytes) in beans::server::ASSETS {
        let path = format!("/{name}");
        let (status, headers, body) = s.request("GET", &path, "");
        assert_eq!(status, 200);
        assert_eq!(body.as_bytes(), *bytes);
        assert!(
            headers
                .to_ascii_lowercase()
                .contains("x-content-type-options: nosniff")
        );
        if name.ends_with(".js") {
            assert!(headers.contains("text/javascript"));
        }
        if name.ends_with(".css") {
            assert!(headers.contains("text/css"));
        }
        let (status, headers, body) = s.request("HEAD", &path, "");
        assert_eq!(status, 200);
        assert!(body.is_empty());
        assert!(
            headers
                .to_ascii_lowercase()
                .contains(&format!("content-length: {}", bytes.len()))
        );
    }
    for name in ["/assets/missing.css", "/api/missing", "/api"] {
        assert_eq!(s.request("GET", name, "").0, 404);
    }
}

#[test]
fn native_http_git_deadline_hook_failure_and_cli_lock_share_state() {
    use std::os::{fd::AsRawFd, unix::fs::PermissionsExt};
    let f = Fixture::new();
    let mut hub = Hub::new(
        f.hub.clone(),
        f.root.join("other-home/cache"),
        "main".into(),
        "HTTP Tester".into(),
        false,
        Duration::ZERO,
    )
    .unwrap();
    hub.executor.policy.lock_timeout = Duration::from_millis(80);
    hub.executor.policy.command_timeout = Duration::from_millis(150);
    let s = Server::with_app(App::new(hub, "p".into()).unwrap());
    let alias = f.root.join("alias");
    std::os::unix::fs::symlink(&f.hub, &alias).unwrap();
    let cli_hub = Hub::new(
        alias,
        f.root.join("cli-home/cache"),
        "main".into(),
        "CLI Tester".into(),
        true,
        Duration::ZERO,
    )
    .unwrap();
    assert_eq!(cli_hub.lock_path(), s.app.hub.lock_path());
    let lock = fs::File::create(cli_hub.lock_path()).unwrap();
    assert_eq!(
        unsafe { libc::flock(lock.as_raw_fd(), libc::LOCK_EX | libc::LOCK_NB) },
        0
    );
    let before = git(&f.hub, &["rev-parse", "HEAD"]);
    let (code, response) = s.json(
        "POST",
        "/api/issues/p-aaaa/notes",
        json!({"text":"must wait"}),
    );
    assert_eq!(code, 423, "{response}");
    assert_eq!(git(&f.hub, &["rev-parse", "HEAD"]), before);
    drop(lock);
    let hook = f.hub.join(".git/hooks/pre-commit");
    fs::write(&hook, "#!/bin/sh\nsleep 10\n").unwrap();
    fs::set_permissions(&hook, fs::Permissions::from_mode(0o755)).unwrap();
    let at = Instant::now();
    let (code, response) = s.json(
        "POST",
        "/api/issues/p-aaaa/notes",
        json!({"text":"retained after timeout"}),
    );
    assert_eq!(code, 504, "{response}");
    assert_eq!(response["error"]["code"], "git_timeout");
    assert!(at.elapsed() < Duration::from_secs(3));
    assert!(cli_hub.journal_path().exists());
    assert_eq!(git(&f.hub, &["rev-parse", "HEAD"]), before);
    fs::remove_file(hook).unwrap();
    let reject = f.root.join("remote.git/hooks/pre-receive");
    fs::write(
        &reject,
        "#!/bin/sh\necho 'hook refusal fetch first' >&2\nexit 1\n",
    )
    .unwrap();
    fs::set_permissions(&reject, fs::Permissions::from_mode(0o755)).unwrap();
    let (code, response) = s.json(
        "POST",
        "/api/issues/p-aaaa/notes",
        json!({"text":"retained hook refusal"}),
    );
    assert_eq!(code, 502, "{response}");
    assert_eq!(response["error"]["code"], "git_error");
    assert!(cli_hub.journal_path().exists());
    fs::remove_file(reject).unwrap();
    s.app.hub.sync().unwrap();
    assert!(!cli_hub.journal_path().exists());
}

#[test]
fn real_cli_and_http_writers_wait_then_preserve_both_notes() {
    use std::os::unix::fs::PermissionsExt;
    let f = Fixture::new();
    let mut hub = Hub::new(
        f.hub.clone(),
        f.root.join("http-home/cache"),
        "main".into(),
        "HTTP Writer".into(),
        false,
        Duration::ZERO,
    )
    .unwrap();
    hub.executor.policy.lock_timeout = Duration::from_secs(2);
    let server = Server::with_app(App::new(hub, "p".into()).unwrap());
    let entered = f.root.join("entered");
    let release = f.root.join("release");
    let overlap = f.root.join("overlap");
    let hook = f.hub.join(".git/hooks/pre-commit");
    fs::write(&hook, format!("#!/bin/sh\nif mkdir '{}' 2>/dev/null; then\n touch '{}'\n while test ! -f '{}'; do sleep 0.01; done\nelse\n touch '{}'\nfi\n", f.root.join("barrier").display(), entered.display(), release.display(), overlap.display())).unwrap();
    fs::set_permissions(hook, fs::Permissions::from_mode(0o755)).unwrap();
    let alias = f.root.join("alias");
    std::os::unix::fs::symlink(&f.hub, &alias).unwrap();
    let mut cli = Command::new(env!("CARGO_BIN_EXE_bn"))
        .args(["--branch", "main", "--no-sync", "--hub"])
        .arg(alias)
        .args(["--project", "p", "note", "p-aaaa", "CLI concurrent note"])
        .env("BEANS_HOME", f.root.join("cli-home"))
        .env("BN_ACTOR", "CLI Writer")
        .env_remove("BEANS_HUB")
        .env_remove("BN_CONFIG")
        .stdout(std::process::Stdio::null())
        .stderr(std::process::Stdio::piped())
        .spawn()
        .unwrap();
    let at = Instant::now();
    while !entered.exists() {
        assert!(at.elapsed() < Duration::from_secs(5));
        std::thread::sleep(Duration::from_millis(10));
    }
    std::thread::scope(|scope| {
        let response = scope.spawn(|| {
            server.json(
                "POST",
                "/api/issues/p-aaaa/notes",
                json!({"text":"HTTP concurrent note"}),
            )
        });
        std::thread::sleep(Duration::from_millis(150));
        assert!(!overlap.exists());
        fs::write(release, b"release").unwrap();
        let (code, body) = response.join().unwrap();
        assert_eq!(code, 200, "{body}");
    });
    let deadline = Instant::now();
    loop {
        if let Some(status) = cli.try_wait().unwrap() {
            assert!(status.success());
            break;
        }
        if deadline.elapsed() > Duration::from_secs(5) {
            cli.kill().unwrap();
            cli.wait().unwrap();
            panic!("CLI writer watchdog");
        }
        std::thread::sleep(Duration::from_millis(10));
    }
    let issue = fs::read_to_string(f.hub.join("projects/p/issues/p-aaaa-first.md")).unwrap();
    assert_eq!(issue.matches("CLI concurrent note").count(), 1);
    assert_eq!(issue.matches("HTTP concurrent note").count(), 1);
    assert_eq!(
        git(&f.hub, &["rev-list", "--count", "origin/main..HEAD"]),
        "0"
    );
}
