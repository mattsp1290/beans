#!/usr/bin/env python3
"""Inventory tests, routes and storage fixtures at the immutable baseline."""
import io
import hashlib
import json
from pathlib import Path
import re
import subprocess
import tarfile

ROOT = Path(__file__).resolve().parents[2]


def main():
    baseline = json.loads((ROOT / "tests/contract/baseline.json").read_text())
    data = subprocess.run(["git", "archive", baseline["source_sha"]], cwd=ROOT,
                          capture_output=True, check=True).stdout
    tests, routes, fixtures = [], [], []
    destinations = {"cmd/bn": "tests/cli.rs", "gitops": "tests/git_pipeline.rs",
                    "internal/ops": "tests/operations.rs", "internal/server": "tests/server.rs",
                    "markdown": "tests/markdown.rs", "vault": "tests/vault.rs",
                    "issue": "tests/domain.rs", "plan": "tests/plans.rs",
                    "version": "tests/cli.rs"}
    with tarfile.open(fileobj=io.BytesIO(data)) as archive:
        for member in sorted(archive.getmembers(), key=lambda m: m.name):
            if not member.isfile():
                continue
            content = archive.extractfile(member).read()
            path = Path(member.name)
            if path.name.endswith("_test.go"):
                text = content.decode()
                for match in re.finditer(r"^func (Test\w+)\(t \*testing.T\)", text, re.M):
                    tests.append({"source": member.name, "test": match[1],
                                  "line": text[:match.start()].count("\n") + 1,
                                  "destination": destinations.get(str(path.parent), "unassigned"),
                                  "status": "pending", "retirement_rationale": None})
            if "testdata" in path.parts:
                target = ROOT / "tests/fixtures/go-baseline" / member.name
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_bytes(content)
                fixtures.append({"source": member.name, "destination": target.relative_to(ROOT).as_posix(),
                                 "sha256": hashlib.sha256(content).hexdigest()})
            if member.name == "internal/server/routes.go":
                for method, route, handler in re.findall(r'api\.(Get|Post|Patch|Delete)\("([^"]+)", s\.(\w+)\)', content.decode()):
                    routes.append({"method": method.upper(), "path": "/api" + route,
                                   "handler": handler, "implicit_head": method == "Get",
                                   "status": "pending"})
    out = ROOT / "tests/contract"
    for name, value in (("regressions.json", tests), ("routes.json", routes), ("fixtures.json", fixtures)):
        (out / name).write_text(json.dumps({"source_sha": baseline["source_sha"],
                                          "entries": value}, indent=2) + "\n")
    print(f"Inventoried {len(tests)} regression tests, {len(routes)} routes, {len(fixtures)} fixtures")


if __name__ == "__main__":
    main()
