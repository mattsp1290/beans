#!/usr/bin/env python3
"""Replay successful CLI mutations and compare validated identities and Git effects."""
import argparse
import base64
from datetime import datetime, timedelta, timezone
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile

from runner import CONTRACT, encode, environment, fixture, snapshot
from reference import authenticate_capture


def git(root, env, *args):
    return subprocess.run(["git", "-C", str(root), *args], env=env, check=True,
                          capture_output=True, timeout=15).stdout


class Identities:
    def __init__(self, root):
        self.root = root
        self.ids = {}
        self.reserved = set()
        for path in (root / "hub").rglob("*.md"):
            match = re.search(r"(?m)^id: (\S+)$", path.read_text())
            if match:
                self.reserved.add(match[1])
        self.commits = {}
        self.nonces = {}
        self.previous = {}
        self.start = datetime.now(timezone.utc)
        self.end = self.start

    def bind(self, name, value, namespace):
        if not re.fullmatch(re.escape(namespace) + r"[a-z0-9]{4}", value):
            raise ValueError(f"{name}: invalid generated ID namespace/alphabet/length: {value}")
        if value in self.reserved or value in self.ids.values() or name in self.ids:
            raise ValueError("generated ID binding is not bijective")
        self.ids[name] = value

    def argument(self, value):
        value = value.replace("${FIXTURE_ROOT}", str(self.root))
        for name, identity in self.ids.items():
            value = value.replace("${" + name + "}", identity)
        if re.search(r"\$\{\w+\}", value):
            raise ValueError(f"unbound journey argument: {value}")
        return value

    def text(self, value):
        value = value.replace(str(self.root), "${FIXTURE_ROOT}")
        for name, identity in self.ids.items():
            value = value.replace(identity, "${" + name + "}")
        for sha, token in self.commits.items():
            value = value.replace(sha, token)
        return value

    def timestamp(self, value):
        # Fixed seed timestamps remain literal. Only runtime-owned fields call
        # this function; body prose and arbitrary timestamps are never stripped.
        if not re.fullmatch(r"\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d{1,9})?Z", value):
            raise ValueError(f"invalid runtime UTC timestamp: {value}")
        instant = datetime.fromisoformat(value)
        if instant < self.start - timedelta(seconds=2):
            return value
        if not self.start - timedelta(seconds=2) <= instant <= self.end + timedelta(seconds=2):
            raise ValueError(f"runtime timestamp outside journey interval: {value}")
        return "${NOW_NANO}" if "." in value else "${NOW_SECOND}"

    def markdown(self, value):
        parts = value.split("\n---\n", 1) if value.startswith("---\n") else []
        if parts:
            parts[0] = re.sub(r'''(?m)^(created|updated): (["']?)([^"'\s]+)\2$''',
                              lambda m: m[1] + ": " + m[2] + self.timestamp(m[3]) + m[2], parts[0])
            value = "\n---\n".join(parts)
        # Existing ordered event lines are retained; runtime timestamps alone
        # vary. Validate chronology before substitution, including seed events.
        section = re.search(r"(?m)^## Log\n", value)
        if section is None:
            return self.text(value)
        following = re.search(r"(?m)^## ", value[section.end():])
        end = section.end() + following.start() if following else len(value)
        log = value[section.end():end]
        times = re.findall(r"(?m)^- (\d{4}-\d\d-\d\dT\S+) ", log)
        if any(datetime.fromisoformat(a) > datetime.fromisoformat(b) for a, b in zip(times, times[1:])):
            raise ValueError("non-monotone log timestamps")
        log = re.sub(r"(?m)^- (\d{4}-\d\d-\d\dT\S+) ",
                     lambda m: "- " + self.timestamp(m[1]) + " ", log)
        value = value[:section.end()] + log + value[end:]
        return self.text(value)

    def output(self, data, raw=False):
        value = data.decode()
        if raw:
            return encode(self.markdown(value).encode())
        # Replace only designated JSON DTO time fields and raw log-line time
        # fields, preserving every other byte of JSON spelling and indentation.
        value = re.sub(r'("(?:created|updated|at|last_fetch|Created|Updated|At)":\s*")([^"\\]*)(")',
                       lambda m: m[1] + self.timestamp(m[2]) + m[3], value)
        value = re.sub(r'("raw":\s*"- )(\d{4}-\d\d-\d\dT\S+?)( )',
                       lambda m: m[1] + self.timestamp(m[2]) + m[3], value)
        return encode(self.text(value).encode())

    def human_output(self, data, case, phase_start):
        value = data.decode()
        command = case["argv"][0]
        summaries = {"update", "note", "close", "reopen", "delete", "remember", "forget"}
        mutation_leaves = {("dep", "add"), ("dep", "remove"), ("doc", "new"),
                          ("project", "create"), ("project", "link"),
                          ("plan", "put"), ("plan", "link"), ("plan", "unlink"),
                          ("request", "update"), ("request", "link"), ("request", "unlink"),
                          ("handoff", "create"), ("handoff", "attach"), ("handoff", "detach"),
                          ("handoff", "restore")}
        if command in summaries or tuple(case["argv"][:2]) in mutation_leaves:
            match = re.match(r"\A([^\n]* \()([0-9a-f]{7})(\)\n)", value)
            if match:
                owners = [token for sha, token in self.commits.items() if sha.startswith(match[2])]
                if len(owners) != 1:
                    raise ValueError("human commit summary does not identify one observed commit")
                value = value[:match.start(2)] + owners[0][:-1] + "_SHORT}" + value[match.end(2):]
        if case["argv"][:2] == ["import", "bd"]:
            match = re.search(r"(?m)^imported into project [^\n]+ \(([0-9a-f]{7})\)\n", value)
            if match:
                owners = [token for sha, token in self.commits.items() if sha.startswith(match[1])]
                if len(owners) != 1:
                    raise ValueError("human import summary does not identify one observed commit")
                value = value[:match.start(1)] + owners[0][:-1] + "_SHORT}" + value[match.end(1):]
        if command == "show" and not case.get("raw"):
            before, separator, log = value.rpartition("\nlog:\n")
            if separator:
                identity = self.argument(case["argv"][1])
                paths = [p for p in (self.root / "hub").rglob("*.md")
                         if re.search(r"(?m)^id: " + re.escape(identity) + r"$", p.read_text())]
                if len(paths) != 1:
                    raise ValueError("human log requires one persisted issue")
                text = paths[0].read_text()
                section = re.search(r"(?m)^## Log\n", text)
                stored = text[section.end():] if section else ""
                following = re.search(r"(?m)^## ", stored)
                stored = stored[:following.start()] if following else stored
                for entry in re.finditer(r"(?m)^- (\S+) (\S+?)(?: \([^\n]+\))?: (.*)$", stored):
                    stamp, actor, event = entry.groups()
                    display = datetime.fromisoformat(stamp).strftime("%Y-%m-%d %H:%M")
                    expected = "  " + display + " " + actor + ": " + event + "\n"
                    if expected not in log:
                        raise ValueError("human log differs from persisted event/date")
                    if self.timestamp(stamp).startswith("${"):
                        normalized = "  ${NOW_MINUTE} " + actor + ": " + event + "\n"
                        log = log.replace(expected, normalized, 1)
                value = before + separator + log
        if command == "status":
            match = re.search(r"(?m)^last fetch: (?:(\d+)h)?(?:(\d+)m)?(\d+)s ago$", value)
            if match:
                fetched = datetime.fromisoformat((self.root / "beans/cache/last-fetch").read_text().strip())
                age = int(match[1] or 0) * 3600 + int(match[2] or 0) * 60 + int(match[3])
                lower = int((phase_start - fetched).total_seconds() + 0.5)
                upper = int((self.end - fetched).total_seconds() + 0.5)
                if not lower <= age <= upper:
                    raise ValueError("human fetch age differs from persisted time/process interval")
                value = value[:match.start()] + "last fetch: ${FETCH_AGE} ago" + value[match.end():]
        return self.output(value.encode(), case.get("raw", False))

    def metadata(self):
        result = {}
        for path in (self.root / "hub").rglob("*.md"):
            text = path.read_text()
            if not text.startswith("---\n"):
                continue
            header = text.split("\n---\n", 1)[0]
            identity = re.search(r"(?m)^(?:id|key): (\S+)$", header)
            if identity is None:
                continue
            times = {m[1]: m[3] for m in re.finditer(r'''(?m)^(created|updated): (["']?)([^"'\s]+)\2$''', header)}
            for key, instant in times.items():
                self.timestamp(instant)
                previous = self.previous.get((identity[1], key))
                if previous is not None:
                    if key == "created" and previous != instant:
                        raise ValueError("mutation changed immutable creation time")
                    if datetime.fromisoformat(instant) < datetime.fromisoformat(previous):
                        raise ValueError("mutation moved an owned timestamp backwards")
                self.previous[(identity[1], key)] = instant
            result[identity[1]] = times
        return result

    def validate_output(self, data, metadata):
        text = data.decode().lstrip()
        if not text.startswith(("{", "[")):
            return
        def walk(value):
            if isinstance(value, dict):
                identity = value.get("id", value.get("ID", value.get("plan_id")))
                if isinstance(identity, str) and identity in metadata:
                    for key in ("created", "updated", "Created", "Updated"):
                        if key in value and key.lower() in metadata[identity]:
                            actual = datetime.fromisoformat(value[key]).replace(microsecond=0)
                            stored = datetime.fromisoformat(metadata[identity][key.lower()]).replace(microsecond=0)
                            if actual != stored:
                                raise ValueError("DTO timestamp differs from the persisted artifact revision")
                for child in value.values():
                    walk(child)
            elif isinstance(value, list):
                for child in value:
                    walk(child)
        walk(json.loads(text))

    def history(self, root, env):
        shas = git(root / "hub", env, "rev-list", "--reverse", "--topo-order", "--all").decode().splitlines()
        for sha in shas:
            if not re.fullmatch(r"[0-9a-f]{40}", sha):
                raise ValueError("invalid Git object identity")
            self.commits.setdefault(sha, "${COMMIT_" + str(len(self.commits)) + "}")
        commits = []
        for sha in shas:
            parents, body, author, email = git(root / "hub", env, "show", "-s",
                                              "--format=%P%x00%B%x00%an%x00%ae", sha).decode().rstrip("\n").split("\0")
            for parent in parents.split():
                if parent not in self.commits:
                    raise ValueError("commit parent missing from observed ancestry")
            trailers = re.findall(r"(?m)^Bn-Run: (.+)$", body)
            if body.startswith("bn:"):
                if len(trailers) != 1 or not re.fullmatch(r"[0-9a-f]{16}", trailers[0]):
                    raise ValueError("operation commit missing valid nonce ownership trailer")
                nonce = trailers[0]
                owner = self.nonces.setdefault(nonce, sha)
                if owner != sha:
                    raise ValueError("distinct operation commits reused one nonce")
                body = body.replace("Bn-Run: " + nonce, "Bn-Run: ${NONCE_" + str(shas.index(sha)) + "}")
            commits.append({"sha": self.commits[sha], "parents": [self.commits[p] for p in parents.split()],
                            "message": self.text(body), "author": author, "email": email})
        refs = self.text(git(root / "hub", env, "show-ref").decode())
        remote = self.text(git(root / "remote/.git", env, "show-ref").decode())
        return {"commits": commits, "refs": refs, "remote_refs": remote,
                "status": self.text(git(root / "hub", env, "status", "--porcelain=v1", "--untracked-files=all").decode()),
                "staged": encode(git(root / "hub", env, "diff", "--cached", "--binary")),
                "clone_refs": self.text(git(root / "newhub", env, "show-ref").decode()) if (root / "newhub/.git").exists() else None}

    def files(self, root):
        result = {}
        for name, data in snapshot(root).items():
            data = base64.b64decode(data)
            if name.endswith(".md"):
                data = self.markdown(data.decode()).encode()
            elif name in ("beans/cache/last-fetch", "beans/cache/last-fetch-attempt"):
                data = (self.timestamp(data.decode().rstrip("\n")) + "\n").encode()
            else:
                data = data.replace(os.fsencode(root), b"${FIXTURE_ROOT}")
            result[self.text(name)] = encode(data)
        return result


