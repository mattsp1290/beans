use super::*;

#[test]
fn real_http_external_watcher_sse_reconnect_heartbeat_and_shutdown() {
    let f = Fixture::new();
    let s = Server::new(&f);
    let mut stream = s.events();
    f.write(
        "projects/p/docs/external.md",
        "# Externally added\n\nwatcherneedle\n",
    );
    let event = until(&mut stream, "event: reload");
    assert!(event.contains("reload"));
    assert_eq!(
        s.ok("/api/search?q=watcherneedle")[0]["title"],
        "Externally added"
    );
    drop(stream);
    let mut reconnect = s.events();
    until(&mut reconnect, "heartbeat");
    f.write(
        "projects/p/docs/external.md",
        "# Externally changed\n\nchangedneedle\n",
    );
    until(&mut reconnect, "event: reload");
    assert_eq!(
        s.ok("/api/search?q=changedneedle")[0]["title"],
        "Externally changed"
    );
    let start = Instant::now();
    s.app.shutdown.send_replace(true);
    let mut remainder = String::new();
    reconnect.read_to_string(&mut remainder).unwrap();
    assert!(start.elapsed() < Duration::from_secs(2));
}
#[test]
fn native_server_startup_failure_and_signals_close_active_sse() {
    let f = Fixture::new();
    let occupied = TcpListener::bind("127.0.0.1:0").unwrap();
    let port = occupied.local_addr().unwrap().port().to_string();
    let output = Command::new(env!("CARGO_BIN_EXE_bn"))
        .args(["--hub", f.hub.to_str().unwrap(), "serve", "--port", &port])
        .env("BEANS_HOME", f.root.join("home"))
        .output()
        .unwrap();
    assert!(!output.status.success());
    for signal in [libc::SIGINT, libc::SIGTERM] {
        let listener = TcpListener::bind("127.0.0.1:0").unwrap();
        let address = listener.local_addr().unwrap();
        drop(listener);
        let mut child = Command::new(env!("CARGO_BIN_EXE_bn"))
            .args([
                "--hub",
                f.hub.to_str().unwrap(),
                "--no-sync",
                "serve",
                "--port",
                &address.port().to_string(),
            ])
            .env("BEANS_HOME", f.root.join("home"))
            .current_dir(&f.root)
            .stderr(std::process::Stdio::null())
            .spawn()
            .unwrap();
        let start = Instant::now();
        let mut stream = loop {
            if let Ok(s) = TcpStream::connect(address) {
                break s;
            }
            assert!(start.elapsed() < Duration::from_secs(5));
            std::thread::sleep(Duration::from_millis(20));
        };
        stream
            .set_read_timeout(Some(Duration::from_secs(3)))
            .unwrap();
        write!(
            stream,
            "GET /api/events HTTP/1.1\r\nHost: localhost\r\n\r\n"
        )
        .unwrap();
        let mut reader = BufReader::new(stream);
        let mut line = String::new();
        reader.read_line(&mut line).unwrap();
        assert!(line.contains("200"));
        unsafe {
            libc::kill(child.id() as i32, signal);
        }
        let deadline = Instant::now();
        let status = loop {
            if let Some(status) = child.try_wait().unwrap() {
                break status;
            }
            if deadline.elapsed() > Duration::from_secs(3) {
                child.kill().unwrap();
                panic!("server did not drain SSE on signal");
            }
            std::thread::sleep(Duration::from_millis(20));
        };
        assert!(status.success());
        let mut remaining = String::new();
        reader.read_to_string(&mut remaining).unwrap();
    }
}
#[test]
fn native_watcher_config_new_directory_burst_plan_updates_and_removal() {
    let f = Fixture::new();
    let s = Server::new(&f);
    let baseline = *s.app.reload.borrow();
    for name in ["burst-a", "burst-b", "burst-c"] {
        f.write(
            &format!("projects/p/docs/{name}.md"),
            &format!("# {name}\n"),
        );
        std::thread::sleep(Duration::from_millis(10));
    }
    std::thread::sleep(Duration::from_millis(500));
    assert_eq!(
        *s.app.reload.borrow() - baseline,
        1,
        "burst must yield one debounced index rebuild"
    );
    for name in ["burst-a", "burst-b", "burst-c"] {
        assert_eq!(s.ok(&format!("/api/search?q={name}"))[0]["title"], name);
    }
    let baseline = *s.app.reload.borrow();
    f.write(
        "projects/p/beans.toml",
        "name = \"p\"\nprefix = \"p\"\n[workflow]\nactive = [\"open\", \"blocked\"]\n",
    );
    f.write(
        "projects/p/docs/new-directory/late.md",
        "# Newly watched directory\n",
    );
    wait_reload(&s, baseline);
    assert!(
        s.ok("/api/projects")[0]["workflow"]["active"]
            .as_array()
            .unwrap()
            .contains(&json!("blocked"))
    );
    assert_eq!(
        s.ok("/api/search?q=Newly+watched")[0]["title"],
        "Newly watched directory"
    );
    let baseline = *s.app.reload.borrow();
    f.write(
        "projects/p/plans/p-plan-a3f2-add/sections/context.md",
        "# Updated context\n\nSection watcher proof.\n",
    );
    wait_reload(&s, baseline);
    assert!(
        s.ok("/api/plans/p-plan-a3f2")["sections"][0]["html"]
            .as_str()
            .unwrap()
            .contains("Section watcher proof")
    );
    let baseline = *s.app.reload.borrow();
    fs::remove_dir_all(f.hub.join("projects/p/plans/p-plan-a3f2-add")).unwrap();
    wait_reload(&s, baseline);
    assert_eq!(s.request("GET", "/api/plans/p-plan-a3f2", "").0, 404);
    assert_eq!(s.ok("/api/projects/p/plans"), json!([]));
}
fn wait_reload(s: &Server, baseline: u64) {
    let deadline = Instant::now();
    while *s.app.reload.borrow() == baseline {
        assert!(
            deadline.elapsed() < Duration::from_secs(3),
            "watcher failed to publish reload"
        );
        std::thread::sleep(Duration::from_millis(10));
    }
}
#[test]
fn native_snapshot_reload_and_reads_are_serialized_without_recovery() {
    let f = Fixture::new();
    let app = f.app();
    f.write(
        "projects/p/plans/.p-plan-a3f2-add.backup/file",
        "backup must remain\n",
    );
    let threads: Vec<_> = (0..4)
        .map(|_| {
            let app = app.clone();
            std::thread::spawn(move || {
                for _ in 0..100 {
                    let index = app.index.read().unwrap();
                    assert_eq!(index.ready(b"p", false).len(), 1);
                    assert_eq!(index.lookup(b"guide").unwrap().title, b"Guide");
                }
            })
        })
        .collect();
    for _ in 0..20 {
        app.refresh().unwrap();
    }
    for thread in threads {
        thread.join().unwrap();
    }
    assert_eq!(
        fs::read_to_string(f.hub.join("projects/p/plans/.p-plan-a3f2-add.backup/file")).unwrap(),
        "backup must remain\n"
    );
    let output = Command::new(env!("CARGO_BIN_EXE_bn"))
        .arg("--version")
        .output()
        .unwrap();
    assert!(output.status.success());
    assert_eq!(
        String::from_utf8(output.stdout).unwrap().trim(),
        format!("bn {}", env!("BN_VERSION"))
    );
    assert!(!env!("BN_VERSION").is_empty());
}
