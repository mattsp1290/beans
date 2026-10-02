#!/usr/bin/env python3
"""Capture handoff document codecs from the fixed Go tree."""
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
    harness = ROOT / "tools/compat/handoff_census.go.txt"
    with tempfile.TemporaryDirectory(prefix="beans-handoff-oracle-") as work:
        source = Path(work)
        with tarfile.open(fileobj=io.BytesIO(archive)) as tar:
            tar.extractall(source, filter="data")
        (source / "issue/migration_handoff_test.go").write_bytes(harness.read_bytes())
        literal=(source / "issue/handoff_test.go").read_text().split("const handoffFixture = ",1)[1].splitlines()[0]
        fixture=json.loads(literal)
        path="projects/alpha/handoffs/alpha-a1b2-continue-work.md"
        cases=[{"name":"original","path":path,"input":fixture}]
        def add(name,text=fixture,file=path):
            cases.append({"name":name,"path":file,"input":text})
        for key in ["id","aliases","title","issue","created","updated","custom"]:
            add("missing-"+key,"".join(line for line in fixture.splitlines(True) if not line.startswith(key+":")))
            add("duplicate-"+key,fixture.replace("---\n# Context",key+": null\n---\n# Context",1))
        variants={"id":["null","bad","alpha-a1b2.1","[]"],"title":["null","''","' '","true","[]"],"aliases":["null","[]","[external]","[null, 42]","[{}]"],"issue":["null","true","'bare'","[]","'[[x#h|a]]'"],"created":["null","''","0001-01-01T00:00:00Z","bad","[]","2026-09-10T3:00:00,5Z"],"updated":["null","bad","2026-09-10T00:00:00+24:00"]}
        for key,values in variants.items():
            for index,value in enumerate(values):
                add(key+"-"+str(index),re.sub(r"^"+key+r":.*",key+": "+value,fixture,count=1,flags=re.M))
        files=["projects/alpha/handoffs/alpha-a1b2.md","projects/alpha/handoffs/alpha-a1b2-.md","projects/alpha/handoffs/alpha-a1b2x.md","projects/alpha/handoffs/.md","projects/alpha/handoffs/alpha-a1b2.MD","projects/alpha/handoffs/nested/alpha-a1b2.md","docs/alpha-a1b2.md","/projects/alpha/handoffs/alpha-a1b2.md","./projects/alpha/handoffs/alpha-a1b2.md","projects//handoffs/alpha-a1b2.md"]
        files += ["projects/alpha/handoffs/archive/"+year+"/alpha-a1b2.md" for year in ["2026","0000","9999","abcd","123","12345","２０２６"]]
        for index,file in enumerate(files): add("path-"+str(index),file=file)
        add("crlf",fixture.replace("\n","\r\n",1),"bad/path")
        add("unknown-anchor",fixture.replace("custom: keep me","custom: *missing"))
        add("nested-unknown",fixture.replace("custom: keep me","custom: &a {雪: [one, two]}\nreference: *a"))
        add("body-no-newline",fixture.rstrip("\n"))
        add("path-before-field-validation",fixture.replace("created: 2026-09-10T21:44:02Z","created: bad"),"bad/path")
        if rust_input is not None:
            cases = json.loads(Path(rust_input).read_text())
        inputs = source / "input.json"
        inputs.write_text(json.dumps(cases))
        output = source / "handoffs.json"
        subprocess.run(["go", "test", "./issue", "-run", "^TestMigrationHandoffCensus$", "-count=1"],
                       cwd=source, env=dict(os.environ, BN_HANDOFF_INPUT=str(inputs), BN_HANDOFF_OUTPUT=str(output),
                                            BN_HANDOFF_READ_ONLY="1" if rust_input else "0"),
                       check=True, capture_output=True)
        return {"schema": "beans-handoffs-v1", "source_sha": baseline["source_sha"],
                "harness_sha256": hashlib.sha256(harness.read_bytes()).hexdigest(),
                "cases": json.loads(output.read_text())}


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, default=ROOT / ".compat/handoff-reference.json")
    parser.add_argument("--check", type=Path)
    args = parser.parse_args()
    result = capture()
    if args.check:
        if result != json.loads(args.check.read_text()):
            raise SystemExit("Handoff document corpus differs from the fixed Go source")
        print("fixed Go handoff document corpus matches")
    else:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(json.dumps(result, indent=2, ensure_ascii=False) + "\n")
        print(args.output)
