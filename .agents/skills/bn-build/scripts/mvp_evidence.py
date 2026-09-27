"""Typed, scope-bound evidence and conservative assignment recovery."""
from pathlib import Path
import re
from urllib.parse import urlsplit

from mvp_common import Failure, canonical, git, require, run
from mvp_contract import SHA, identifier, ref, relative_path, string, strings
from mvp_ownership import remote_refs

TYPES = {
    "scope": {"scope_revision"},
    "assignment": {"slice_id", "assignment_id", "branch", "worktree", "status"},
    "review": {"slice_id", "base_sha", "head_sha", "reviewer", "verdict", "evidence"},
    "integration": {"slice_id", "base_sha", "head_sha", "integrated_sha", "checks", "reviews"},
    "finding-disposition": {"fingerprint", "disposition", "evidence"},
    "demo": {"head_sha", "checks", "journey", "runtime_identity"},
    "manual-acceptance": {"head_sha", "evidence"},
    "final-pr": {"head_sha", "url"},
}


def checks(repo, value):
    require(isinstance(value, list) and value, "check results must be a nonempty list")
    for check in value:
        require(isinstance(check, dict) and {"argv", "cwd", "exit_code"} <= set(check), "check needs argv, cwd, exit_code")
        strings(check["argv"])
        relative_path(repo, check["cwd"])
        require(type(check["exit_code"]) is int and check["exit_code"] == 0, "failed checks cannot attest integration/demo")


def validate_record(record, c, repo):
    require(isinstance(record, dict) and set(record) == {"key", "type", "scope_digest", "data"}, "invalid evidence envelope")
    string(record["key"])
    require(re.fullmatch(r"[a-zA-Z0-9][a-zA-Z0-9:_.-]*", record["key"]), "invalid event key")
    require(record["type"] in TYPES, "unknown evidence type")
    require(isinstance(record["scope_digest"], str) and re.fullmatch(r"[0-9a-f]{64}", record["scope_digest"]), "invalid scope digest")
    data, kind = record["data"], record["type"]
    require(isinstance(data, dict) and TYPES[kind] <= set(data), "missing typed evidence fields")
    for key in ("base_sha", "head_sha", "integrated_sha"):
        if key in data:
            require(isinstance(data[key], str) and SHA.fullmatch(data[key]), "invalid " + key)
    if "slice_id" in data:
        identifier(data["slice_id"])
    if kind == "scope":
        require(type(data["scope_revision"]) is int and data["scope_revision"] > 0, "invalid scope revision")
    elif kind == "assignment":
        identifier(data["assignment_id"])
        ref(repo, data["branch"])
        require(data["branch"] not in (c["default_ref"], "refs/heads/main", c["target_ref"]), "assignment branch must be isolated")
        string(data["worktree"])
        path = Path(data["worktree"])
        require(".." not in path.parts, "worktree traversal", "unsafe-path")
        if path.is_absolute():
            require(not any(p.is_symlink() for p in (path, *path.parents)), "worktree path contains symlink", "unsafe-path")
        else:
            relative_path(repo, data["worktree"])
        require(data["status"] in ("active", "stopped", "integrated"), "invalid assignment status")
        if "replaces" in data:
            identifier(data["replaces"])
    elif kind == "review":
        string(data["reviewer"])
        strings(data["evidence"])
        require(data["verdict"] in ("pass", "changes"), "invalid review verdict")
    elif kind == "integration":
        checks(repo, data["checks"])
        strings(data["reviews"])
    elif kind == "finding-disposition":
        string(data["fingerprint"])
        string(data["evidence"])
        require(data["disposition"] in ("accepted", "rejected", "deferred", "superseded", "unverified"), "invalid finding disposition")
    elif kind == "demo":
        checks(repo, data["checks"])
        strings(data["journey"])
        string(data["runtime_identity"])
    elif kind == "manual-acceptance":
        string(data["evidence"])
    elif kind == "final-pr":
        string(data["url"])
        url = urlsplit(data["url"])
        require(url.scheme == "https" and url.hostname and not url.username and not url.password,
                "final PR needs its actual credential-free HTTPS URL")


