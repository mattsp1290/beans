"""Negative candidates must not disappear behind dynamic-field substitution."""
import base64
import json
import os
from pathlib import Path
import sys
import tempfile
import unittest

from journey import execute
from runner import CONTRACT


class JourneyMutationTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.binary = Path(os.environ.get("BN_REFERENCE_BINARY", ".compat/reference/bn-go")).resolve(strict=True)
        cls.corpus = json.loads((CONTRACT / "journey.json").read_text())

    def candidate(self, mutation, count):
        with tempfile.TemporaryDirectory() as directory:
            script = Path(directory) / "bad-candidate"
            script.write_text(f"#!{sys.executable} -S\n" +
                              "import json, os, pathlib, subprocess, sys\n" +
                              f"p = subprocess.run([{str(self.binary)!r}, *sys.argv[1:]], capture_output=True)\n" +
                              "out = p.stdout\n" + mutation + "\n" +
                              "sys.stdout.buffer.write(out)\nsys.stderr.buffer.write(p.stderr)\nsys.exit(p.returncode)\n")
            script.chmod(0o755)
            return execute(script, dict(self.corpus, steps=self.corpus["steps"][:count]))

    def test_rejects_invalid_generated_id(self):
        mutation = "if 'create' in sys.argv and 'Journey issue' in sys.argv:\n v = json.loads(out)\n v['id'] = 'alpha-ZZZZ'\n out = json.dumps(v).encode()"
        with self.assertRaisesRegex(ValueError, "ID namespace/alphabet/length"):
            self.candidate(mutation, 3)

    def test_rejects_generated_id_colliding_with_existing_issue(self):
        mutation = "if 'create' in sys.argv and 'Journey issue' in sys.argv:\n v = json.loads(out)\n v['id'] = 'alpha-a1b2'\n out = json.dumps(v).encode()"
        with self.assertRaisesRegex(ValueError, "not bijective"):
            self.candidate(mutation, 3)

    def test_rejects_dto_revision_different_from_file(self):
        mutation = "if 'show' in sys.argv:\n v = json.loads(out)\n v['updated'] = '2026-01-01T00:00:00Z'\n out = json.dumps(v).encode()"
        with self.assertRaisesRegex(ValueError, "persisted artifact revision"):
            self.candidate(mutation, 4)

    def test_preserves_user_timestamp_prose_and_detects_corruption(self):
        mutation = "if 'create' in sys.argv and 'Journey issue' in sys.argv:\n v = json.loads(out)\n path = pathlib.Path(os.environ['BEANS_HUB']) / v['path']\n path.write_bytes(path.read_bytes() + b'User prose 2024-01-01T00:00:00Z\\n')"
        results = self.candidate(mutation, 3)
        expected = self.corpus["steps"][2]["expected"]
        self.assertNotEqual(results[2]["files"], expected["files"])
        self.assertNotEqual(results[2]["git"]["status"], expected["git"]["status"])
        changed = [base64.b64decode(value) for value in results[2]["files"].values()]
        self.assertTrue(any(b"User prose 2024-01-01T00:00:00Z" in value for value in changed))

    def test_rejects_nonce_shared_by_distinct_commits(self):
        mutation = "if 'create' in sys.argv or 'update' in sys.argv:\n hub = os.environ['BEANS_HUB']\n msg = subprocess.run(['git', '-C', hub, 'log', '-1', '--format=%B'], check=True, capture_output=True).stdout.decode()\n import re\n msg = re.sub(r'(?m)^Bn-Run: .+$', 'Bn-Run: 0123456789abcdef', msg)\n subprocess.run(['git', '-C', hub, 'commit', '--amend', '-m', msg], check=True, capture_output=True)"
        with self.assertRaisesRegex(ValueError, "reused one nonce"):
            self.candidate(mutation, 5)


if __name__ == "__main__":
    unittest.main()
