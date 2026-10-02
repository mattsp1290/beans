//! Review-discovered HTTP/rendering contracts, asserted independently of domains.
use super::*;
#[test]
fn http_timestamp_strings_preserve_fraction_and_offset_without_domain_changes() {
    let f = Fixture::new();
    let timestamp = "2026-01-01T05:45:06.123456789+05:45";
    for path in [
        "projects/p/issues/p-aaaa-first.md",
        "projects/p/requests/p-r-a3f2-first-request.md",
    ] {
        let original = fs::read_to_string(f.hub.join(path)).unwrap();
        f.write(path, &original.replace("2026-01-01T00:00:00Z", timestamp));
    }
    let plan = "projects/p/plans/p-plan-a3f2-add/plan.md";
    let plan_timestamp = "2026-01-01T00:00:06.123456789Z";
    f.write(
        plan,
        &fs::read_to_string(f.hub.join(plan))
            .unwrap()
            .replace("2026-01-01T00:00:00Z", plan_timestamp),
    );
    let server = Server::new(&f);
    for (list, detail, id, expected) in [
        (
            "/api/projects/p/issues",
            "/api/issues/p-aaaa",
            "p-aaaa",
            timestamp,
        ),
        (
            "/api/projects/p/requests",
            "/api/requests/p-r-a3f2",
            "p-r-a3f2",
            timestamp,
        ),
        (
            "/api/projects/p/plans",
            "/api/plans/p-plan-a3f2",
            "p-plan-a3f2",
            plan_timestamp,
        ),
    ] {
        let list = server.ok(list);
        let listed = list
            .as_array()
            .unwrap()
            .iter()
            .find(|value| value["id"] == id)
            .unwrap();
        let detail = server.ok(detail);
        for value in [listed, &detail] {
            for field in ["created", "updated"] {
                assert_eq!(value[field], expected);
                let parsed = time::OffsetDateTime::parse(
                    value[field].as_str().unwrap(),
                    &time::format_description::well_known::Rfc3339,
                )
                .unwrap();
                assert_eq!(parsed.nanosecond(), 123_456_789);
            }
            if let Some(log) = value["log"].as_array() {
                assert!(!log.is_empty());
                for entry in log {
                    assert_eq!(entry["at"], expected);
                    time::OffsetDateTime::parse(
                        entry["at"].as_str().unwrap(),
                        &time::format_description::well_known::Rfc3339,
                    )
                    .unwrap();
                }
            }
        }
    }
    let cli = f.cli(&["show", "p-aaaa"]);
    assert!(cli["created"].is_object());
    assert!(cli["log"][0]["at"].is_object());
    assert_eq!(cli["created"]["offset_seconds"], 20_700);
    let (status, created) = server.json(
        "POST",
        "/api/projects/p/issues",
        json!({"title":"HTTP wire clock"}),
    );
    assert_eq!(status, 201);
    let detail = server.ok(&format!("/api/issues/{}", created["id"].as_str().unwrap()));
    for value in [
        &detail["created"],
        &detail["updated"],
        &detail["log"][0]["at"],
    ] {
        time::OffsetDateTime::parse(
            value.as_str().unwrap(),
            &time::format_description::well_known::Rfc3339,
        )
        .unwrap();
    }
}
#[test]
fn http_request_body_query_keeps_native_trimmed_search_matches() {
    let f = Fixture::new();
    let path = "projects/p/requests/p-r-a3f2-first-request.md";
    f.write(
        path,
        &fs::read_to_string(f.hub.join(path)).unwrap().replace(
            "Request body with [[p-bbbb]].",
            "Request body with [[p-bbbb]].\n\nneedleonlybody exists only here.",
        ),
    );
    let server = Server::new(&f);
    assert!(
        !server.ok("/api/projects/p/requests")[0]
            .to_string()
            .contains("needleonlybody")
    );
    for query in [
        "needleonlybody",
        "%20%20needleonlybody%20%20",
        "%20%09needleonlybody%0A%20",
        "NEEDLEONLYBODY",
    ] {
        let results = server.ok(&format!("/api/projects/p/requests?q={query}"));
        assert_eq!(results.as_array().unwrap().len(), 1, "query {query}");
        assert_eq!(results[0]["id"], "p-r-a3f2");
    }
    assert_eq!(
        server.ok("/api/projects/p/requests?q=absentbody"),
        json!([])
    );
}
#[test]
fn http_local_markdown_images_decode_once_and_fetch_indexed_space_paths() {
    let f = Fixture::new();
    f.write("projects/p/docs/space-images.md","# Images\n\n![Encoded](my%20image.png)\n\n![Angle](<my image.png>)\n\n![Traversal](%2e%2e/my%20image.png)\n\n![Double](my%2520image.png)\n\n![Separator](docs%2fmy%20image.png)\n\n![Scheme](javascript%3aalert)\n");
    let server = Server::new(&f);
    let page = server.ok("/api/docs/projects/p/docs/space-images");
    let html = page["html"].as_str().unwrap();
    assert_eq!(
        html.matches("src=\"/api/assets/projects/p/docs/my%20image.png\"")
            .count(),
        2,
        "{html}"
    );
    for tag in html.split("<img ").skip(1) {
        let (attributes, _) = tag.split_once('>').unwrap();
        let destination = attributes
            .split_once("src=\"")
            .unwrap()
            .1
            .split('"')
            .next()
            .unwrap();
        if attributes.contains("alt=\"Encoded\"") || attributes.contains("alt=\"Angle\"") {
            let (status, headers, body) = server.request("GET", destination, "");
            assert_eq!(status, 200);
            assert!(headers.contains("image/png"));
            assert_eq!(body, "SPACEPNG");
        } else {
            assert!(
                !destination.starts_with("/api/assets/"),
                "unsafe destination was rewritten: {attributes}"
            );
        }
    }
    for path in [
        "/api/assets/projects/p/docs/%2e%2e/my%20image.png",
        "/api/assets/projects/p/docs/my%2520image.png",
        "/api/assets/projects/p/docs/docs%252fmy%20image.png",
    ] {
        assert!(matches!(server.request("GET", path, "").0, 400 | 404));
    }
}
#[test]
fn http_hub_svg_is_sandboxed_with_script_and_connect_disabled() {
    let f = Fixture::new();
    let svg = "<svg xmlns=\"http://www.w3.org/2000/svg\" width=\"120\" height=\"30\"><script>fetch('/api/issues/p-aaaa/notes',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({text:'svg-unauthorized'})})</script><rect width=\"120\" height=\"30\" fill=\"green\"/><text y=\"20\">Useful diagram</text></svg>";
    f.write("projects/p/docs/diagram.svg", svg);
    f.write("projects/p/docs/diagram.SVG", svg);
    let server = Server::new(&f);
    for method in ["GET", "HEAD"] {
        let (status, headers, body) =
            server.request(method, "/api/assets/projects/p/docs/diagram.svg", "");
        assert_eq!(status, 200);
        assert!(headers.contains("image/svg+xml"));
        let policy = headers
            .lines()
            .find(|line| {
                line.to_ascii_lowercase()
                    .starts_with("content-security-policy:")
            })
            .unwrap();
        for directive in [
            "sandbox",
            "default-src 'none'",
            "script-src 'none'",
            "connect-src 'none'",
            "base-uri 'none'",
            "form-action 'none'",
        ] {
            assert!(policy.contains(directive), "{policy}");
        }
        assert!(!policy.contains("allow-scripts"));
        assert!(!policy.contains("allow-same-origin"));
        if method == "GET" {
            assert_eq!(body, svg);
        } else {
            assert!(body.is_empty());
        }
    }
    assert!(
        !server
            .request("GET", "/index.html", "")
            .1
            .to_ascii_lowercase()
            .contains("content-security-policy:")
    );
    let (status, headers, body) =
        server.request("GET", "/api/assets/projects/p/docs/diagram.SVG", "");
    assert_eq!(status, 200);
    assert!(headers.contains("image/svg+xml"));
    assert!(
        headers
            .to_ascii_lowercase()
            .contains("content-security-policy:")
    );
    assert_eq!(body, svg);
}
