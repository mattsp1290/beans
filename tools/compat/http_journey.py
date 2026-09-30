#!/usr/bin/env python3
"""Capture successful HTTP writes against a real server and local Git remote."""
import argparse
import base64
from datetime import datetime, timezone
import http.client
import json
from pathlib import Path
import re
import socket
import subprocess
import tempfile
import time

from http_runner import response
from journey import Identities, git, setup
from reference import authenticate_capture
from runner import CONTRACT, encode, environment


def steps():
    cases = [
        ("POST", "/api/projects/alpha/issues", {"title": "HTTP journey", "description": "Keep café & <HTML>.\n\nA date 2024-01-01T00:00:00Z stays.", "labels": ["one,two", "three"], "priority": 1}),
        ("GET", "/api/issues/${ISSUE}", None),
        ("HEAD", "/api/issues/${ISSUE}", None),
        ("PATCH", "/api/issues/${ISSUE}", {"claim": True, "note": "Claim through HTTP", "add_labels": ["api"]}),
        ("POST", "/api/issues/${ISSUE}/notes", {"text": "HTTP note café & <HTML>"}),
        ("POST", "/api/issues/${ISSUE}/deps", {"target": "alpha-0000"}),
        ("GET", "/api/projects/alpha/ready", None),
        ("GET", "/api/graph?project=alpha", None),
        ("GET", "/api/issues/${ISSUE}", None),
        ("DELETE", "/api/issues/${ISSUE}/deps/alpha-0000", None),
        ("POST", "/api/issues/${ISSUE}/close", {"reason": "HTTP journey complete"}),
        ("POST", "/api/issues/${ISSUE}/close", {"reason": "Idempotent close"}),
        ("GET", "/api/projects/alpha/issues?status=closed", None),
        ("POST", "/api/issues/${ISSUE}/reopen", {}),
        ("GET", "/api/issues/${ISSUE}", None),
        ("HEAD", "/api/issues/${ISSUE}", None),
        ("PATCH", "/api/issues/${ISSUE}", {"title": "HTTP final title", "priority": 0, "description": "Final body with ==highlight==", "remove_labels": ["api"]}),
        ("GET", "/api/projects/alpha/issues?q=HTTP", None),
        ("GET", "/api/search?q=HTTP&project=alpha", None),
        ("GET", "/api/issues/${ISSUE}", None),
    ]
    return [{"id": f"http-write-{i:02}:{method}:{path}", "method": method, "path": path,
             "body": json.dumps(body, ensure_ascii=False, separators=(",", ":")) if body is not None else None,
             "bind": "ISSUE" if i == 0 else None} for i, (method, path, body) in enumerate(cases)]


def html_output(identities, data):
    # HTML embeds owned Log sections. Restrict substitution to this section;
    # user dates elsewhere in rendered content remain byte-exact.
    text = data.decode()
    def html(match):
        encoded = match[2]
        rendered = json.loads(encoded)
        section = re.search(r'<h2 id="log">Log</h2>\n', rendered)
        if section is not None:
            following = re.search(r"<h2 ", rendered[section.end():])
            end = section.end() + following.start() if following else len(rendered)
            log = rendered[section.end():end]
            encoded_section = re.search(r'(?:<|\\u003c)h2 id=\\"log\\"(?:>|\\u003e)Log(?:<|\\u003c)/h2(?:>|\\u003e)\\n', encoded)
            if encoded_section is None:
                raise ValueError("cannot locate the encoded owned Log section")
            next_heading = re.search(r'(?:<|\\u003c)h2 ', encoded[encoded_section.end():])
            encoded_end = encoded_section.end() + next_heading.start() if next_heading else len(encoded)
            encoded_log = encoded[encoded_section.end():encoded_end]
            for stamp in re.findall(r"<li>(\d{4}-\d\d-\d\dT\S+) ", log):
                token = identities.timestamp(stamp)
                # All stamps are ASCII and appear identically in the JSON
                # string. Preserve the original JSON escaping of all HTML.
                encoded_log = encoded_log.replace(stamp, token)
            encoded = encoded[:encoded_section.end()] + encoded_log + encoded[encoded_end:]
        return match[1] + encoded
    text = re.sub(r'("html":\s*)("(?:[^"\\]|\\.)*")', html, text)
    return base64.b64decode(identities.output(text.encode()))


