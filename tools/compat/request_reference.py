#!/usr/bin/env python3
"""Capture request document codecs from the fixed Go tree."""
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
    harness = ROOT / "tools/compat/request_census.go.txt"
    with tempfile.TemporaryDirectory(prefix="beans-request-oracle-") as work:
        source = Path(work)
        with tarfile.open(fileobj=io.BytesIO(archive)) as tar:
            tar.extractall(source, filter="data")
        (source / "issue/migration_request_test.go").write_bytes(harness.read_bytes())
        fixture = (source / "issue/request_codec_test.go").read_text().split("const requestFixture = `",1)[1].split("`",1)[0]
        path = "projects/beans/requests/beans-r-a3f2.md"
        cases = [{"name":"original", "path":path, "input":fixture}]
        for file in sorted((source / "issue/testdata/request-roundtrip").glob("*.md")):
            cases.append({"name":file.name,"path":path,"input":file.read_text()})
        def add(name, text=fixture, file=path):
            cases.append({"name":name,"path":file,"input":text})
        for key in ["id","aliases","title","status","priority","created","updated"]:
            lines = fixture.splitlines(True)
            add("missing-"+key,"".join(line for line in lines if not line.startswith(key+":")))
            add("duplicate-"+key,fixture.replace("---\nContext.",key+": null\n---\nContext.",1))
        values = {"id":["beans-a3f2","null"],"aliases":["[]","null","[external]","[beans-r-a3f2, null]"],"title":["''","null","true","[]"],"status":["later","null"],"priority":["-1","5","0x10","' 3 '","9223372036854775808","[]"],"created":["0001-01-01T00:00:00Z","null","2026-09-11T3:00:00,5Z","2026-09-11T03:00:00+24:00"],"requested_by":["null","[]","true"],"issues":["null","[]","['', null, '[[x|a]]']","*unknown"]}
        for key, variants in values.items():
            for index, value in enumerate(variants):
                text = re.sub(r"^"+key+r":.*(?:\n  -.*)*",key+": "+value,fixture,count=1,flags=re.M)
                add(key+"-"+str(index),text)
        for index, file in enumerate(["projects/beans/issues/x.md", "projects/beans/requests/.md", "projects/beans/requests/x.MD", "projects/beans/requests/x.md/extra", "/tmp/projects/beans/requests/x.md", "./projects//beans/requests/../requests/x.md", "projects/beans/requests/../../beans/requests/x.md", "projects/beans/requests/x\\name.md", "projects/beans/requests/../x.md"]):
            add("path-"+str(index),file=file)
        add("crlf",fixture.replace("\n","\r\n"),"bad/path")
        add("unknown-duplicate",fixture.replace("custom: preserve", "custom: preserve\ncustom: second\nanchor: &a {雪: [one, two]}\nreference: *a"))
        add("no-log",fixture.split("## Log")[0])
        add("no-body",fixture.split("---\nContext.")[0]+"---\n")
        add("body-no-newline",fixture.split("## Log")[0].rstrip("\n"))
        if rust_input is not None:
            cases = json.loads(Path(rust_input).read_text())
        inputs = source / "input.json"
        inputs.write_text(json.dumps(cases))
        output = source / "requests.json"
        subprocess.run(["go", "test", "./issue", "-run", "^TestMigrationRequestCensus$", "-count=1"],
                       cwd=source, env=dict(os.environ, BN_REQUEST_INPUT=str(inputs), BN_REQUEST_OUTPUT=str(output),
                                            BN_REQUEST_READ_ONLY="1" if rust_input else "0"),
                       check=True, capture_output=True)
        return {"schema": "beans-requests-v1", "source_sha": baseline["source_sha"],
                "harness_sha256": hashlib.sha256(harness.read_bytes()).hexdigest(),
                "cases": json.loads(output.read_text())}


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, default=ROOT / ".compat/request-reference.json")
    parser.add_argument("--check", type=Path)
    args = parser.parse_args()
    result = capture()
    if args.check:
        if result != json.loads(args.check.read_text()):
            raise SystemExit("Request document corpus differs from the fixed Go source")
        print("fixed Go request document corpus matches")
    else:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(json.dumps(result, indent=2, ensure_ascii=False) + "\n")
        print(args.output)
