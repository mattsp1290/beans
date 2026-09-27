"""Small filesystem and subprocess primitives for the MVP coordinator."""
import contextlib
import fcntl
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile


class Failure(Exception):
    def __init__(self, category, message):
        self.category, self.message = category, message
        super().__init__(message)


def require(condition, message, category="invalid"):
    if not condition:
        raise Failure(category, message)


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=True)


def digest(value):
    return hashlib.sha256(canonical(value).encode()).hexdigest()


def run(argv, cwd, *, input=None, env=None, check=True):
    environment = dict(os.environ, GIT_TERMINAL_PROMPT="0", GCM_INTERACTIVE="never")
    if env:
        environment.update(env)
    result = subprocess.run(argv, cwd=cwd, input=input, text=True,
                            capture_output=True, env=environment, timeout=120)
    if check and result.returncode:
        # Do not echo remote URLs, credentials, or arbitrary subprocess output.
        raise Failure("external", f"{Path(argv[0]).name} {argv[1]} failed (exit {result.returncode}); inspect locally")
    return result


def git(repo, *args, **kwargs):
    return run(["git", *args], repo, **kwargs).stdout.strip()


def read_json(path):
    with open(path, encoding="utf-8") as stream:
        return json.load(stream)


def safe_child(root, relative):
    path = root / relative
    require(not path.is_symlink(), f"symlink forbidden in state: {path}", "unsafe-path")
    require(path.resolve().is_relative_to(root.resolve()), "state path escaped its root", "unsafe-path")
    return path


def atomic_json(path, value):
    require(not path.is_symlink(), "state file is a symlink", "unsafe-path")
    fd, temporary = tempfile.mkstemp(prefix=".write-", dir=path.parent)
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as stream:
            stream.write(canonical(value) + "\n")
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, path)
        directory = os.open(path.parent, os.O_RDONLY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


class Store:
    def __init__(self, repo, contract):
        common = Path(git(repo, "rev-parse", "--path-format=absolute", "--git-common-dir")).resolve()
        root = safe_child(common, "bn-mvp")
        root.mkdir(mode=0o700, exist_ok=True)
        self.path = safe_child(root, contract["key"])
        self.path.mkdir(mode=0o700, exist_ok=True)

    def file(self, name):
        return safe_child(self.path, name)

    def read(self, name, default=None):
        path = self.file(name)
        return read_json(path) if path.exists() else default

    def write(self, name, value):
        atomic_json(self.file(name), value)

    @contextlib.contextmanager
    def lock(self, name):
        fd = os.open(self.file(name), os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600)
        try:
            try:
                fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError:
                raise Failure("locked", "another live process holds " + name) from None
            yield
        finally:
            os.close(fd)

    def require_keeper(self, token):
        keeper = self.read("keeper.json", {})
        require(token and keeper.get("token_digest") == digest(token),
                "start hold-lock and supply its caller token", "ownership")
        try:
            os.kill(keeper["pid"], 0)
        except (KeyError, ProcessLookupError, PermissionError):
            raise Failure("ownership", "lock keeper is no longer alive; restart it") from None
        try:
            with self.lock("coordinator.lock"):
                pass
        except Failure as exc:
            if exc.category == "locked":
                return
            raise
        raise Failure("ownership", "keeper is not holding the coordinator lock")
