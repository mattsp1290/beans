#!/usr/bin/env python3
"""Read actual Rust plan encoder outputs with the immutable Go parser."""
import json
import os
from pathlib import Path
import subprocess

from plan_encode_reference import ROOT, capture

work = ROOT / '.compat/plan-encode-cross-read'
work.mkdir(parents=True, exist_ok=True)
inputs = work / 'rust-plans.json'
subprocess.run(['cargo', 'test', '--locked', '--test', 'domain',
                'plan_encode::plan_encoding_matches_fixed_go_bytes_and_errors', '--', '--exact'],
               cwd=ROOT, env=dict(os.environ, BN_PLAN_ENCODE_RUST_OUTPUT=str(inputs)), check=True)
rows = json.loads(inputs.read_text())
fixture = json.loads((ROOT / 'tests/contract/plan-encode.json').read_text())
expected = {r['name']: r for r in fixture['encodes'] if not r['error']}
assert {r['name'] for r in rows} == set(expected), 'Missing actual Rust outputs'
result = capture(inputs)
(work / 'go-reads.json').write_text(json.dumps(result, indent=2, ensure_ascii=False) + '\n')
accepted = 0
unchanged = 0
assert {r["name"] for r in result["reads"]} == set(expected)
for row in result['reads']:
    original = expected[row['name']]
    assert row['read_error'] == original['read_error'], (row['name'], row['read_error'], original['read_error'])
    if not row['read_error']:
        assert row['read_output'] == original['read_output'], ('Different Go read-back', row['name'])
        unchanged += row['unchanged']
        accepted += 1
print(f'Go accepts {accepted} actual Rust plans ({unchanged} unchanged on Go re-encode); {len(rows)-accepted} matching read rejections')
