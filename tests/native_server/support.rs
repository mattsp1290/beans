use beans::{
    gitops::Hub,
    server::{App, router, watcher},
};
use serde_json::{Value, json};
use std::{
    fs,
    io::{BufRead, BufReader, Read, Write},
    net::{SocketAddr, TcpListener, TcpStream},
    path::{Path, PathBuf},
    process::Command,
    sync::{
        Arc,
        atomic::{AtomicBool, Ordering},
    },
    time::{Duration, Instant},
};
struct Fixture {
    root: PathBuf,
    hub: PathBuf,
}
fn git(dir: &Path, args: &[&str]) -> String {
    let output = Command::new("git")
        .arg("-C")
        .arg(dir)
        .args(args)
        .env("GIT_TERMINAL_PROMPT", "0")
        .output()
        .unwrap();
    assert!(
        output.status.success(),
        "git {args:?}: {}",
        String::from_utf8_lossy(&output.stderr)
    );
    String::from_utf8(output.stdout).unwrap().trim().into()
}
impl Fixture {
    fn new() -> Self {
        let mut random = [0; 8];
        getrandom::fill(&mut random).unwrap();
        let root = std::env::temp_dir().join(format!(
            "bn-http-{}-{}",
            std::process::id(),
            u64::from_ne_bytes(random)
        ));
        let hub = root.join("hub");
        fs::create_dir_all(&hub).unwrap();
        git(&hub, &["init", "-q", "--initial-branch=main"]);
        git(&hub, &["config", "user.name", "HTTP Tester"]);
        git(&hub, &["config", "user.email", "http@example.test"]);
        let remote = root.join("remote.git");
        fs::create_dir_all(&remote).unwrap();
        git(&remote, &["init", "-q", "--bare", "--initial-branch=main"]);
        git(&hub, &["remote", "add", "origin", remote.to_str().unwrap()]);
        let fixture = Self { root, hub };
        beans::vault::create_project_files(&fixture.hub, b"p", b"").unwrap();
        fixture.write("projects/p/issues/p-aaaa-first.md","---\nid: p-aaaa\naliases: [p-aaaa]\ntitle: First\ntype: task\nstatus: open\npriority: 1\ncreated: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\n---\nSee [[guide]] and ![[img.png]].\n\n## Log\n- 2026-01-01T00:00:00Z t: created\n");
        fixture.write("projects/p/issues/p-bbbb-second.md","---\nid: p-bbbb\naliases: [p-bbbb]\ntitle: Second\ntype: task\nstatus: open\npriority: 2\nblocked_by: [\"[[p-aaaa-first]]\"]\ncreated: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\n---\n");
        fixture.write("projects/p/requests/p-r-a3f2-first-request.md","---\nid: p-r-a3f2\naliases: [p-r-a3f2]\ntitle: First request\nstatus: open\npriority: 2\nrequested_by: tester\nissues: [\"[[p-aaaa]]\", \"[[missing-a1b2]]\"]\ncreated: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\n---\nRequest body with [[p-bbbb]].\n\n## Log\n- 2026-01-01T00:00:00Z t: created\n");
        fixture.write("projects/p/docs/guide.md","---\ntitle: Guide\ncustom: example\n---\n# Guide\n\nLinks to [[p-aaaa-first]] and [[missing-page]].\n\n## Section\n\n==highlight==\n\n> [!NOTE]\n> Callout content\n\n<script>alert(1)</script>\n\n[x](javascript:alert(1))\n\n![x](data:text/html,evil)\n\n![[img.png]]\n");
        fixture.write("projects/p/docs/img.png", "PNG");
        fixture.write("projects/p/docs/my image.png", "SPACEPNG");
        fixture.write("projects/p/plans/p-plan-a3f2-add/plan.md","---\nid: p-plan-a3f2\naliases: [p-plan-a3f2]\ntitle: Add plan support\nslug: add\nstatus: draft\ncreated: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\nsections: [sections/context.md]\n---\n## Summary\n\n### Outcome\n\n<!-- bn:todo -->\n\n### Affected areas\n\n<!-- bn:todo -->\n\n### Execution order\n\n<!-- bn:todo -->\n\n### Risks\n\n<!-- bn:todo -->\n\n### Change graph\n\n```bn-change-graph\nversion: 1\nnodes: []\nedges: []\n```\n");
        fixture.write(
            "projects/p/plans/p-plan-a3f2-add/sections/context.md",
            "# Context\n\nPlan details.\n",
        );
        git(&fixture.hub, &["add", "-A"]);
        git(&fixture.hub, &["commit", "-qm", "seed"]);
        git(&fixture.hub, &["push", "-q", "origin", "main"]);
        fixture
    }
    fn write(&self, path: &str, bytes: &str) {
        let path = self.hub.join(path);
        fs::create_dir_all(path.parent().unwrap()).unwrap();
        fs::write(path, bytes).unwrap();
    }
    fn app(&self) -> Arc<App> {
        App::new(
            Hub::new(self.hub.clone(), self.root.join("cache"), "main".into(), "HTTP Tester".into(), false, Duration::ZERO).map(|mut h| { h.executor.policy.lock_timeout = Duration::from_millis(80); h }).unwrap(),
            "p".into(),
        )
        .unwrap()
    }
    fn cli(&self, args: &[&str]) -> Value {
        let output = Command::new(env!("CARGO_BIN_EXE_bn"))
            .args([
                "--hub",
                self.hub.to_str().unwrap(),
                "--project",
                "p",
                "--json",
                "--no-fetch",
            ])
            .args(args)
            .env("BEANS_HOME", self.root.join("home"))
            .env_remove("BN_CONFIG")
            .output()
            .unwrap();
        assert!(
            output.status.success(),
            "{}",
            String::from_utf8_lossy(&output.stderr)
        );
        serde_json::from_slice(&output.stdout).unwrap()
    }
}
impl Drop for Fixture {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.root);
    }
}
struct Server {
    address: SocketAddr,
    app: Arc<App>,
    thread: Option<std::thread::JoinHandle<()>>,
    stop: Arc<AtomicBool>,
    watcher: Option<std::thread::JoinHandle<()>>,
}
impl Server {
    fn new(fixture: &Fixture) -> Self {
        Self::with_app(fixture.app())
    }
    fn with_app(app: Arc<App>) -> Self {
        let listener = TcpListener::bind("127.0.0.1:0").unwrap();
        listener.set_nonblocking(true).unwrap();
        let address = listener.local_addr().unwrap();
        let stop = Arc::new(AtomicBool::new(false));
        let watcher = watcher(app.clone(), stop.clone()).unwrap();
        let server_app = app.clone();
        let thread = std::thread::spawn(move || {
            tokio::runtime::Runtime::new()
                .unwrap()
                .block_on(async move {
                    let mut shutdown = server_app.shutdown.subscribe();
                    axum::serve(
                        tokio::net::TcpListener::from_std(listener).unwrap(),
                        router(server_app),
                    )
                    .with_graceful_shutdown(async move {
                        while !*shutdown.borrow() {
                            if shutdown.changed().await.is_err() {
                                break;
                            }
                        }
                    })
                    .await
                    .unwrap();
                });
        });
        Self {
            address,
            app,
            thread: Some(thread),
            stop,
            watcher: Some(watcher),
        }
    }
    fn request(&self, method: &str, path: &str, body: &str) -> (u16, String, String) {
        let mut stream = TcpStream::connect(self.address).unwrap();
        stream
            .set_read_timeout(Some(Duration::from_secs(10)))
            .unwrap();
        write!(stream,"{method} {path} HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\nContent-Type: application/json\r\nContent-Length: {}\r\n\r\n{body}",body.len()).unwrap();
        let mut response = String::new();
        stream.read_to_string(&mut response).unwrap();
        let (headers, body) = response.split_once("\r\n\r\n").unwrap();
        let code = headers.split_whitespace().nth(1).unwrap().parse().unwrap();
        (code, headers.into(), body.into())
    }
    fn json(&self, method: &str, path: &str, body: Value) -> (u16, Value) {
        let (code, _, response) = self.request(
            method,
            path,
            if body.is_null() {
                String::new()
            } else {
                body.to_string()
            }
            .as_str(),
        );
        (code, serde_json::from_str(&response).unwrap())
    }
    fn ok(&self, path: &str) -> Value {
        let (code, value) = self.json("GET", path, Value::Null);
        assert_eq!(code, 200, "{path}: {value}");
        value
    }
    fn events(&self) -> BufReader<TcpStream> {
        let mut stream = TcpStream::connect(self.address).unwrap();
        stream
            .set_read_timeout(Some(Duration::from_secs(20)))
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
        loop {
            line.clear();
            reader.read_line(&mut line).unwrap();
            if line == "\r\n" {
                break;
            }
        }
        reader
    }
}
impl Drop for Server {
    fn drop(&mut self) {
        self.app.shutdown.send_replace(true);
        self.stop.store(true, Ordering::Relaxed);
        if let Some(w) = self.watcher.take() {
            w.join().unwrap();
        }
        if let Some(t) = self.thread.take() {
            t.join().unwrap();
        }
    }
}
fn until(reader: &mut BufReader<TcpStream>, needle: &str) -> String {
    let mut received = String::new();
    while !received.contains(needle) {
        let mut line = String::new();
        assert!(
            reader.read_line(&mut line).unwrap() > 0,
            "closed before {needle}: {received}"
        );
        received.push_str(&line);
    }
    received
}
