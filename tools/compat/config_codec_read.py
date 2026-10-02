#!/usr/bin/env python3
"""Compare actual Rust config output using the fixed Go file loaders."""
import json
import os
import subprocess

from config_codec_reference import ROOT, capture

work = ROOT / '.compat/config-codec-cross-read'
work.mkdir(parents=True, exist_ok=True)
inputs = work / 'rust-configs.json'
subprocess.run(['cargo', 'test', '--locked', '--test', 'domain',
                'config_codec::configuration_output_matches_go_bytes', '--', '--exact'],
               cwd=ROOT, env=dict(os.environ, BN_CONFIG_CODEC_RUST_OUTPUT=str(inputs)), check=True)
fixture = json.loads((ROOT / 'tests/contract/config-codec.json').read_text())
expected = {r['name']: r for r in fixture['encodes']}
rows = json.loads(inputs.read_text())
assert len(rows) == len(expected)
assert {r['name'] for r in rows} == set(expected), 'Missing actual Rust encodes'
result = capture(inputs)
(work / 'go-reads.json').write_text(json.dumps(result, indent=2, ensure_ascii=False) + '\n')
assert len(result['cross_reads']) == len(expected)
assert {r['name'] for r in result['cross_reads']} == set(expected)
for row in result['cross_reads']:
    original = expected[row['name']]
    assert row['error'] == original['read_error'], ('Different Go error', row['name'], row['error'])
    assert row['config'] == original['read_config'], ('Different Go model', row['name'])
accepted = sum(not r['error'] for r in result['cross_reads'])
print(f'Go reads all {len(rows)} actual Rust configs identically: {accepted} accepted, '
      f'{len(rows) - accepted} matching rejections')