def setup(root, env):
    fixture(root, "seeded", env)
    old = (root / "hub/projects/alpha/archive/2025/alpha-789a-old-contract.md").read_text()
    (root / "hub/projects/alpha/issues/alpha-9999-aged.md").write_text(old.replace("alpha-789a", "alpha-9999"))
    git(root / "hub", env, "add", ".")
    git(root / "hub", env, "commit", "-m", "aged fixture")
    git(root / "cwd", env, "init", "--initial-branch=main")
    (root / "cwd/README.md").write_text("Synthetic code repository.\n")
    git(root / "cwd", env, "add", ".")
    git(root / "cwd", env, "commit", "-m", "code seed")
    # The synthetic code remote is read for project resolution, never fetched.
    git(root / "cwd", env, "remote", "add", "origin", str(root / "code/.git"))
    (root / "remote").mkdir()
    git(root / "remote", env, "init", "--bare", "--initial-branch=main", ".git")
    git(root / "hub", env, "remote", "add", "origin", str(root / "remote/.git"))
    git(root / "hub", env, "push", "-u", "origin", "main")
    imported = {"id": "alpha-import1", "title": "Imported journey issue", "status": "open", "priority": 2,
                "issue_type": "task", "created_at": "2026-01-01T00:00:00Z",
                "updated_at": "2026-01-01T00:00:00Z", "dependencies": [], "labels": ["legacy"]}
    (root / "cwd/import.jsonl").write_text(json.dumps(imported) + "\n")


