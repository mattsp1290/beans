#!/usr/bin/env python3
"""AST-derived yaml.v3 row accounting and immutable production-reader observations."""

import argparse, hashlib, io, json, os, subprocess, tarfile, tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
ORACLE = "718726a580c19becd5fb57513be9e76fda40ea26"
GOENV = dict(os.environ, GOTOOLCHAIN="go1.25.7")
STAMP = b"created: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\n"
HEADS = {
    "issue": b"id: p-one\ntitle: Title\ntype: task\nstatus: open # retained\npriority: 2\n",
    "request": b"id: p-r-one\naliases: [p-r-one]\ntitle: Title\nstatus: open\npriority: 2\n",
    "memory": b"key: key\ntype: reference\n",
    "handoff": b"id: p-one\ntitle: Title\n",
    "manifest": b"id: p-plan-one\naliases: [p-plan-one]\ntitle: Title\nslug: title\nstatus: draft\nsections: []\n",
}
BODY = b"Body\n"
PLANBODY = b"## Summary\n\n### Outcome\nresult\n\n### Affected areas\nfiles\n\n### Execution order\nfirst\n\n### Risks\nnone\n\n### Change graph\n```bn-change-graph\nversion: 1\nnodes: []\nedges: []\n```\n"
FIELDS = {
    "issue": ["title", "labels", "created", "priority", "parent"],
    "request": ["title", "labels", "created", "priority", "issues"],
    "memory": ["key", "tags", "created"],
    "handoff": ["title", "aliases", "created", "issue"],
    "manifest": ["title", "aliases", "created", "sections"],
}


def path(kind):
    return {
        "issue": "projects/p/issues/p-one.md",
        "request": "projects/p/requests/p-r-one.md",
        "memory": "projects/p/memories/key.md",
        "handoff": "projects/p/handoffs/p-one.md",
        "manifest": "plan.md",
        "graph": "graph.md",
        "workflow": "wf.yaml",
    }[kind]


def envelope(kind, fm):
    return b"---\n" + fm + b"---\n" + (PLANBODY if kind == "manifest" else BODY)


def cases(census):
    result = []

    def add(identity, kind, recipe, raw, edit=False):
        result.append(
            dict(
                ID=identity + "/" + kind + "/" + recipe,
                Kind=kind,
                Path=path(kind),
                Input=list(raw),
                Edit=edit,
                recipe=recipe,
            )
        )

    add(
        "demo/anchored",
        "issue",
        "status-edit",
        envelope(
            "issue",
            HEADS["issue"]
            + STAMP
            + b"custom: &settings {nested: [a, b]}\ncustom_alias: *settings\n# untouched\n",
        ),
        True,
    )
    for row in census["rows"]:
        if row["mode"] == "encode-only":
            continue
        original = bytes(row["input"])
        # Exact lexical removal of a one-letter mapping prefix; no YAML reserialization.
        payload = (
            original[3:]
            if len(original) >= 3
            and original[1:3] == b": "
            and original[:1] in [b"v", b"a"]
            else original
        )
        nested = lambda b: b"\n" + b"".join(
            b"  " + line + b"\n" for line in b.split(b"\n")
        )
        for kind in HEADS:
            head = HEADS[kind] + STAMP
            add(row["id"], kind, "raw-frontmatter", envelope(kind, original + b"\n"))
            add(
                row["id"],
                kind,
                "unknown-block",
                envelope(kind, head + b"upstream:" + nested(original)),
            )
            for field in FIELDS[kind]:
                base = b"".join(
                    line + b"\n"
                    for line in head.splitlines()
                    if not line.startswith(field.encode() + b":")
                )
                add(
                    row["id"],
                    kind,
                    "owned-" + field,
                    envelope(kind, base + field.encode() + b": " + payload + b"\n"),
                )
        add(row["id"], "workflow", "raw-document", original)
        for field in ["default", "statuses", "transitions"]:
            add(
                row["id"],
                "workflow",
                "owned-" + field,
                b"workflow:\n  "
                + field.encode()
                + b": "
                + payload.replace(b"\n", b"\n  ")
                + b"\n",
            )
        add(
            row["id"],
            "graph",
            "raw-fence",
            b"```bn-change-graph\n" + original + b"\n```\n",
        )
        for field in ["version", "nodes", "edges"]:
            base = b"".join(
                f + b": " + v + b"\n"
                for f, v in [(b"version", b"1"), (b"nodes", b"[]"), (b"edges", b"[]")]
                if f.decode() != field
            )
            add(
                row["id"],
                "graph",
                "owned-" + field,
                b"```bn-change-graph\n"
                + base
                + field.encode()
                + b": "
                + payload
                + b"\n```\n",
            )
    for name, payload in [
        ("mapping-value-diagnostic", b"a: b"),
        ("missing-colon-key-mark", b"# head\ntrue # inline\n# trailing\n"),
        ("block-sequence-diagnostic", b"- item"),
        ("forbidden-token-diagnostic", b"%bad"),
    ]:
        add("regression/" + name, "issue", "owned-title", envelope("issue",
            HEADS["issue"].replace(b"title: Title\n", b"") + STAMP + b"title: " + payload + b"\n"))
    add("regression/document-start-diagnostic", "issue", "raw-frontmatter",
        envelope("issue", b"%YAML 1.1\nplain\n"))
    for name, payload in [
        ("required-flow-sequence-key-mark", b"a:\n  b: c\n[broken, x]\nnext: value\n"),
        ("required-flow-mapping-key-mark", b"a:\n  b: c\n{broken, x}\nnext: value\n"),
        ("first-line-mapping-diagnostic", b"a: b: c\n"),
    ]:
        for kind in ["issue", "workflow"]:
            raw = envelope(kind, payload) if kind == "issue" else payload
            add("regression/" + name, kind, "raw-document", raw)
    add("regression/empty-graph-fence", "graph", "raw-fence", b"```bn-change-graph\n```\n")
    add("regression/raw-graph-invalid-utf8", "graph", "raw-fence",
        b"```bn-change-graph\nversion: \xff\n```\n")
    for kind in ["workflow", "graph"]:
        payload = b"\xff\xfe" + "ñoño: true".encode("utf-16le")
        raw = payload if kind == "workflow" else b"```bn-change-graph\n" + payload + b"\n```\n"
        add("regression/utf16-reader", kind, "raw-document", raw)
    add("regression/owned-status-anchor", "issue", "retained-alias", envelope(
        "issue", HEADS["issue"].replace(b"status: open # retained", b"status: &state open # retained")
        + STAMP + b"unknown_status: *state\n"))
    add("regression/description-log-tail", "issue", "retained-sections",
        envelope("issue", HEADS["issue"] + STAMP + b"custom: &settings {nested: [a, b]}\ncustom_alias: *settings\n")
        + b"\n## Details\nBody stays.\n\n## Log\n- 2026-01-01T00:00:00Z user: preserved event\n  retained continuation\n\n## Tail\nTail stays.\n")
    originals = list(result)
    for c in originals:
        if c["Kind"] == "issue" and not c["Edit"]:
            for field in ["status", "description"]:
                result.append(dict(c, ID=c["ID"] + "/edit-" + field, EditField=field))
    return result


