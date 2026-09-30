#!/usr/bin/env python3
"""Characterize SIGINT/SIGTERM on a live server after socket readiness."""
import argparse
from datetime import datetime, timezone
import json
from pathlib import Path
import re
import signal
import socket
import subprocess
import tempfile
import time
import http.client

from http_runner import response
from reference import authenticate_capture
from runner import CONTRACT, encode, environment, fixture, git_state, snapshot


def execute(binary, name):
    with tempfile.TemporaryDirectory(prefix="beans-signal-") as directory:
        root = Path(directory)
        env = environment(root)
        fixture(root, "seeded", env)
        before = snapshot(root)
        start = datetime.now(timezone.utc)
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            port = sock.getsockname()[1]
        with tempfile.TemporaryFile() as stdout, tempfile.TemporaryFile() as stderr:
            process = subprocess.Popen([str(binary), "serve", "--host", "127.0.0.1", "--port", str(port),
                                        "--project", "alpha", "--no-fetch"], cwd=root / "cwd", env=env,
                                       stdout=stdout, stderr=stderr)
            try:
                deadline = time.monotonic() + 10
                while True:
                    if process.poll() is not None:
                        raise RuntimeError("server exited before signal readiness")
                    try:
                        if response(port, {"method": "GET", "path": "/api/health", "body": None}, root)["status"] == 200:
                            break
                    except (OSError, http.client.HTTPException):
                        pass
                    if time.monotonic() >= deadline:
                        raise TimeoutError("signal readiness deadline")
                    time.sleep(0.02)
                process.send_signal(getattr(signal, name))
                process.wait(timeout=7)
            finally:
                if process.poll() is None:
                    process.kill()
                    process.wait()
            end = datetime.now(timezone.utc)
            stdout.seek(0)
            stderr.seek(0)
            out = stdout.read().replace(str(root).encode(), b"${FIXTURE_ROOT}")
            out = out.replace(f"http://127.0.0.1:{port}/".encode(), b"${SERVER_URL}")
            err = stderr.read().decode()
            log = re.fullmatch(r"(\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ) 200 GET /api/health ( *[0-9]+(?:\.[0-9]+)?(?:ns|µs|ms|s))\n", err)
            if log is None:
                raise ValueError("unexpected server log framing: " + repr(err))
            if log[2] != log[2].lstrip().rjust(13):
                raise ValueError("request latency does not use Go's 13-column field")
            instant = datetime.fromisoformat(log[1])
            if not start.replace(microsecond=0) <= instant <= end:
                raise ValueError("request log timestamp outside server lifetime")
            err = err.replace(log[1], "${REQUEST_TIME}").replace(log[2], "${REQUEST_LATENCY}")
        after = snapshot(root)
        return {"exit": process.returncode, "stdout_b64": encode(out), "stderr_b64": encode(err.encode()),
                "changed_files": {k: v for k, v in after.items() if before.get(k) != v},
                "removed_files": sorted(set(before) - set(after)), "git": git_state(root, env)}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=["capture", "check"])
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--corpus", type=Path, default=CONTRACT / "signals.json")
    args = parser.parse_args()
    binary = args.binary.resolve(strict=True)
    if args.mode == "capture":
        authenticate_capture(binary)
        corpus = {"schema": "beans-signal-contract-v1", "cases": [{"signal": s} for s in ("SIGINT", "SIGTERM")]}
    else:
        corpus = json.loads(args.corpus.read_text())
    failures = []
    for case in corpus["cases"]:
        result = execute(binary, case["signal"])
        if args.mode == "capture":
            case["expected"] = result
        elif result != case["expected"]:
            failures.append(case["signal"])
    if args.mode == "capture":
        args.corpus.write_text(json.dumps(corpus, indent=2) + "\n")
        print("Captured SIGINT/SIGTERM contracts")
    elif failures:
        parser.exit(1, "Signal mismatches: " + ", ".join(failures) + "\n")
    else:
        print("Passed SIGINT/SIGTERM contracts")


if __name__ == "__main__":
    main()