def steps():
    result = []
    def step(argv, **kwargs):
        result.append(dict(argv=argv, stdin_b64="", **kwargs))
    step(["init", "${FIXTURE_ROOT}/remote/.git", "--hub", "${FIXTURE_ROOT}/newhub"])
    step(["doctor"])
    step(["create", "Journey issue", "-l", "one,two", "-l", "three", "--silent"], bind="ISSUE", namespace="alpha-")
    step(["show", "${ISSUE}"])
    step(["update", "${ISSUE}", "--claim", "--description", "Keep <HTML> & café", "--note", "Claimed"])
    step(["note", "${ISSUE}", "A second event"])
    step(["dep", "add", "${ISSUE}", "alpha-a1b2"])
    step(["blocked"])
    step(["dep", "remove", "${ISSUE}", "alpha-a1b2"])
    step(["dep", "add", "${ISSUE}", "alpha-a1b2", "-t", "parent-child"])
    step(["children", "alpha-a1b2"])
    step(["dep", "remove", "${ISSUE}", "alpha-a1b2", "-t", "parent-child"])
    step(["close", "${ISSUE}", "-r", "Journey done"])
    step(["close", "${ISSUE}", "-r", "Idempotent close"])
    step(["reopen", "${ISSUE}"])
    step(["request", "create", "Journey request", "--stdin", "--issue", "${ISSUE}", "--silent"],
         bind="REQUEST", namespace="alpha-r-", stdin="## Request\n\nA literal timestamp 2024-01-01T00:00:00Z stays.\n")
    step(["request", "show", "${REQUEST}"])
    step(["request", "update", "${REQUEST}", "--status", "accepted", "--description", "Accepted body"])
    step(["request", "link", "${REQUEST}", "alpha-a1b2"])
    step(["request", "unlink", "${REQUEST}", "${ISSUE}"])
    step(["handoff", "create", "--file", "-", "--issue", "${ISSUE}", "--silent"],
         bind="HANDOFF", namespace="alpha-", stdin="# Journey handoff\n\nKeep café & <HTML>.\n")
    step(["handoff", "show", "${HANDOFF}", "--raw"], raw=True)
    step(["handoff", "detach", "${HANDOFF}"])
    step(["handoff", "attach", "${HANDOFF}", "alpha-a1b2"])
    step(["handoff", "archive", "${HANDOFF}", "--dry-run"])
    step(["handoff", "archive", "${HANDOFF}"])
    step(["handoff", "archive", "${HANDOFF}"])
    step(["handoff", "restore", "${HANDOFF}"])
    step(["remember", "Journey memory <HTML> & café", "--key", "journey", "--tag", "one,two", "--tag", "three"])
    step(["memories", "Journey"])
    step(["forget", "journey"])
    step(["doc", "new", "journey.md"])
    step(["doc", "list"])
    step(["project", "create", "beta"])
    step(["project", "link", "beta"])
    step(["plan", "init", "Journey plan", "--output", "draft"], bind="PLAN", namespace="alpha-plan-")
    step(["plan", "validate", "draft"])
    step(["plan", "put", "draft"], online=True)
    step(["plan", "get", "${PLAN}", "--output", "retrieved"])
    step(["plan", "show", "${PLAN}"])
    step(["plan", "link", "alpha-plan-1234", "oracle", "${ISSUE}"], online=True)
    step(["plan", "status", "alpha-plan-1234"])
    step(["plan", "unlink", "alpha-plan-1234", "oracle", "${ISSUE}"], online=True)
    step(["delete", "${ISSUE}", "--force"])
    step(["archive", "--older-than", "30d", "--dry-run"])
    step(["archive", "--older-than", "30d"])
    step(["import", "bd", "import.jsonl", "--project", "beta"])
    step(["sync"], online=True)
    step(["status"], online=True)
    step(["cache", "clear"])
    for i, item in enumerate(result):
        item["id"] = f"journey-{i:03}:" + "-".join(item["argv"][:2])
        item["stdin_b64"] = encode(item.pop("stdin", "").encode())
    return result


