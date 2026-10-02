"""An explicit Beans freshness barrier; never blindly publishes hand edits."""
import json
import os
from pathlib import Path

from mvp_common import digest, git, require, run
from mvp_contract import SHA, parse_contract, string
from mvp_ownership import remote_refs


def sync_barrier(repo, c, hub, bn, allowed):
    hub = Path(hub).resolve()
    selected_home = os.environ.get("BEANS_HOME")
    selected_hub = os.environ.get("BEANS_HUB")
    require(selected_home and selected_hub and Path(selected_hub).resolve() == hub,
            "set exclusive role-specific BEANS_HOME and BEANS_HUB before a barrier", "identity")
    require(Path(selected_home).resolve() != (Path.home() / ".beans").resolve() and
            hub != (Path.home() / ".beans" / "hub").resolve(),
            "automated barriers cannot use the shared default Beans home/hub", "identity")
    require(not git(hub, "status", "--porcelain", "--untracked-files=all"),
            "hub has dirty/untracked files; preserve and identify them before sync", "unsynchronized")
    status = json.loads(run([bn, "status", "--json"], repo).stdout)
    require(Path(status["hub"]).resolve() == hub and status.get("project") == c["project"],
            "Beans resolved a different hub/project; set its environment explicitly", "identity")
    # Beans' hub pipeline always uses origin; status.remote is its URL, not name.
    remote, branch = "origin", status["branch"]
    require(status["remote"] == git(hub, "remote", "get-url", remote) and not branch.startswith("-"), "invalid hub remote/branch", "identity")
    git(hub, "fetch", "--no-tags", remote, branch)
    pending = git(hub, "rev-list", "FETCH_HEAD..HEAD").splitlines()
    allowed_shas = set()
    for intent in allowed:
        require(isinstance(intent, dict) and set(intent) == {"sha", "intent_key", "milestone_id"},
                "pending authorization needs sha, intent_key, milestone_id")
        require(isinstance(intent["sha"], str) and SHA.fullmatch(intent["sha"]) and
                intent["milestone_id"] == c["milestone_id"], "pending intent belongs to another milestone")
        string(intent["intent_key"])
        allowed_shas.add(intent["sha"])
    require(set(pending) <= allowed_shas, "hub has unidentified pending commits; inspect and supply --allow-pending intent records", "unsynchronized")
    known_ids = [c["milestone_id"], *[s["id"] for s in c["slices"]]]
    for sha in pending:
        paths = git(hub, "diff-tree", "--no-commit-id", "--name-only", "-r", sha).splitlines()
        require(paths and all(path.startswith(f"projects/{c['project']}/issues/") and
                              any(Path(path).name.startswith(i + "-") for i in known_ids) for path in paths),
                "pending commit changes files outside known milestone issues; recover manually", "unsynchronized")
    require(not git(hub, "status", "--porcelain", "--untracked-files=all"), "hub changed during preflight", "unsynchronized")
    synchronized = json.loads(run([bn, "sync", "--json"], repo).stdout)
    synchronized_status = synchronized.get("status")
    require(synchronized.get("synced") is True and isinstance(synchronized_status, dict) and
            synchronized_status.get("ahead") == 0 and synchronized_status.get("behind") == 0,
            "bn sync did not establish ahead=0, behind=0", "unsynchronized")
    head = git(hub, "rev-parse", "HEAD")
    actual = remote_refs(hub, remote, "refs/heads/" + branch)
    require(actual.get("refs/heads/" + branch) == head and
            not git(hub, "status", "--porcelain", "--untracked-files=all"),
            "hub is not clean and equal to its remote after sync", "unsynchronized")
    issue = json.loads(run([bn, "show", c["milestone_id"], "--json"], repo).stdout)
    _, published_scope = parse_contract(issue.get("description", ""), repo)
    require(published_scope == digest(c), "supplied contract differs from the synchronized root contract", "stale")
    require(git(hub, "rev-parse", "HEAD") == head and
            not git(hub, "status", "--porcelain", "--untracked-files=all"),
            "hub changed while reading evidence; repeat the barrier", "unsynchronized")
    return {"hub_head": head, "issue": issue, "freshness": "synchronized"}
