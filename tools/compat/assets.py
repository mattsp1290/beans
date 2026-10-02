#!/usr/bin/env python3
"""Compare every full-build embedded UI asset and native HTTP delivery behavior."""
import argparse
import base64
import hashlib
import json
from pathlib import Path

from http_runner import execute
from reference import authenticate_capture
from runner import CONTRACT, ROOT


def requests(manifest):
    result = []
    for name in manifest:
        path = "/" + name
        for variant in (path, path + "?cache=contract", path.upper(), path + "/"):
            for method in ("GET", "HEAD"):
                result.append({"method": method, "path": variant, "body": None})
        result.append({"method": "GET", "path": path, "body": None,
                       "headers": {"Range": "bytes=0-31"}})
    for path in ("/", "/issues/alpha-a1b2", "/deep/wiki/route", "/assets/missing.js",
                 "/missing.css", "/api/unknown", "/API/unknown"):
        for method in ("GET", "HEAD"):
            result.append({"method": method, "path": path, "body": None})
    for i, case in enumerate(result):
        case["id"] = f"asset-{i:03}:{case['method']}:{case['path']}"
    return result


def summarize(case, result, manifest):
    data = base64.b64decode(result["body_b64"], validate=True)
    headers = dict(result["headers"])
    if case["method"] == "HEAD":
        if data:
            raise ValueError("asset HEAD response contains a body")
    elif "content-length" in headers and int(headers["content-length"]) != len(data):
        raise ValueError("asset Content-Length differs from actual bytes")
    digest = hashlib.sha256(data).hexdigest()
    name = case["path"].removeprefix("/")
    if case["method"] == "GET" and name in manifest and "headers" not in case:
        if result["status"] != 200 or digest != manifest[name]:
            raise ValueError("embedded asset differs from pinned UI build: " + name)
    # A digest compares all bytes without committing compiled JS/CSS artifacts.
    return {"status": result["status"], "headers": result["headers"],
            "body_bytes": len(data), "body_sha256": digest}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=["capture", "check"])
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--corpus", type=Path, default=CONTRACT / "assets.json")
    args = parser.parse_args()
    binary = args.binary.resolve(strict=True)
    if args.mode == "capture":
        authenticate_capture(binary)
        metadata = json.loads((binary.parent / "build.json").read_text())
        if metadata["ui"] != "built":
            raise ValueError("asset capture requires a full UI reference build")
        corpus = {"schema": "beans-embedded-assets-v1", "source_sha": metadata["source_sha"],
                  "node_version": metadata["node_version"], "npm_version": metadata["npm_version"],
                  "ui_lock_sha256": metadata["ui_lock_sha256"], "manifest": metadata["ui_assets"],
                  "cases": requests(metadata["ui_assets"])}
    else:
        corpus = json.loads(args.corpus.read_text())
    results, state = execute(binary, corpus)
    summaries = [summarize(case, result, corpus["manifest"])
                 for case, result in zip(corpus["cases"], results, strict=True)]
    if args.mode == "capture":
        for case, result in zip(corpus["cases"], summaries, strict=True):
            case["expected"] = result
        corpus["post_state"] = state
        args.corpus.write_text(json.dumps(corpus, indent=2) + "\n")
        print(f"Captured {len(summaries)} asset requests for {len(corpus['manifest'])} embedded files")
    else:
        failures = [case["id"] for case, result in zip(corpus["cases"], summaries, strict=True)
                    if case["expected"] != result]
        if state != corpus["post_state"]:
            failures.append("post_state")
        if failures:
            report = ROOT / ".compat/failures/assets.json"
            report.parent.mkdir(parents=True, exist_ok=True)
            report.write_text(json.dumps({"results": summaries, "post_state": state}, indent=2) + "\n")
            parser.exit(1, "Asset mismatches:\n" + "\n".join(failures) + "\n")
        print(f"Passed {len(summaries)} asset requests for {len(corpus['manifest'])} embedded files")


if __name__ == "__main__":
    main()
