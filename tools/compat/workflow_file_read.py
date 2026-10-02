#!/usr/bin/env python3
"""Read actual Rust project output with the fixed Go strict workflow decoder."""
import json
import os
import subprocess
from workflow_file_reference import ROOT, capture

work = ROOT / '.compat/workflow-file-cross-read'
work.mkdir(parents=True, exist_ok=True)
inputs = work / 'rust-configs.json'
subprocess.run(['cargo', 'test', '--locked', '--test', 'domain',
                'config_codec::configuration_output_matches_go_bytes', '--', '--exact'],
               cwd=ROOT, env=dict(os.environ, BN_CONFIG_CODEC_RUST_OUTPUT=str(inputs)), check=True)
fixture = json.loads((ROOT / 'tests/contract/workflow-file.json').read_text())
expected = {r['name']: r for r in fixture['decodes'] if r['name'].startswith('project-writer-')}
rust = [r for r in json.loads(inputs.read_text()) if r['kind'] == 'project']
assert len(rust) == len(expected)
assert {'project-writer-' + r['name'] for r in rust} == set(expected)
result = capture(inputs)
(work / 'go-reads.json').write_text(json.dumps(result, indent=2, ensure_ascii=False) + '\n')
assert len(result['reads']) == len(expected)
assert {r['name'] for r in result['reads']} == set(expected)
for row in result['reads']:
    original = expected[row['name']]
    assert row['error'] == original['error'], (row['name'], row['error'])
    assert row['file_config'] == original['file_config'], ('Different Go model', row['name'])
accepted = sum(not r['error'] for r in result['reads'])
print(f'Go strictly reads all {len(rust)} actual Rust project configs identically: '
      f'{accepted} accepted, {len(rust) - accepted} matching rejections')