def file_steps():
    cases = [
        {"argv": ["create", "File inputs issue", "--actor", "File Author"], "bind": "ISSUE", "namespace": "alpha-"},
        {"argv": ["request", "create", "File request", "--body-file", "request.md", "--issue", "${ISSUE}",
                  "--requested-by", "File requester", "-l", "one,two", "-l", "three"], "bind": "REQUEST", "namespace": "alpha-r-"},
        {"argv": ["request", "show", "${REQUEST}"]},
        {"argv": ["request", "update", "${REQUEST}", "--body-file", "replacement.md", "--status", "accepted"]},
        {"argv": ["request", "show", "${REQUEST}"]},
        {"argv": ["handoff", "create", "--file", "handoff.md", "--issue", "${ISSUE}"], "bind": "HANDOFF", "namespace": "alpha-"},
        {"argv": ["handoff", "show", "${HANDOFF}", "--raw"], "raw": True},
        {"argv": ["show", "${ISSUE}", "--raw"], "raw": True},
        {"argv": ["show", "${ISSUE}"]},
    ]
    for i, case in enumerate(cases):
        case.update(id=f"file-journey-{i:02}:" + "-".join(case["argv"][:2]), stdin_b64="")
    return cases


def execute(binary, corpus):
    with tempfile.TemporaryDirectory(prefix="beans-journey-") as directory:
        root = Path(directory)
        env = environment(root)
        setup(root, env)
        for name, value in corpus.get("initial_files", {}).items():
            path = Path(name)
            if path.is_absolute() or ".." in path.parts:
                raise ValueError("journey initial file must stay within the temporary root")
            target = root / path
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(base64.b64decode(value, validate=True))
        identities = Identities(root)
        results = []
        for case in corpus["steps"]:
            argv = ["--project", "alpha", "--no-fetch"] + [identities.argument(a) for a in case["argv"]]
            if not case.get("online") and not corpus.get("push_each", False):
                argv.append("--no-sync")
            if not case.get("raw") and corpus.get("output", "json") == "json":
                argv.append("--json")
            phase_start = datetime.now(timezone.utc)
            process = subprocess.run([str(binary), *argv], cwd=root / "cwd", env=env,
                                     input=base64.b64decode(case["stdin_b64"]),
                                     capture_output=True, timeout=20)
            identities.end = datetime.now(timezone.utc)
            if process.returncode != 0:
                raise RuntimeError(f"{case['id']}: exit {process.returncode}: {process.stderr.decode()}")
            if "bind" in case:
                # Handoff's --silent wins over --json; issue/request JSON wins.
                out = process.stdout.decode()
                if out.lstrip().startswith("{"):
                    value = json.loads(out)["id"]
                elif case["argv"][:2] == ["plan", "init"]:
                    match = re.fullmatch(r"created draft \(([^\n]+)\)\nnext: bn plan validate draft\n", out)
                    if match is None:
                        raise ValueError("unexpected plan scaffold binding output")
                    value = match[1]
                elif case["argv"][0] == "create" or case["argv"][:2] == ["request", "create"]:
                    title = case["argv"][1] if case["argv"][0] == "create" else case["argv"][2]
                    match = re.fullmatch(r"created (\S+): " + re.escape(title) + r"\n", out)
                    value = match[1] if match else out.rstrip("\n")
                elif case["argv"][:2] == ["handoff", "create"] and "--silent" not in case["argv"]:
                    match = re.fullmatch(r"created handoff (\S+) \([0-9a-f]{7}\)\n", out)
                    if match is None:
                        raise ValueError("unexpected handoff creation binding output")
                    value = match[1]
                else:
                    value = out.rstrip("\n")
                identities.bind(case["bind"], value, case["namespace"])
            history = identities.history(root, env)
            metadata = identities.metadata()
            if not case.get("raw"):
                identities.validate_output(process.stdout, metadata)
            stdout = identities.human_output(process.stdout, case, phase_start) if corpus.get("output") == "text" else identities.output(process.stdout, case.get("raw", False))
            results.append({"stdout_b64": stdout,
                            "stderr_b64": identities.output(process.stderr), "exit": process.returncode,
                            "files": identities.files(root), "git": history})
        return results


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=["capture", "check"])
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--corpus", type=Path, default=CONTRACT / "journey.json")
    parser.add_argument("--push-each", action="store_true", help="capture normal pushed mutations rather than offline mutations")
    parser.add_argument("--text", action="store_true", help="capture successful human output instead of JSON")
    parser.add_argument("--files", action="store_true", help="capture the focused file-input journey")
    args = parser.parse_args()
    if args.mode == "capture":
        authenticate_capture(args.binary.resolve(strict=True))
    corpus = {"schema": "beans-mutation-journey-v1", "push_each": args.push_each, "steps": steps()} if args.mode == "capture" else json.loads(args.corpus.read_text())
    if args.mode == "capture" and args.text:
        corpus["output"] = "text"
    if args.mode == "capture" and args.files:
        corpus["steps"] = file_steps()
        corpus["initial_files"] = {"cwd/" + name: encode(body.encode()) for name, body in {
            "request.md": "# File request\n\nKeep café & <HTML>, [[Guide]], and 2024-01-01T00:00:00Z.\n",
            "replacement.md": "# Replacement\n\nExact replacement body.\n",
            "handoff.md": "# File handoff\n\nKeep leading and trailing spaces.  \n",
        }.items()}
    results = execute(args.binary.resolve(strict=True), corpus)
    if args.mode == "capture":
        for case, result in zip(corpus["steps"], results, strict=True):
            case["expected"] = result
        args.corpus.write_text(json.dumps(corpus, indent=2) + "\n")
        print(f"Captured {len(results)} mutation journey steps")
    else:
        failures = [c["id"] + ": " + ", ".join(k for k in r if r[k] != c["expected"][k])
                    for c, r in zip(corpus["steps"], results, strict=True) if c["expected"] != r]
        if failures:
            parser.exit(1, "Journey mismatches:\n" + "\n".join(failures) + "\n")
        print(f"Passed {len(results)} mutation journey steps")


if __name__ == "__main__":
    main()
