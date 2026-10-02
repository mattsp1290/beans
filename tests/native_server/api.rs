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
    assert_eq!(graph["nodes"].as_array().unwrap().len(), 2);
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
fn real_http_lock_contention_and_git_conflict_are_not_validation_errors() {
    use std::os::fd::AsRawFd;
    let f = Fixture::new();
    let s = Server::new(&f);
    fs::create_dir_all(&s.app.hub.cache).unwrap();
    let lock = fs::OpenOptions::new()
        .create(true)
        .truncate(false)
        .write(true)
        .open(s.app.hub.cache.join("hub.lock"))
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
