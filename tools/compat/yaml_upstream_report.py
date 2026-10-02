#!/usr/bin/env python3
"""Visible production comparison; mismatches are failures, never excluded rows."""

import argparse, collections, hashlib, json, os, subprocess, platform, re
from pathlib import Path
from yaml_upstream_reference import ROOT, cases


def compare(corpus, actual):
    expected_ids = [c["ID"] for c in corpus["cases"]]
    if len(set(expected_ids)) != len(expected_ids):
        raise ValueError("duplicate observation identities")
    if set(actual) != set(expected_ids):
        raise ValueError("missing or unexpected Rust observation")
    return [
        dict(id=c["ID"], expected=c["expected"], actual=actual[c["ID"]])
        for c in corpus["cases"]
        if c["expected"] != actual[c["ID"]]
    ]


def validate(corpus):
    # Independent cardinality and contiguous indices prevent silent missing-row edits.
    counts = {"unmarshalTests": 172, "nodeTests": 74}
    for table, count in counts.items():
        indices = [r["index"] for r in corpus["rows"] if r["table"] == table]
        if indices != list(range(count)):
            raise ValueError("missing or reordered upstream row: " + table)
    for r in corpus["rows"]:
        if r["id"] != f"{r['table']}/{r['index']:04d}":
            raise ValueError("changed stable row identity")
        if r["mode"] == "encode-only" and not (
            r["table"] == "nodeTests" and r["annotation"] == "[encode]"
        ):
            raise ValueError("unsupported decode exclusion")
    generated = cases(corpus)
    if generated != [
        {k: v for k, v in c.items() if k != "expected"} for c in corpus["cases"]
    ]:
        raise ValueError("missing or changed projection/input")


def run(corpus_path, report_path):
    corpus = json.loads(corpus_path.read_text())
    validate(corpus)
    output = ROOT / ".compat/yaml-upstream-rust.json"
    output.parent.mkdir(parents=True, exist_ok=True)
    env = dict(
        os.environ,
        BN_YAML_CORPUS=str(corpus_path.resolve()),
        BN_YAML_RUST_OUTPUT=str(output),
    )
    subprocess.run(
        [
            "cargo",
            "test",
            "--locked",
            "--test",
            "domain",
            "yaml_upstream_observations",
            "--",
            "--nocapture",
        ],
        cwd=ROOT,
        env=env,
        check=True,
    )
    actual = json.loads(output.read_text())
    failures = compare(corpus, actual)
    demo = corpus["cases"][0]
    if demo["expected"] != actual[demo["ID"]]:
        raise ValueError("anchored issue edit/readback parity failed")
    raw = bytes(demo["Input"])
    edited = bytes(actual[demo["ID"]]["noop"])
    old = b"status: open # retained\n"
    new = b"status: in_progress # retained\n"
    start = raw.index(old)
    if (
        edited[:start] != raw[:start]
        or edited[start + len(new) :] != raw[start + len(old) :]
        or edited[start : start + len(new)] != new
    ):
        raise ValueError("anchored issue edited unaffected bytes")
    # Meaningful changed-expectation control on a known successful anchored model.
    altered = json.loads(json.dumps(corpus))
    altered["cases"][0]["expected"]["model"]["status"] = [0]
    if not any(f["id"] == demo["ID"] for f in compare(altered, actual)):
        raise ValueError("changed expectation was accepted")
    removed = json.loads(json.dumps(corpus))
    removed["rows"].pop()
    try:
        validate(removed)
    except ValueError:
        pass
    else:
        raise ValueError("removed upstream row was accepted")
    counts = collections.Counter((r["table"], r["mode"]) for r in corpus["rows"])
    groups = collections.defaultdict(list)
    for f in failures:
        wanted = bytes(f["expected"]["parse_error"]).decode("utf8", "backslashreplace")
        got = bytes(f["actual"]["parse_error"]).decode("utf8", "backslashreplace")
        if wanted and got:
            category = re.sub(r"line \d+: ", "", wanted.split("yaml: ")[-1])
            category = category.split(" at line")[0]
            groups[category].append(dict(id=f["id"], go=wanted, rust=got))
        else:
            groups["model/acceptance/encode/reread difference"].append(
                dict(id=f["id"], expected=f["expected"], actual=f["actual"])
            )
    grouped = {
        k: dict(count=len(v), examples=v[:3])
        for k, v in sorted(groups.items(), key=lambda kv: -len(kv[1]))
    }
    report = dict(
        runtime=dict(
            platform=platform.platform(),
            go=subprocess.check_output(
                ["go", "version"],
                env=dict(os.environ, GOTOOLCHAIN="go1.25.7"),
                text=True,
            ).strip(),
            rust=subprocess.check_output(["rustc", "--version"], text=True).strip(),
        ),
        groups=grouped,
        schema="beans-yaml-upstream-report-v1",
        head=subprocess.check_output(
            ["git", "rev-parse", "HEAD"], cwd=ROOT, text=True
        ).strip(),
        oracle_sha=corpus["oracle_sha"],
        corpus_sha256=hashlib.sha256(corpus_path.read_bytes()).hexdigest(),
        counts={f"{a}/{b}": n for (a, b), n in counts.items()},
        observations=len(actual),
        mismatches=len(failures),
        by_domain=dict(collections.Counter(f["id"].split("/")[-2] for f in failures)),
        failures=failures,
        negative_controls="changed expectation and removed row rejected",
        anchored_demo="production edit and reread; exact outside-status bytes preserved",
    )
    report_path.parent.mkdir(parents=True, exist_ok=True)
    report_path.write_text(json.dumps(report, indent=2) + "\n")
    print(
        json.dumps(
            {k: v for k, v in report.items() if k not in ["failures", "groups"]},
            indent=2,
        )
    )
    for name, g in list(grouped.items())[:12]:
        print(
            "GROUP",
            g["count"],
            name,
            "examples:",
            ", ".join(x["id"] for x in g["examples"]),
        )
    for f in failures[:3]:
        delta = [k for k in f["expected"] if f["expected"][k] != f["actual"].get(k)]
        print("MISMATCH", f["id"], ",".join(delta))
        if "error" in delta:
            print(
                " expected error:",
                bytes(f["expected"]["error"]).decode("utf8", "backslashreplace"),
                "\n actual error:",
                bytes(f["actual"]["error"]).decode("utf8", "backslashreplace"),
            )
    print("Full per-case report:", report_path)
    return bool(failures)


if __name__ == "__main__":
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument(
        "--corpus", type=Path, default=ROOT / "tests/contract/yaml-upstream.json"
    )
    p.add_argument(
        "--report", type=Path, default=ROOT / ".compat/yaml-upstream-report.json"
    )
    a = p.parse_args()
    raise SystemExit(run(a.corpus, a.report))
