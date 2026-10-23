#!/bin/sh
# Disposable two-actor prompt acceptance journey. No user hub or config is read.
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
BIN=${BN_BIN:-"$ROOT/target/debug/bn"}
case "$BIN" in
  /*) ;;
  *) BIN="$ROOT/$BIN" ;;
esac
if [ ! -x "$BIN" ]; then
  printf 'bn binary is not executable: %s\nBuild it with: cargo build --locked\n' "$BIN" >&2
  exit 2
fi
for command in git curl python3; do
  command -v "$command" >/dev/null 2>&1 || { printf 'missing command: %s\n' "$command" >&2; exit 2; }
done

TMP=$(mktemp -d "${TMPDIR:-/tmp}/bn-prompt-journey.XXXXXX")
SERVER_PID=
cleanup() {
  if [ -n "$SERVER_PID" ]; then
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
  fi
  rm -rf "$TMP"
}
trap cleanup EXIT HUP INT TERM

export GIT_TERMINAL_PROMPT=0
export GIT_AUTHOR_NAME='Prompt Journey'
export GIT_AUTHOR_EMAIL='prompt-journey@example.invalid'
export GIT_COMMITTER_NAME="$GIT_AUTHOR_NAME"
export GIT_COMMITTER_EMAIL="$GIT_AUTHOR_EMAIL"
REMOTE="$TMP/hub.git"
COORD_HOME="$TMP/coordinator-home"
WORKER_HOME="$TMP/worker-home"
PROJECT=demo

git init --bare --initial-branch=main "$REMOTE" >/dev/null
coord() {
  env -u BEANS_HUB -u BN_CONFIG BEANS_HOME="$COORD_HOME" BEANS_PROJECT="$PROJECT" BN_ACTOR=coordinator "$BIN" "$@"
}
worker() {
  env -u BEANS_HUB -u BN_CONFIG BEANS_HOME="$WORKER_HOME" BEANS_PROJECT="$PROJECT" BN_ACTOR=worker "$BIN" "$@"
}
json_id() {
  python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])'
}

coord init "$REMOTE" >/dev/null
issue_json=$(coord --json create 'Disposable prompt issue' -d 'Durable execution context')
issue_id=$(printf '%s' "$issue_json" | json_id)
printf '## Objective\n\nReview the disposable journey.\n\n## Expected result\n\nRecord exact evidence.\n' >"$TMP/body.md"
prompt_json=$(coord --json prompt create 'Disposable two-actor review' --to worker --issue "$issue_id" --body-file "$TMP/body.md")
prompt_id=$(printf '%s' "$prompt_json" | json_id)

# A genuinely separate home and clone discovers the addressed prompt.
worker init "$REMOTE" >/dev/null
inbox_json=$(worker --json prompt inbox)
INBOX_JSON="$inbox_json" PROMPT_ID="$prompt_id" python3 - <<'PY'
import json, os
rows = json.loads(os.environ['INBOX_JSON'])
assert [row['id'] for row in rows] == [os.environ['PROMPT_ID']], rows
assert rows[0]['to'] == 'worker' and rows[0]['status'] == 'open'
assert set(rows[0]) == {'id','aliases','title','status','priority','labels','from','to','claimed_by','issue','created','updated','path','project'}
PY

claim_json=$(worker --json prompt update "$prompt_id" --claim)
CLAIM_JSON="$claim_json" PROMPT_ID="$prompt_id" python3 - <<'PY'
import json, os
value = json.loads(os.environ['CLAIM_JSON'])
assert set(value) == {'id','key','sha','commit','pushed','message'}, value
assert value['id'] == value['key'] == os.environ['PROMPT_ID']
assert value['sha'] == value['commit'] and value['sha'] and value['pushed'] is True
PY
coord sync >/dev/null
claimed_json=$(coord --json prompt show "$prompt_id")
CLAIMED_JSON="$claimed_json" python3 - <<'PY'
import json, os
value = json.loads(os.environ['CLAIMED_JSON'])
assert value['status'] == 'in_progress' and value['claimed_by'] == 'worker'
PY

# A third identity cannot replace ownership and the failed attempt changes nothing.
worker_hub="$WORKER_HOME/hub"
before_head=$(git -C "$worker_hub" rev-parse HEAD)
before_blob=$(git -C "$worker_hub" hash-object -- "$(git -C "$worker_hub" ls-files -- "projects/$PROJECT/prompts/$prompt_id*.md")")
set +e
other_error=$(env -u BEANS_HUB -u BN_CONFIG BEANS_HOME="$WORKER_HOME" BEANS_PROJECT="$PROJECT" BN_ACTOR=other "$BIN" --json prompt update "$prompt_id" --claim 2>&1)
other_status=$?
set -e
[ "$other_status" -ne 0 ]
[ "$other_error" = 'bn: prompt cannot be claimed by this actor' ]
[ "$(git -C "$worker_hub" rev-parse HEAD)" = "$before_head" ]
after_blob=$(git -C "$worker_hub" hash-object -- "$(git -C "$worker_hub" ls-files -- "projects/$PROJECT/prompts/$prompt_id*.md")")
[ "$after_blob" = "$before_blob" ]

worker --json prompt update "$prompt_id" --status completed --note 'disposable journey verified' >/dev/null
[ "$(worker --json prompt inbox)" = '[]' ]
terminal_json=$(worker --json prompt inbox --terminal)
TERMINAL_JSON="$terminal_json" PROMPT_ID="$prompt_id" python3 - <<'PY'
import json, os
rows = json.loads(os.environ['TERMINAL_JSON'])
row = next(row for row in rows if row['id'] == os.environ['PROMPT_ID'])
assert row['status'] == 'completed' and row['claimed_by'] == 'worker'
PY
coord sync >/dev/null

coord_hub="$COORD_HOME/hub"
prompt_path=$(git -C "$coord_hub" ls-files -- "projects/$PROJECT/prompts/$prompt_id*.md")
[ "$(printf '%s\n' "$prompt_path" | wc -l)" -eq 1 ]
PROMPT_FILE="$coord_hub/$prompt_path" PROMPT_ID="$prompt_id" ISSUE_ID="$issue_id" python3 - <<'PY'
import os
from pathlib import Path
raw = Path(os.environ['PROMPT_FILE']).read_text()
assert f"id: {os.environ['PROMPT_ID']}\n" in raw
assert 'from: coordinator\n' in raw and 'to: worker\n' in raw
assert 'status: completed\n' in raw and 'claimed_by: worker\n' in raw
assert f'issue: "[[{os.environ["ISSUE_ID"]}]]"\n' in raw
assert 'Review the disposable journey.\n' in raw
assert raw.count(': prompt created') == 1
assert raw.count(': prompt claimed') == 1
assert raw.count(': disposable journey verified') == 1
PY
[ "$(git -C "$coord_hub" log --format=%s --grep="^bn: record $prompt_id$" | wc -l)" -eq 3 ]

# Exercise exact read projections, the derived issue relationship, and every
# mutation method's read-only guard before JSON decoding.
PORT=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')
env -u BEANS_HUB -u BN_CONFIG BEANS_HOME="$COORD_HOME" BEANS_PROJECT="$PROJECT" BN_ACTOR=coordinator \
  "$BIN" serve --host 127.0.0.1 --port "$PORT" >"$TMP/server.log" 2>&1 &
SERVER_PID=$!
ready=false
for _n in $(seq 1 100); do
  if curl -fsS "http://127.0.0.1:$PORT/api/health" >"$TMP/health.json" 2>/dev/null; then ready=true; break; fi
  sleep 0.05
done
[ "$ready" = true ] || { printf 'server did not become ready\n' >&2; python3 -c 'import sys; print(open(sys.argv[1]).read(), end="", file=sys.stderr)' "$TMP/server.log"; exit 1; }
curl -fsS "http://127.0.0.1:$PORT/api/projects/$PROJECT/prompts?status=completed" >"$TMP/prompts.json"
curl -fsS "http://127.0.0.1:$PORT/api/prompts/$prompt_id" >"$TMP/prompt.json"
curl -fsS "http://127.0.0.1:$PORT/api/issues/$issue_id" >"$TMP/issue.json"
PROMPTS="$TMP/prompts.json" PROMPT="$TMP/prompt.json" ISSUE="$TMP/issue.json" PROMPT_ID="$prompt_id" python3 - <<'PY'
import json, os
rows = json.load(open(os.environ['PROMPTS']))
summary = next(row for row in rows if row['id'] == os.environ['PROMPT_ID'])
assert set(summary) == {'id','title','status','priority','labels','from','to','claimed_by','created','updated','project','path','has_issue'}
detail = json.load(open(os.environ['PROMPT']))
assert detail['id'] == os.environ['PROMPT_ID'] and detail['status'] == 'completed'
issue = json.load(open(os.environ['ISSUE']))
assert [row['id'] for row in issue['prompts']] == [os.environ['PROMPT_ID']]
assert all(link.get('note_kind') != 'prompt' for link in issue['backlinks'])
PY
for method in POST PATCH DELETE; do
  for route in "/api/projects/$PROJECT/prompts" "/api/prompts/$prompt_id"; do
    code=$(curl -sS -o "$TMP/readonly.json" -w '%{http_code}' -X "$method" --data-binary '{' "http://127.0.0.1:$PORT$route")
    [ "$code" = 405 ]
    python3 -c 'import json,sys; v=json.load(open(sys.argv[1])); assert v == {"error":{"code":"method_not_allowed","message":"resource is read-only"}}' "$TMP/readonly.json"
  done
done
kill "$SERVER_PID"
wait "$SERVER_PID" 2>/dev/null || true
SERVER_PID=

coord sync >/dev/null
worker sync >/dev/null
remote_head=$(git --git-dir="$REMOTE" rev-parse main)
[ "$(git -C "$coord_hub" rev-parse HEAD)" = "$remote_head" ]
[ "$(git -C "$worker_hub" rev-parse HEAD)" = "$remote_head" ]

printf 'PASS disposable prompt journey\n'
printf '  issue=%s prompt=%s\n' "$issue_id" "$prompt_id"
printf '  inbox=open -> claimed_by=worker -> completed; other claim rejected byte-identically\n'
printf '  prompt_commits=3 clones_synced=%s\n' "$remote_head"
printf '  http=list/detail/issue-reverse read_only=POST,PATCH,DELETE:405\n'
