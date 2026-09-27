#!/usr/bin/env python3
"""Narrow standard-library MVP ownership/evidence CLI. No general command proxy."""
import argparse
import json
from pathlib import Path
import subprocess
import sys

sys.dont_write_bytecode = True

from mvp_common import Failure, Store, git, read_json, require
from mvp_contract import load_contract
from mvp_evidence import extract_records, record_event, reconcile
from mvp_ownership import check_owner, hold_lock, reserve
from mvp_sync import sync_barrier


def emit(value):
    print(json.dumps(value, sort_keys=True), flush=True)


class JSONParser(argparse.ArgumentParser):
    def error(self, message):
        raise Failure("invalid", message)


def parser():
    result = JSONParser(description=__doc__)
    commands = result.add_subparsers(dest="operation", required=True)
    for operation in ("validate-contract", "hold-lock", "reserve-target", "check-owner", "record", "reconcile", "sync-barrier"):
        command = commands.add_parser(operation)
        command.add_argument("--repo", required=True, help="source checkout root")
        command.add_argument("--contract", required=True, help="contract JSON or issue-description Markdown file")
        if operation != "validate-contract":
            command.add_argument("--token", required=True, help="caller-generated secret, at least 16 characters for hold-lock")
        if operation == "reserve-target":
            command.add_argument("--allow-existing-target", action="store_true")
        if operation == "record":
            command.add_argument("--record", required=True, help="JSON evidence envelope")
        if operation in ("reconcile", "sync-barrier"):
            command.add_argument("--hub", required=operation == "sync-barrier", help="expected isolated/configured Beans hub")
            command.add_argument("--bn", default="bn", help="Beans executable path")
            command.add_argument("--allow-pending", help="JSON array of {sha,intent_key,milestone_id} for inspected pending commits")
        if operation == "reconcile":
            command.add_argument("--notes", help="offline bn show JSON or evidence envelopes; cannot establish freshness")
            command.add_argument("--live-assignments", help="JSON array of assignment IDs still live in the harness")
    return result


def main(argv=None):
    args = parser().parse_args(argv)
    repo = Path(args.repo).resolve()
    require(Path(git(repo, "rev-parse", "--show-toplevel")).resolve() == repo, "--repo must be the source checkout root")
    contract, scope = load_contract(args.contract, repo)
    if args.operation == "validate-contract":
        return {"contract": contract, "scope_digest": scope}
    store = Store(repo, contract)
    if args.operation == "hold-lock":
        hold_lock(store, args.token, emit)
        return None
    store.require_keeper(args.token)
    with store.lock("operation.lock"):
        store.require_keeper(args.token)
        if args.operation == "reserve-target":
            return reserve(repo, contract, store, args.allow_existing_target)
        ownership = check_owner(repo, contract, store)
        if args.operation == "check-owner":
            return ownership
        if args.operation == "record":
            return record_event(read_json(args.record), repo, contract, scope, store)
        snapshot = None
        if args.hub:
            allowed = read_json(args.allow_pending) if args.allow_pending else []
            require(isinstance(allowed, list), "--allow-pending must contain a JSON array of pending intent records")
            snapshot = sync_barrier(repo, contract, args.hub, args.bn, allowed)
        if args.operation == "sync-barrier":
            return snapshot
        require(args.notes or snapshot, "reconcile requires --hub or --notes")
        notes = snapshot["issue"] if snapshot else read_json(args.notes)
        live = read_json(args.live_assignments) if args.live_assignments else []
        require(isinstance(live, list) and all(isinstance(s, str) for s in live), "live assignments must be a JSON array of IDs")
        result = reconcile(repo, contract, scope, store, notes, set(live))
        result["freshness"] = "synchronized" if snapshot else "unverified"
        if snapshot:
            result["hub_head"] = snapshot["hub_head"]
            store.write("publication.json", {"hub_head": snapshot["hub_head"], "scope_digest": scope,
                        "keys": [record["key"] for record in extract_records(notes)]})
        else:
            result["dispatchable"] = []
        return result


if __name__ == "__main__":
    try:
        output = main()
        if output is not None:
            emit(output)
    except Failure as exc:
        emit({"error": exc.category, "message": exc.message})
        sys.exit(1)
    except (OSError, ValueError, KeyError, TypeError, RecursionError, subprocess.TimeoutExpired) as exc:
        emit({"error": "invalid" if isinstance(exc, (ValueError, KeyError, TypeError, RecursionError)) else "external",
              "message": "input/state could not be processed: " + type(exc).__name__})
        sys.exit(1)
