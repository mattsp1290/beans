"""Strict version-one contract validation; no hub reads or mutations."""
import json
from pathlib import Path, PurePosixPath
import re
from urllib.parse import urlsplit

from mvp_common import digest, git, require

IDENTIFIER = re.compile(r"[a-z0-9][a-z0-9-]*\Z")
SHA = re.compile(r"(?:[0-9a-f]{40}|[0-9a-f]{64})\Z")


def string(value):
    require(isinstance(value, str) and bool(value.strip()) and
            all(ord(c) >= 32 and ord(c) != 127 for c in value), "expected a nonempty string without control characters")


def identifier(value):
    string(value)
    require(IDENTIFIER.fullmatch(value), "IDs, project, and key must be lowercase letters, numbers, hyphens")


def strings(value, *, nonempty=True):
    require(isinstance(value, list) and (bool(value) or not nonempty), "expected string array")
    for item in value:
        string(item)


def relative_path(repo, value):
    string(value)
    path = PurePosixPath(value)
    require(not path.is_absolute() and ".." not in path.parts and "\\" not in value,
            "paths must be repository relative without traversal", "unsafe-path")
    require((repo / value).resolve().is_relative_to(repo), "repository path escapes through a symlink", "unsafe-path")


def ref(repo, value):
    string(value)
    require(value.startswith("refs/heads/"), "expected a full branch ref")
    git(repo, "check-ref-format", value)


def identity(value):
    string(value)
    if value.startswith("/"):
        return str(Path(value).resolve())
    if re.match(r"[^/@:]+@[^/:]+:.+", value):
        userhost, path = value.split(":", 1)
        user, host = userhost.split("@", 1)
        require(user == "git", "scp-style repository identity must use git user")
        scheme, port = "ssh", None
    else:
        parsed = urlsplit(value)
        require(parsed.scheme in ("https", "ssh") and parsed.hostname and not parsed.password
                and not parsed.query and not parsed.fragment,
                "repository must be credential-free HTTPS, SSH, scp Git address, or absolute fixture path")
        require(parsed.username in (None, "git") if parsed.scheme == "ssh" else parsed.username is None,
                "repository URL cannot contain credentials")
        host, path, scheme, port = parsed.hostname, parsed.path, parsed.scheme, parsed.port
    path = path.strip("/")
    require(path and ".." not in path.split("/") and not any(c.isspace() for c in path), "invalid repository path")
    if path.endswith(".git"):
        path = path[:-4]
    host = host.lower()
    if host == "github.com" and port in (None, 22, 443):
        return "github.com/" + path.lower()
    return f"{scheme}://{host}{':' + str(port) if port else ''}/{path}"


def load_contract(path, repo):
    text = Path(path).read_text(encoding="utf-8")
    return parse_contract(text, repo)


def parse_contract(text, repo):
    if text.lstrip().startswith("{"):
        contract = json.loads(text)
    else:
        fences = re.findall(r"^```bn-mvp-contract\s*\n(.*?)^```\s*$", text, re.M | re.S)
        require(len(fences) == 1, "expected exactly one bn-mvp-contract JSON fence")
        contract = json.loads(fences[0])
    validate(contract, repo)
    return contract, digest(contract)


def validate(c, repo):
    required = "version milestone_id key project repository remote target_ref default_ref base_sha scope_revision outcome non_goals context authority acceptance checks prerequisites runtime_identity max_in_flight slices".split()
    require(isinstance(c, dict) and set(c) == set(required), "contract has missing or unknown keys")
    require(type(c["version"]) is int and c["version"] == 1, "only integer contract version 1 is supported")
    for field in ("scope_revision", "max_in_flight"):
        require(type(c[field]) is int and c[field] > 0, field + " must be a positive integer")
    for field in ("milestone_id", "key", "project"):
        identifier(c[field])
    identity(c["repository"])
    string(c["remote"])
    require(re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_.-]*", c["remote"]), "remote must be a configured remote name")
    for field in ("target_ref", "default_ref"):
        ref(repo, c[field])
    require(c["target_ref"] not in (c["default_ref"], "refs/heads/main") and
            not c["target_ref"].startswith(("refs/heads/bn-mvp-owner/", "refs/heads/bn-mvp-milestone/")), "forbidden target branch")
    require(isinstance(c["base_sha"], str) and SHA.fullmatch(c["base_sha"]), "base_sha must be a full object ID")
    for field in ("outcome", "runtime_identity"):
        string(c[field])
    for field in ("non_goals", "prerequisites", "acceptance"):
        strings(c[field], nonempty=field == "acceptance")
    for field, booleans in (("context", ("active_users", "preserve_compatibility")),
                            ("authority", ("integrate_target", "main_merge", "deploy"))):
        require(isinstance(c[field], dict) and set(c[field]) == {*booleans, "source"}, "invalid " + field)
        for key in booleans:
            require(type(c[field][key]) is bool, field + "." + key + " must be boolean")
        string(c[field]["source"])
    require(c["authority"]["integrate_target"] and not c["authority"]["main_merge"] and not c["authority"]["deploy"], "v1 requires target integration authority and excludes main merge/deploy")
    require(isinstance(c["checks"], list) and c["checks"], "checks cannot be empty")
    for check in c["checks"]:
        require(isinstance(check, dict) and set(check) == {"argv", "cwd"}, "invalid check")
        strings(check["argv"])
        require(not check["argv"][0].startswith("-"), "check executable cannot be option-like")
        relative_path(repo, check["cwd"])
    require(isinstance(c["slices"], list) and c["slices"], "slices cannot be empty")
    graph = {}
    for item in c["slices"]:
        require(isinstance(item, dict) and set(item) == {"id", "outcome", "depends_on", "areas", "acceptance", "risk"}, "invalid slice")
        identifier(item["id"])
        require(item["id"] != c["milestone_id"] and item["id"] not in graph, "duplicate/root slice ID")
        string(item["outcome"])
        strings(item["acceptance"])
        strings(item["depends_on"], nonempty=False)
        strings(item["areas"])
        for area in item["areas"]:
            relative_path(repo, area)
        require(item["risk"] in ("routine", "high"), "invalid risk")
        graph[item["id"]] = item["depends_on"]
    visiting, visited = set(), set()
    def visit(node):
        require(node in graph, "dependency is outside selected slices")
        require(node not in visiting, "dependency cycle")
        if node in visited:
            return
        visiting.add(node)
        for dependency in graph[node]:
            visit(dependency)
        visiting.remove(node)
        visited.add(node)
    for node in graph:
        visit(node)
