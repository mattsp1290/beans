#!/usr/bin/env python3
"""Capture memory document codecs from the fixed Go tree."""
import argparse
import hashlib
import io
import json
import os
import re
from pathlib import Path
import subprocess
import tarfile
import tempfile

ROOT = Path(__file__).resolve().parents[2]


def capture(rust_input=None):
    baseline = json.loads((ROOT / "tests/contract/baseline.json").read_text())
    archive = subprocess.check_output(["git", "archive", baseline["source_sha"]], cwd=ROOT)
    harness = ROOT / "tools/compat/memory_census.go.txt"
    with tempfile.TemporaryDirectory(prefix="beans-request-oracle-") as work:
        source = Path(work)
        with tarfile.open(fileobj=io.BytesIO(archive)) as tar:
            tar.extractall(source, filter="data")
        (source / "issue/migration_memory_test.go").write_bytes(harness.read_bytes())
        fixture = (source / "issue/memory_test.go").read_text().split("const memSample = `",1)[1].split("`",1)[0]
        path = "projects/beans/memories/sample.md"
        cases = [{"name":"original","path":path,"input":fixture}]
        def add(name,text=fixture,file=path):
            cases.append({"name":name,"path":file,"input":text})
        for key in ["key","type","tags","created","updated"]:
            add("missing-"+key,"".join(line for line in fixture.splitlines(True) if not line.startswith(key+":")))
            add("duplicate-"+key,fixture.replace("---\nBody",key+": null\n---\nBody",1))
        variants={"key":["null","''","true","'New Key 雪'","[]"],"type":["null","bogus","[]"],"tags":["null","true","[null, 42, '']","[{}]"],"created":["null","''","' '","0001-01-01T00:00:00Z","bad","[]","2026-09-10T3:00:00,5Z"],"updated":["null","bad","2026-09-10T00:00:00+24:00"]}
        for key,values in variants.items():
            for index,value in enumerate(values):
                add(key+"-"+str(index),re.sub(r"^"+key+r":.*",key+": "+value,fixture,count=1,flags=re.M))
        add("minimal","---\nkey: x\n---\n")
        add("unknown-duplicates",fixture.replace("custom: keep me","custom: keep me\ncustom: second\nanchor: &a {雪: [one, two]}\nreference: *a"))
        add("crlf",fixture.replace("\n","\r\n"))
        add("empty-key",fixture.replace("key: bean-counter-prod-schema","key:"))
        add("body-no-newline",fixture.rstrip("\n"))
        add("global",file="memories/sample.md")
        add("repeated-project",file="projects/first/projects/last/memories/sample.md")
        if rust_input is not None:
            cases = json.loads(Path(rust_input).read_text())
        inputs = source / "input.json"
        inputs.write_text(json.dumps(cases))
        output = source / "memories.json"
        subprocess.run(["go", "test", "./issue", "-run", "^TestMigrationMemoryCensus$", "-count=1"],
                       cwd=source, env=dict(os.environ, BN_MEMORY_INPUT=str(inputs), BN_MEMORY_OUTPUT=str(output),
                                            BN_MEMORY_READ_ONLY="1" if rust_input else "0"),
                       check=True, capture_output=True)
        return {"schema": "beans-memories-v1", "source_sha": baseline["source_sha"],
                "harness_sha256": hashlib.sha256(harness.read_bytes()).hexdigest(),
                "cases": json.loads(output.read_text())}


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, default=ROOT / ".compat/memory-reference.json")
    parser.add_argument("--check", type=Path)
    args = parser.parse_args()
    result = capture()
    if args.check:
        if result != json.loads(args.check.read_text()):
            raise SystemExit("Memory document corpus differs from the fixed Go source")
        print("fixed Go memory document corpus matches")
    else:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(json.dumps(result, indent=2, ensure_ascii=False) + "\n")
        print(args.output)