def target_sha(repo, c):
    actual = remote_refs(repo, c["remote"], c["target_ref"])
    sha = actual.get(c["target_ref"])
    if sha:
        git(repo, "fetch", "--no-tags", c["remote"], c["target_ref"])
        fetched = git(repo, "rev-parse", "FETCH_HEAD")
        require(fetched == sha, "target moved during inspection; reconcile again", "stale")
    return sha


def ancestor(repo, earlier, later):
    result = run(["git", "merge-base", "--is-ancestor", earlier, later], repo, check=False)
    return result.returncode == 0


def integration_proof(record, records, repo, c, target):
    data = record["data"]
    require(target and all(ancestor(repo, data[key], target) for key in ("base_sha", "head_sha", "integrated_sha")),
            "integration commit or reviewed head is absent from the remote target", "stale")
    require(data["integrated_sha"] in git(repo, "rev-list", "--first-parent", target).splitlines(),
            "integration must be on the target's first-parent history, not only a merged side branch", "stale")
    require(ancestor(repo, data["head_sha"], data["integrated_sha"]) and
            ancestor(repo, data["base_sha"], data["integrated_sha"]), "integration does not preserve the reviewed pair", "stale")
    if data["integrated_sha"] == data["head_sha"]:
        require(ancestor(repo, data["base_sha"], data["head_sha"]),
                "fast-forward integration must descend from its reviewed base", "stale")
    else:
        parents = git(repo, "show", "-s", "--format=%P", data["integrated_sha"]).split()
        require(parents == [data["base_sha"], data["head_sha"]],
                "integration merge must directly join the reviewed target base and head", "stale")
    reviewers = set()
    for key in data["reviews"]:
        review = records.get(key, {})
        require(review.get("type") == "review" and review.get("scope_digest") == record["scope_digest"], "missing or stale review evidence", "stale")
        details = review["data"]
        require(all(details.get(field) == data[field] for field in ("slice_id", "base_sha", "head_sha")) and
                details["verdict"] == "pass", "review does not approve this exact integration pair", "stale")
        reviewers.add(details["reviewer"])
    item = next((s for s in c["slices"] if s["id"] == data["slice_id"]), None)
    require(item, "integration slice is outside current scope", "stale")
    required = 2 if item["risk"] == "high" or data.get("risk") == "high" else 1
    require(len(reviewers) >= required, "integration lacks required independent review attestations", "stale")


def extract_records(value):
    """Read envelopes or typed note lines, including bn show's log[].event."""
    if isinstance(value, dict):
        if {"key", "type", "scope_digest", "data"} <= set(value):
            yield value
        else:
            for key in ("log", "notes", "event", "records"):
                if key in value:
                    yield from extract_records(value[key])
    elif isinstance(value, list):
        for item in value:
            yield from extract_records(item)
    elif isinstance(value, str):
        import json
        for line in value.splitlines():
            if line.startswith("note — "):
                line = line[len("note — "):]
            if line.startswith("bn-mvp:v1 "):
                yield json.loads(line[len("bn-mvp:v1 "):])


def add_record(records, record):
    previous = records.get(record["key"])
    require(previous is None or previous == record, "event key already has different content", "conflict")
    records[record["key"]] = record
    return previous is not None


def assignments_from(records):
    assignments = {}
    for record in records.values():
        if record["type"] == "assignment":
            data = record["data"]
            previous = assignments.get(data["assignment_id"])
            if previous:
                require(all(previous[field] == data[field] for field in ("slice_id", "branch", "worktree")),
                        "assignment identity cannot change", "conflict")
                rank = {"active": 0, "stopped": 1, "integrated": 2}
                if rank[previous["status"]] > rank[data["status"]]:
                    continue
            assignments[data["assignment_id"]] = data
    return assignments


