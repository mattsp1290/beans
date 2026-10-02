#!/usr/bin/env python3
"""Read actual Rust bundle snapshots with the immutable Go validator."""
import json
import os
import subprocess

from plan_bundle_reference import ROOT, capture

work = ROOT / '.compat/plan-bundle-cross-read'
work.mkdir(parents=True, exist_ok=True)
inputs = work / 'rust-bundles.json'
subprocess.run(['cargo', 'test', '--locked', '--test', 'domain',
                'plan_bundle::snapshot_loading_matches_go_boundaries_paths_and_order',
                '--', '--exact'], cwd=ROOT,
               env=dict(os.environ, BN_PLAN_BUNDLE_RUST_OUTPUT=str(inputs)), check=True)
fixture = json.loads((ROOT / 'tests/contract/plan-bundle.json').read_text())
expected = {r['name']: r for r in fixture['loads'] if not r['error']}
rows = json.loads(inputs.read_text())
assert {r['name'] for r in rows} == set(expected), 'Missing actual Rust snapshots'
result = capture(inputs)
(work / 'go-reads.json').write_text(json.dumps(result, indent=2, ensure_ascii=False) + '\n')
assert {r['name'] for r in result['reads']} == set(expected)
accepted = 0
for row in result['reads']:
    original = expected[row['name']]
    assert row['error'] == original['read_error'], (row['name'], row['error'], original['read_error'])
    assert row['bundle'] == original['read_bundle'], ('Different Go bundle', row['name'])
    accepted += not row['error']
print(f'Go accepts {accepted} actual Rust bundle snapshots; {len(rows)-accepted} matching rejections')