def capture():
    with tempfile.TemporaryDirectory(prefix="beans-yaml-upstream-") as work:
        work = Path(work)
        extractor = work / "extract.go"
        extractor.write_bytes(
            (ROOT / "tools/compat/yaml-upstream/extract.go.txt").read_bytes()
        )
        dependency = (
            Path(
                subprocess.check_output(
                    ["go", "env", "GOMODCACHE"], env=GOENV, text=True
                ).strip()
            )
            / "gopkg.in/yaml.v3@v3.0.1"
        )
        census = json.loads(
            subprocess.check_output(
                ["go", "run", str(extractor), str(dependency)], env=GOENV
            )
        )
        census.update(
            schema="beans-yaml-upstream-v1",
            oracle_sha=ORACLE,
            dependency="gopkg.in/yaml.v3@v3.0.1",
            license=(dependency / "LICENSE").read_text(),
            license_sha256=hashlib.sha256(
                (dependency / "LICENSE").read_bytes()
            ).hexdigest(),
            upstream_notice=(dependency / "decode_test.go")
            .read_text()
            .split("package yaml_test")[0],
            dependency_go_mod_sha256=hashlib.sha256(
                (dependency / "go.mod").read_bytes()
            ).hexdigest(),
            annotation_evidence="node_test.go TestNodeRoundtrip: [encode] sets decode=false; [decode] sets encode=false; prefixes removed before decode",
        )
        inputs = cases(census)
        inputfile = work / "inputs.json"
        inputfile.write_text(json.dumps(inputs))
        source = work / "oracle"
        source.mkdir()
        archive = subprocess.check_output(["git", "archive", ORACLE], cwd=ROOT)
        with tarfile.open(fileobj=io.BytesIO(archive)) as tar:
            tar.extractall(source, filter="data")
        observations = {}
        for package in ["issue", "plan"]:
            harness = (
                ROOT / f"tools/compat/yaml-upstream/{package}.go.txt"
            ).read_bytes() + (
                ROOT / "tools/compat/yaml-upstream/common.go.txt"
            ).read_bytes()
            (source / package / "migration_yaml_test.go").write_bytes(harness)
            output = work / f"{package}.json"
            subprocess.run(
                [
                    "go",
                    "test",
                    "./" + package,
                    "-run",
                    "^TestMigrationYAMLCensus$",
                    "-count=1",
                ],
                cwd=source,
                env=dict(
                    GOENV, BN_YAML_INPUT=str(inputfile), BN_YAML_OUTPUT=str(output)
                ),
                check=True,
            )
            observations.update(json.loads(output.read_text()))
        census["cases"] = [dict(c, expected=observations[c["ID"]]) for c in inputs]
        census["harness_sha256"] = {
            p.name: hashlib.sha256(p.read_bytes()).hexdigest()
            for p in sorted((ROOT / "tools/compat/yaml-upstream").glob("*.txt"))
        }
        return census


if __name__ == "__main__":
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument(
        "--output", type=Path, default=ROOT / "tests/contract/yaml-upstream.json"
    )
    p.add_argument("--check", type=Path)
    a = p.parse_args()
    result = capture()
    if a.check:
        if result != json.loads(a.check.read_text()):
            raise SystemExit("upstream census/immutable Go observations differ")
        print("Complete upstream census and expectations match immutable Go")
    else:
        a.output.parent.mkdir(parents=True, exist_ok=True)
        a.output.write_text(json.dumps(result, separators=(",", ":")) + "\n")
        print(a.output)
