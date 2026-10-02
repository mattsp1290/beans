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
        wanted = bytes(f["expected"].get("parse_error", [])).decode("utf8", "backslashreplace")
        got = bytes(f["actual"].get("parse_error", [])).decode("utf8", "backslashreplace")
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
    edits = [c for c in corpus["cases"] if c.get("EditField")]
    edit_counts = collections.Counter()
    for c in edits:
        r = actual[c["ID"]]
        if r["initial_read"]["parse_error"]:
            edit_counts["initial_parse_failure"] += 1
        elif r["edit_error"]:
            edit_counts["encode_failure"] += 1
        elif r["reread"]["parse_error"]:
            edit_counts["historical_invalid_edit"] += 1
        else:
            edit_counts["valid_" + c["EditField"] + "_edit"] += 1
            raw, edited = bytes(c["Input"]), bytes(r["edit_bytes"])
            if bytes(r["initial_read"]["noop"]) != raw:
                raise ValueError("initial issue no-op changed bytes: " + c["ID"])
            if r["initial_read"]["model"]["unknown"] != r["reread"]["model"]["unknown"]:
                raise ValueError("valid edit changed unknown YAML model: " + c["ID"])
            for begin, end, dest, dest_end in r["unchanged_ranges"]:
                if raw[begin:end] != edited[dest:dest_end]:
                    raise ValueError("changed outside owned range: " + c["ID"])
    original = json.loads(subprocess.check_output(
        ["git", "show", "7d43671f5327aae08ec509e852bab092fe9b512c:tests/contract/yaml-upstream.json"], cwd=ROOT))
    by_id = {c["ID"]: c for c in corpus["cases"]}
    if any(by_id.get(c["ID"]) != c for c in original["cases"]):
        raise ValueError("original census case/input/expectation changed")
    stage_counts = collections.Counter()
    original_stages = collections.Counter()
    supplemental_stages = collections.Counter()
    original_ids = {c["ID"] for c in original["cases"]}
    normalized_noops = []
    for c in corpus["cases"]:
        if c.get("EditField"):
            continue
        r = actual[c["ID"]]
        stage = "parse_error" if r["parse_error"] else "encode_error" if r["encode_error"] else "success"
        stage_counts[stage] += 1
        (original_stages if c["ID"] in original_ids else supplemental_stages)[stage] += 1
        if not c["Edit"] and r["noop"] is not None and r["noop"] != c["Input"]:
            normalized_noops.append(c["ID"])
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
        retained_original_observations=len(original["cases"]),
        edit_qualification=dict(edit_counts),
        projection_stages=dict(stage_counts),
        accounting=dict(original=len(original_ids), supplemental=len(actual)-len(original_ids),
                        original_stages=dict(original_stages), supplemental_read_stages=dict(supplemental_stages),
                        supplemental_edits=len(edits)),
        historical_normalized_noops=dict(count=len(normalized_noops), ids=normalized_noops),
        scope_revision=2,
        noop_policy="User-approved scope revision 2: byte-identical notes; immutable-Go canonical plan manifests with all 88 original normalizations reported",
        mismatches=len(failures),
        by_domain=dict(collections.Counter(by_id[f["id"]]["Kind"] for f in failures)),
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
