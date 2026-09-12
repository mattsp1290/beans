#!/usr/bin/env python3
"""State-backed bn read double. It refuses paths outside its fixture root."""

import json
import os
from pathlib import Path
import sys

root = Path(os.environ["BN_PLAN_LOOP_FIXTURE_ROOT"]).resolve()
state_path = Path(os.environ["BN_PLAN_LOOP_FAKE_STATE"]).resolve()
trace_path = Path(os.environ["BN_PLAN_LOOP_TRACE"]).resolve()
for path in (state_path, trace_path):
    if root not in path.parents:
        raise SystemExit("fixture path escapes temporary root")
args = sys.argv[1:]
with trace_path.open("a") as trace:
    trace.write(json.dumps(args) + "\n")
state = json.loads(state_path.read_text())
key = " ".join(x for x in args if x not in {"--json", "--no-fetch"})
responses = state.get("responses", {}).get(key)
if not responses:
    print(f"fake-bn: unsupported command: {key}", file=sys.stderr)
    raise SystemExit(2)
if isinstance(responses, list):
    value = responses.pop(0) if len(responses) > 1 else responses[0]
    state["responses"][key] = responses
    state_path.write_text(json.dumps(state))
else:
    value = responses
if isinstance(value, dict) and "error" in value:
    print(value["error"], file=sys.stderr); raise SystemExit(value.get("code", 1))
print(json.dumps(value))
