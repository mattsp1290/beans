#!/usr/bin/env python3
"""Capture/check the real bn HTTP server, including route variants and SSE."""
import argparse
from datetime import datetime, timezone
import http.client
import json
from pathlib import Path
import socket
import subprocess
import tempfile
import time

from runner import CONTRACT, encode, environment, fixture, git_state, snapshot
from reference import authenticate_capture


def requests():
    reads = ["/api/health", "/api/projects", "/api/projects/alpha/issues",
             "/api/projects/alpha/requests", "/api/projects/alpha/plans", "/api/projects/alpha/ready",
             "/api/issues/alpha-a1b2", "/api/requests/alpha-r-c3d4", "/api/plans/alpha-plan-1234",
             "/api/graph", "/api/docs/tree", "/api/docs/docs/Guide.md", "/api/assets/missing.png",
             "/api/search?q=contract"]
    result = []
    for path in reads:
        for variant in (path, path.split("?", 1)[0] + "/" + ("?" + path.split("?", 1)[1] if "?" in path else ""),
                        path.replace("/api/", "/API/", 1)):
            for method in ("GET", "HEAD"):
                result.append({"method": method, "path": variant, "body": None})
    for method, path, body in (
            ("POST", "/api/projects/alpha/issues", "{}"),
            ("POST", "/api/projects/_all/issues", "{}"),
            ("PATCH", "/api/issues/alpha-a1b2", "{}"),
            ("PATCH", "/api/issues/alpha-a1b2", "{"),
            ("POST", "/api/issues/alpha-a1b2/notes", "{}"),
            ("POST", "/api/issues/alpha-a1b2/close", "{}"),
            ("POST", "/api/issues/alpha-ffff/reopen", "{}"),
            ("POST", "/api/issues/alpha-a1b2/deps", "{}"),
            ("DELETE", "/api/issues/alpha-ffff/deps/alpha-a1b2", None),
            ("GET", "/api/projects/alpha/requests?status=invalid", None),
            ("GET", "/api/projects/alpha/requests?priority=-1", None),
            ("GET", "/api/projects/ALPHA/issues", None),
            ("GET", "/api/issues/ALPHA-A1B2", None),
            ("GET", "/api/docs/docs/guide.md", None),
            ("GET", "/api/docs/docs/Guide.md?include_archived_handoffs=true", None),
            ("GET", "/api/assets/%2e%2e/secret", None),
            ("GET", "/api/unknown", None),
            ("GET", "/API/unknown", None),
            ("GET", "/issues/alpha-a1b2", None),
            ("GET", "/missing.js", None),
            ("GET", "/", None),
            ("GET", "/api/events", None)):
        result.append({"method": method, "path": path, "body": body})
    for i, case in enumerate(result):
        case["id"] = f"http-{i:03}:{case['method']}:{case['path']}"
    return result


def response(port, case, root):
    connection = http.client.HTTPConnection("127.0.0.1", port, timeout=5)
    try:
        headers = {"Content-Type": "application/json"} if case["body"] is not None else {}
        headers.update(case.get("headers", {}))
        connection.request(case["method"], case["path"], body=case["body"], headers=headers)
        res = connection.getresponse()
        data = res.read(13) if case["path"] == "/api/events" else res.read()
        health = case["path"].lower().rstrip("/") == "/api/health"
        if health and case["method"] != "HEAD":
            if json.loads(data)["hub"] != str(root / "hub"):
                raise ValueError("health hub differs from fixture hub")
            if int(res.getheader("Content-Length")) != len(data):
                raise ValueError("health Content-Length differs from actual body")
            data = data.replace(str(root).encode(), b"${FIXTURE_ROOT}")
        headers = []
        for name, value in res.getheaders():
            if name.lower() == "date":
                date = datetime.strptime(value, "%a, %d %b %Y %H:%M:%S GMT").replace(tzinfo=timezone.utc)
                if abs((datetime.now(timezone.utc) - date).total_seconds()) > 30:
                    raise ValueError("invalid transport Date timestamp")
                value = "${TRANSPORT_DATE}"
            if health and name.lower() == "content-length":
                value = str(int(value) - len(str(root).encode()) + len(b"${FIXTURE_ROOT}"))
            headers.append([name.lower(), value])
        # A real socket must emit the initial SSE frame. Closing this connection
        # checks initial streaming; reload/debounce/reconnect remain separate gates.
        return {"status": res.status, "headers": headers, "body_b64": encode(data)}
    finally:
        connection.close()


def execute(binary, corpus):
    with tempfile.TemporaryDirectory(prefix="beans-http-contract-") as directory:
        root = Path(directory)
        env = environment(root)
        fixture(root, corpus.get("fixture", "seeded"), env)
        before = snapshot(root)
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            port = sock.getsockname()[1]
        with tempfile.TemporaryFile() as log:
            process = subprocess.Popen([str(binary), "serve", "--host", "127.0.0.1", "--port", str(port),
                                        "--no-fetch", "--no-sync", "--project", "alpha"],
                                       cwd=root / "cwd", env=env, stdout=log, stderr=log)
            try:
                deadline = time.monotonic() + 10
                while True:
                    if process.poll() is not None:
                        log.seek(0)
                        raise RuntimeError("server exited before readiness: " + log.read().decode(errors="replace"))
                    try:
                        ready = response(port, {"method": "GET", "path": "/api/health", "body": None}, root)
                        if ready["status"] == 200:
                            break
                    except (OSError, http.client.HTTPException):
                        pass
                    if time.monotonic() >= deadline:
                        raise TimeoutError("server readiness deadline")
                    time.sleep(0.02)
                results = [response(port, case, root) for case in corpus["cases"]]
            finally:
                process.terminate()
                try:
                    process.wait(timeout=7)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()
                    raise TimeoutError("server did not stop within its shutdown deadline")
        after = snapshot(root)
        state = {"changed_files": {p: v for p, v in after.items() if before.get(p) != v},
                 "removed_files": sorted(set(before) - set(after)), "git": git_state(root, env),
                 "server_exit": process.returncode}
        return results, state


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=["capture", "check"])
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--corpus", type=Path, default=CONTRACT / "http.json")
    parser.add_argument("--fixture", choices=["seeded", "broken-seeded"], default="seeded")
    args = parser.parse_args()
    binary = args.binary.resolve(strict=True)
    if args.mode == "capture":
        authenticate_capture(binary)
    corpus = {"schema": "beans-http-contract-v1", "cases": requests()} if args.mode == "capture" else json.loads(args.corpus.read_text())
    if args.mode == "capture" and args.fixture != "seeded":
        corpus["fixture"] = args.fixture
    results, state = execute(binary, corpus)
    if args.mode == "capture":
        for case, result in zip(corpus["cases"], results, strict=True):
            case["expected"] = result
        corpus["post_state"] = state
        args.corpus.write_text(json.dumps(corpus, indent=2) + "\n")
        print(f"Captured {len(results)} HTTP cases")
    else:
        failures = [c["id"] for c, r in zip(corpus["cases"], results, strict=True) if c["expected"] != r]
        if state != corpus["post_state"]:
            failures.append("post_state")
        if failures:
            parser.exit(1, "HTTP mismatches:\n" + "\n".join(failures) + "\n")
        print(f"Passed {len(results)} HTTP cases")


if __name__ == "__main__":
    main()
