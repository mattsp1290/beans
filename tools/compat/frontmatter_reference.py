#!/usr/bin/env python3
"""Capture parser primitives from the immutable Go tree, never Rust."""
import argparse
import base64
import hashlib
import io
import json
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile

ROOT = Path(__file__).resolve().parents[2]
BASELINE = ROOT / "tests/contract/baseline.json"


def capture():
    baseline = json.loads(BASELINE.read_text())
    archive = subprocess.check_output(["git", "archive", baseline["source_sha"]], cwd=ROOT)
    with tempfile.TemporaryDirectory(prefix="beans-domain-oracle-") as work:
        source = Path(work)
        with tarfile.open(fileobj=io.BytesIO(archive)) as tar:
            tar.extractall(source, filter="data")
        cases = []
        for directory in ("roundtrip", "request-roundtrip"):
            for fixture in sorted((source / "issue/testdata" / directory).glob("*.md")):
                name = directory + "/" + fixture.name
                path = ("projects/proj/requests/" if directory == "request-roundtrip"
                        else "projects/proj/issues/") + fixture.name
                cases.append({"name": name, "path": path,
                              "input_b64": base64.b64encode(fixture.read_bytes()).decode()})
        for fixture in sorted((ROOT / "tests/fixtures/domain-syntax").glob("*.md")):
            cases.append({"name": "syntax/" + fixture.name, "path": "syntax/" + fixture.name,
                          "input_b64": base64.b64encode(fixture.read_bytes()).decode()})
        required = {"id": "issue-abc", "title": "Title", "type": "task", "status": "open",
                    "priority": "2", "created": "2026-09-10T08:01:00Z", "updated": "2026-09-10T08:01:00Z"}
        variants = [("missing-" + key, {k: v for k, v in required.items() if k != key}, "")
                    for key in required]
        for value in ["null", "true", "0x10", "1.5", "9223372036854775807", "9223372036854775808",
                      "-9223372036854775808", "-9223372036854775809", "'+004'", "'-8'", "[]", "' 3 '"]:
            variants.append(("priority-" + str(len(variants)), dict(required, priority=value), ""))
        for value in ["null", "[]", "2026-09-10T08:01:00.123456789Z", "2026-09-10T08:01:00+05:30",
                      "2026-09-10T8:01:00Z", "2026-09-10T08:01:00,5Z", "2026-09-10T08:01:60Z",
                      "2026-09-10t08:01:00z", "2026-09-10T08:01:00+24:00"]:
            variants.append(("timestamp-" + str(len(variants)), dict(required, created=value), ""))
        variants.extend([
            ("duplicate-owned", required, "title: second\n"),
            ("duplicate-unknown", required, "extra: one\nextra: two\n"),
            ("permissive-required", dict(required, id="null", title="''", type="false", status="null", priority="99"), ""),
            ("links-and-lists", required, "aliases: [null, '~', 42]\nlabels: true\nparent: '[[ p#h|a ]]'\nblocked_by: ['', null, '[[q|a]]']\n"),
            ("owned-alias", required, "extra: &label foo\nlabels: *label\n"),
        ])
        for name, fields, suffix in variants:
            text = "---\n" + "".join(f"{key}: {value}\n" for key, value in fields.items()) + suffix + "---\nDescription\n## Log\n- opaque\n"
            cases.append({"name": "metadata/" + name, "path": "projects/proj/archive/issues/" + name + ".md",
                          "input_b64": base64.b64encode(text.encode()).decode()})
        harness = ROOT / "tools/compat/frontmatter_census.go.txt"
        (source / "issue/migration_frontmatter_test.go").write_bytes(harness.read_bytes())
        inputs, output = source / "input.json", source / "output.json"
        inputs.write_text(json.dumps(cases))
        env = dict(os.environ, BN_DOMAIN_INPUT=str(inputs), BN_DOMAIN_OUTPUT=str(output))
        subprocess.run(["go", "test", "./issue", "-run", "^TestMigrationFrontmatterCensus$",
                        "-count=1"], cwd=source, env=env, check=True, capture_output=True)
        captured_output = json.loads(output.read_text())
        captured = captured_output["cases"]
        for case in captured:
            case["input"] = base64.b64decode(case.pop("input_b64")).decode("utf-8")
        return {"schema": "beans-frontmatter-primitives-v1",
                "source_sha": baseline["source_sha"],
                "scope": "YAML nodes, byte spans, literal body sections, links, issue metadata/logs and existing-issue edit outputs; new files and other note schemas remain WP3",
                "harness_sha256": hashlib.sha256(harness.read_bytes()).hexdigest(),
                "cases": captured, "links": captured_output["links"],
                "log_cases": captured_output["log_cases"], "log_formats": captured_output["log_formats"],
                "log_sections": captured_output["log_sections"], "scalar_pairs": captured_output["scalar_pairs"]}


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, default=ROOT / ".compat/frontmatter-reference.json")
    parser.add_argument("--check", type=Path, help="compare a committed corpus without changing it")
    args = parser.parse_args()
    result = capture()
    if args.check:
        if result != json.loads(args.check.read_text()):
            raise SystemExit("frontmatter corpus differs from the fixed Go source")
        print("fixed Go frontmatter corpus matches")
    else:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(json.dumps(result, indent=2, ensure_ascii=False) + "\n")
        print(args.output)
