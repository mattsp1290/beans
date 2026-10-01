#!/usr/bin/env python3
"""Read every successful Rust graph-reference edit with immutable Go."""
import json
import os
import subprocess

from plan_ref_reference import ROOT, capture

work = ROOT / '.compat/plan-ref-cross-read'
work.mkdir(parents=True, exist_ok=True)
inputs = work / 'rust-plans.json'
subprocess.run(['cargo', 'test', '--locked', '--test', 'domain',
                'plan_reference::reference_edits_match_go_bytes_errors_and_failure_state',
                '--', '--exact'], cwd=ROOT,
               env=dict(os.environ, BN_PLAN_REF_RUST_OUTPUT=str(inputs)), check=True)
fixture = json.loads((ROOT / 'tests/contract/plan-ref.json').read_text())
expected = {r['name']: r['after'] for r in fixture['refs'] if not r['error']}
rows = json.loads(inputs.read_text())
assert {r['name'] for r in rows} == set(expected), 'Missing actual Rust edits'
result = capture(inputs)
(work / 'go-reads.json').write_text(json.dumps(result, indent=2, ensure_ascii=False) + '\n')
assert {r['name'] for r in result['reads']} == set(expected)
for row in result['reads']:
    assert not row['error'], (row['name'], row['error'])
    assert row['plan'] == expected[row['name']], ('Different Go model', row['name'])
print(f'Go accepts all {len(rows)} actual Rust reference edits with matching models')