def normalized_response(port, case, root, env, identities):
    result = response(port, case, root)
    if result["status"] != 200:
        raise ValueError(f"successful HTTP journey returned status {result['status']}")
    identities.end = datetime.now(timezone.utc)
    data = base64.b64decode(result["body_b64"])
    lengths = [int(v) for k, v in result["headers"] if k == "content-length"]
    if case["method"] == "HEAD":
        if data:
            raise ValueError("HEAD response contains a body")
        # Verify the HEAD length against a real GET before substituting any
        # dynamic fields. Both requests are reads with fetch disabled.
        companion = response(port, dict(case, method="GET"), root)
        raw = base64.b64decode(companion["body_b64"])
        if lengths != [len(raw)]:
            raise ValueError("HEAD length differs from the GET representation")
        normalized = html_output(identities, raw)
    else:
        if lengths != [len(data)]:
            raise ValueError("HTTP Content-Length differs from actual body")
        if case.get("bind"):
            identities.bind(case["bind"], json.loads(data)["id"], "alpha-")
        identities.history(root, env)
        metadata = identities.metadata()
        identities.validate_output(data, metadata)
        decoded = json.loads(data)
        ordered = decoded.get("nodes") if isinstance(decoded, dict) and case["path"].startswith("/api/graph") else decoded if case["method"] == "GET" and case["path"].startswith("/api/projects/alpha/issues") else None
        if ordered is not None:
            ids = [value["id"] for value in ordered]
            if ids != sorted(ids):
                raise ValueError("HTTP issues/graph are not ordered by actual ID")
        normalized = html_output(identities, data)
        result["body_b64"] = encode(normalized)
    result["headers"] = [[k, str(len(normalized)) if k == "content-length" else v]
                         for k, v in result["headers"]]
    return result


def execute(binary, corpus):
    with tempfile.TemporaryDirectory(prefix="beans-http-writes-") as directory:
        root = Path(directory)
        env = environment(root)
        setup(root, env)
        # These are the smallest valid four-character issue hashes. Every
        # fresh generated issue ID sorts after them (existing IDs cannot be
        # reused), keeping ordering deterministic without reordering responses.
        replacements = {"alpha-a1b2": "alpha-0000", "alpha-789a": "alpha-0001", "alpha-9999": "alpha-0002"}
        for path in list((root / "hub").rglob("*.md")):
            text, name = path.read_text(), path.name
            for old, new in replacements.items():
                text, name = text.replace(old, new), name.replace(old, new)
            path.write_text(text)
            if name != path.name:
                path.rename(path.with_name(name))
        git(root / "hub", env, "add", ".")
        git(root / "hub", env, "commit", "-m", "HTTP boundary ID fixture")
        git(root / "hub", env, "push", "origin", "main")
        identities = Identities(root)
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            port = sock.getsockname()[1]
        with tempfile.TemporaryFile() as log:
            argv = [str(binary), "serve", "--host", "127.0.0.1", "--port", str(port), "--no-fetch", "--project", "alpha"]
            if not corpus["push_each"]:
                argv.append("--no-sync")
            process = subprocess.Popen(argv, cwd=root / "cwd", env=env, stdout=log, stderr=log)
            try:
                deadline = time.monotonic() + 10
                while True:
                    if process.poll() is not None:
                        log.seek(0)
                        raise RuntimeError("server exited before readiness: " + log.read().decode())
                    try:
                        if response(port, {"method": "GET", "path": "/api/health", "body": None}, root)["status"] == 200:
                            break
                    except (OSError, http.client.HTTPException):
                        pass
                    if time.monotonic() > deadline:
                        raise TimeoutError("server readiness deadline")
                    time.sleep(0.02)
                results = []
                for item in corpus["steps"]:
                    case = dict(item, path=identities.argument(item["path"]))
                    result = normalized_response(port, case, root, env, identities)
                    results.append({"response": result, "git": identities.history(root, env),
                                    "files": identities.files(root)})
            finally:
                process.terminate()
                try:
                    process.wait(timeout=7)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()
                    raise TimeoutError("server shutdown deadline")
        return results, process.returncode


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=["capture", "check"])
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--corpus", type=Path, default=CONTRACT / "http-journey.json")
    parser.add_argument("--push-each", action="store_true")
    args = parser.parse_args()
    binary = args.binary.resolve(strict=True)
    if args.mode == "capture":
        authenticate_capture(binary)
    corpus = {"schema": "beans-http-write-journey-v1", "push_each": args.push_each, "steps": steps()} if args.mode == "capture" else json.loads(args.corpus.read_text())
    results, code = execute(binary, corpus)
    if args.mode == "capture":
        for case, result in zip(corpus["steps"], results, strict=True):
            case["expected"] = result
        corpus["server_exit"] = code
        args.corpus.write_text(json.dumps(corpus, indent=2) + "\n")
        print(f"Captured {len(results)} HTTP mutation steps")
    else:
        failures = [case["id"] for case, result in zip(corpus["steps"], results, strict=True) if case["expected"] != result]
        if code != corpus["server_exit"]:
            failures.append("server exit")
        if failures:
            parser.exit(1, "HTTP mutation mismatches:\n" + "\n".join(failures) + "\n")
        print(f"Passed {len(results)} HTTP mutation steps")


if __name__ == "__main__":
    main()