def record_event(record, repo, c, scope, store):
    validate_record(record, c, repo)
    require(record["scope_digest"] == scope, "record refers to an old contract", "stale")
    records = store.read("records.json", {})
    previous_assignments = assignments_from(records)
    duplicate = add_record(records, record)
    data, kind = record["data"], record["type"]
    if "slice_id" in data:
        require(data["slice_id"] in {s["id"] for s in c["slices"]}, "slice is outside selected scope")
    if kind == "scope":
        require(data["scope_revision"] == c["scope_revision"], "scope revision differs", "stale")
    if kind == "assignment":
        previous = previous_assignments.get(data["assignment_id"])
        require(duplicate or not previous or previous["status"] == "active" or data["status"] != "active",
                "terminal assignment cannot become active again", "conflict")
        assignments = assignments_from(records)
        active = [a for a in assignments.values() if a["status"] == "active"]
        require(len({a["slice_id"] for a in active}) == len(active), "slice already has an active assignment", "conflict")
        require(len(active) <= c["max_in_flight"], "max_in_flight exceeded", "conflict")
        if data.get("replaces"):
            old = assignments.get(data["replaces"])
            require(old and old["status"] == "stopped" and old["slice_id"] == data["slice_id"], "replacement needs a stopped assignment for this slice", "conflict")
    if kind == "integration":
        integration_proof(record, records, repo, c, target_sha(repo, c))
    if kind in ("demo", "manual-acceptance", "final-pr"):
        require(data["head_sha"] == target_sha(repo, c), "evidence revision is not the current remote target", "stale")
    if kind == "demo":
        required = {(canonical(check["argv"]), check["cwd"]) for check in c["checks"]}
        actual = {(canonical(check["argv"]), check["cwd"]) for check in data["checks"]}
        require(required <= actual, "demo is missing a required repository check", "stale")
        publication = store.read("publication.json", {})
        published = set(publication.get("keys", [])) if publication.get("scope_digest") == scope else set()
        integrated = set()
        for key, integration in records.items():
            if key in published and integration["type"] == "integration" and integration["scope_digest"] == scope:
                integration_proof(integration, records, repo, c, data["head_sha"])
                integrated.add(integration["data"]["slice_id"])
        require({s["id"] for s in c["slices"]} <= integrated,
                "demo requires published and verified integrations for all slices; reconcile with --hub first", "stale")
    if kind == "final-pr":
        publication = store.read("publication.json", {})
        published = set(publication.get("keys", [])) if publication.get("scope_digest") == scope else set()
        require(any(key in published and r["type"] == "manual-acceptance" and r["scope_digest"] == scope and
                    r["data"]["head_sha"] == data["head_sha"] for key, r in records.items()),
                "final PR needs acceptance of this exact revision and scope", "stale")
    store.write("records.json", records)
    return {"record": record, "note": "bn-mvp:v1 " + canonical(record), "idempotent": duplicate, "published": False}


def reconcile(repo, c, scope, store, notes, live):
    records = store.read("records.json", {})
    published = set()
    for record in extract_records(notes):
        validate_record(record, c, repo)
        add_record(records, record)
        published.add(record["key"])
    target = target_sha(repo, c)
    integrated, blocked = set(), []
    for record in records.values():
        if record["type"] == "integration" and record["scope_digest"] == scope:
            try:
                integration_proof(record, records, repo, c, target)
                # A cached integration is recovery work until its typed note is published.
                if record["key"] in published:
                    integrated.add(record["data"]["slice_id"])
            except Failure as exc:
                blocked.append({"key": record["key"], "reason": exc.message})
    assignments = assignments_from(records)
    recovered = [{**data, "harness_state": "live" if key in live else "inspect-before-replacement"}
                 for key, data in assignments.items()]
    occupied = {a["slice_id"] for a in recovered if a["status"] == "active" or a["assignment_id"] in live}
    available = max(0, c["max_in_flight"] - len(occupied))
    dispatchable = [s["id"] for s in c["slices"] if s["id"] not in integrated | occupied
                    and set(s["depends_on"]) <= integrated]
    if blocked:
        dispatchable = []
    store.write("records.json", records)
    return {"scope_digest": scope, "target_sha": target, "integrated": sorted(integrated),
            "assignments": recovered, "dispatchable": dispatchable[:available], "blocked": blocked,
            "stale_records": [k for k, r in records.items() if r["scope_digest"] != scope or
                              (r["type"] in ("demo", "manual-acceptance", "final-pr") and r["data"]["head_sha"] != target)],
            "pending_records": [k for k in records if k not in published]}
