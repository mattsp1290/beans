"""Immutable remote reservation and invocation-long local lock keeper."""
import os
import secrets
import signal

from mvp_common import canonical, digest, git, require, run
from mvp_contract import identity


def refs(c):
    return ("refs/heads/bn-mvp-owner/" + digest(c["target_ref"]),
            "refs/heads/bn-mvp-milestone/" + digest(c["project"] + ":" + c["milestone_id"]))


def remote_refs(repo, remote, *wanted):
    lines = git(repo, "ls-remote", remote, *wanted).splitlines()
    return dict((line.split()[1], line.split()[0]) for line in lines if line.strip())


def verify_remote(repo, c):
    require(c["remote"] in git(repo, "remote").splitlines(), "configured remote is missing", "identity")
    urls = git(repo, "remote", "get-url", "--all", c["remote"]).splitlines()
    urls += git(repo, "remote", "get-url", "--push", "--all", c["remote"]).splitlines()
    require(urls and all(identity(url) == identity(c["repository"]) for url in urls),
            "fetch/push remote identities disagree with contract", "identity")
    advertised = git(repo, "ls-remote", "--symref", c["remote"], "HEAD")
    defaults = [line.split()[1] for line in advertised.splitlines() if line.startswith("ref:")]
    require(defaults == [c["default_ref"]], "remote default branch changed or cannot be determined", "identity")
    require(c["target_ref"] not in defaults + ["refs/heads/main"], "target is the remote default branch", "identity")


def owner_identity(c):
    return {"repository": identity(c["repository"]), "project": c["project"],
            "milestone_id": c["milestone_id"], "target_ref": c["target_ref"], "key": c["key"]}


def check_owner(repo, c, store):
    verify_remote(repo, c)
    owner = store.read("owner.json", {})
    require(owner.get("identity") == owner_identity(c) and owner.get("nonce") and owner.get("oid"),
            "no matching local reservation; lost caches cannot reclaim a remote owner", "ownership")
    wanted = refs(c)
    actual = remote_refs(repo, c["remote"], *wanted)
    require(all(actual.get(ref) == owner["oid"] for ref in wanted),
            "remote reservation/binding no longer matches; stop all mutation", "ownership")
    return {"owner_oid": owner["oid"], "owner_ref": wanted[0], "milestone_ref": wanted[1]}


def reserve(repo, c, store, allow_existing):
    verify_remote(repo, c)
    owner = store.read("owner.json")
    if owner:
        require(owner["identity"] == owner_identity(c), "local milestone identity or target changed", "ownership")
    wanted = refs(c)
    actual = remote_refs(repo, c["remote"], *wanted, c["target_ref"])
    if any(ref in actual for ref in wanted):
        return check_owner(repo, c, store)
    if c["target_ref"] in actual:
        require(allow_existing, "existing target requires explicit user selection and --allow-existing-target", "ownership")
        require(actual[c["target_ref"]] == c["base_sha"], "existing target differs from the contract starting revision", "stale")
    starting_ref = c["target_ref"] if c["target_ref"] in actual else c["default_ref"]
    git(repo, "fetch", "--no-tags", c["remote"], starting_ref)
    git(repo, "cat-file", "-e", c["base_sha"] + "^{commit}")
    require(run(["git", "merge-base", "--is-ancestor", c["base_sha"], "FETCH_HEAD"], repo, check=False).returncode == 0,
            "base commit is not reachable from the remote starting branch", "identity")
    if not owner:
        owner = {"identity": owner_identity(c), "nonce": secrets.token_hex(32)}
        # Persist the nonce before creating or pushing any reservation object.
        store.write("owner.json", owner)
    if "oid" not in owner:
        payload = {"version": 1, "target_ref": c["target_ref"],
                   "milestone_id": c["milestone_id"], "nonce": owner["nonce"]}
        blob = git(repo, "hash-object", "-w", "--stdin", input=canonical(payload) + "\n")
        tree = git(repo, "mktree", input=f"100644 blob {blob}\towner.json\n")
        environment = {"GIT_AUTHOR_NAME": "bn-mvp", "GIT_AUTHOR_EMAIL": "bn-mvp@localhost",
                       "GIT_COMMITTER_NAME": "bn-mvp", "GIT_COMMITTER_EMAIL": "bn-mvp@localhost"}
        owner["oid"] = git(repo, "-c", "commit.gpgsign=false", "commit-tree", tree,
                           input="bn-mvp immutable ownership reservation\n", env=environment)
        store.write("owner.json", owner)
    result = run(["git", "push", "--atomic", c["remote"],
                  *[owner["oid"] + ":" + ref for ref in wanted]], repo, check=False)
    # Read back even after failure: the response can be lost after a successful push.
    actual = remote_refs(repo, c["remote"], *wanted)
    require(all(actual.get(ref) == owner["oid"] for ref in wanted),
            "reservation rejected: another owner won, remote is unavailable, or atomic push is unsupported",
            "ownership" if actual else "external")
    return {**check_owner(repo, c, store), "push_exit": result.returncode}


def hold_lock(store, token, emit):
    require(isinstance(token, str) and len(token) >= 16, "caller token must contain at least 16 characters")
    with store.lock("coordinator.lock"):
        store.write("keeper.json", {"pid": os.getpid(), "token_digest": digest(token)})
        def stopped(signum, frame):
            raise SystemExit(0)
        signal.signal(signal.SIGTERM, stopped)
        signal.signal(signal.SIGINT, stopped)
        emit({"locked": True, "pid": os.getpid(), "state_dir": str(store.path)})
        while True:
            signal.pause()
