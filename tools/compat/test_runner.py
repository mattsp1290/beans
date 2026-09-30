"""Mutation checks proving the oracle rejects broken candidates."""
import json
import hashlib
import os
from pathlib import Path
import re
import sys
import tempfile
import unittest

from runner import CONTRACT, check, execute


class OracleMutationTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.binary = Path(os.environ.get("BN_REFERENCE_BINARY", ".compat/reference/bn-go")).resolve(strict=True)
        cls.corpus = json.loads((CONTRACT / "cli.json").read_text())

    def case(self, identity):
        return next(c for c in self.corpus["cases"] if c["id"] == identity)

    def candidate(self, mutation, case):
        with tempfile.TemporaryDirectory() as directory:
            script = Path(directory) / "bad-candidate"
            script.write_text(f"#!{sys.executable} -S\n" +
                              "import json, pathlib, subprocess, sys\n" +
                              f"p = subprocess.run([{str(self.binary)!r}, *sys.argv[1:]], capture_output=True)\n" +
                              "out, err, code = p.stdout, p.stderr, p.returncode\n" + mutation + "\n" +
                              "sys.stdout.buffer.write(out)\nsys.stderr.buffer.write(err)\nsys.exit(code)\n")
            script.chmod(0o755)
            return check(script, {"cases": [case]})

    def test_reference_replays(self):
        case = self.case("read:show-alpha-a1b2:json")
        self.assertEqual(execute(self.binary, case), case["expected"])

    def test_rejects_wrong_exit(self):
        self.assertEqual(self.candidate("code = 1", self.case("version")), ["version: exit"])

    def test_rejects_missing_json_field(self):
        mutation = "value = json.loads(out)\ndel value['labels']\nout = (json.dumps(value, indent=2) + '\\n').encode()"
        self.assertEqual(self.candidate(mutation, self.case("read:show-alpha-a1b2:json")),
                         ["read:show-alpha-a1b2:json: stdout_b64"])

    def test_rejects_hub_file_change(self):
        mutation = "import os\np = pathlib.Path(os.environ['BEANS_HUB']) / 'docs/Guide.md'\np.write_bytes(p.read_bytes() + b'changed\\n')"
        self.assertEqual(self.candidate(mutation, self.case("read:show-alpha-a1b2:json")),
                         ["read:show-alpha-a1b2:json: changed_files, git"])

    def test_rejects_whitespace_drift(self):
        self.assertEqual(self.candidate("out = out.rstrip() + b'  \\n'", self.case("version")),
                         ["version: stdout_b64"])

    def test_retained_commands_have_help_and_error_probes(self):
        census = json.loads((CONTRACT / "commands.json").read_text())
        ids = {c["id"] for c in self.corpus["cases"]}
        for cmd in census["commands"]:
            if not cmd["retired"]:
                name = "-".join(cmd["path"]) or "root"
                for variant in ("help", "no-args", "unknown-flag", "trailing-one", "trailing-two"):
                    self.assertIn(name + ":" + variant, ids)

    def test_exit_code_corpus_covers_all_documented_codes(self):
        self.assertEqual({c["expected"]["exit"] for c in self.corpus["cases"]}, {0, 1, 2, 3, 4})

    def test_http_corpus_covers_registered_routes(self):
        routes = json.loads((CONTRACT / "routes.json").read_text())["entries"]
        corpus = json.loads((CONTRACT / "http.json").read_text())["cases"]
        for route in routes:
            expression = re.escape(route["path"])
            expression = re.sub(r":\w+", "[^/]+", expression).replace(r"\*", ".+")
            matches = [c for c in corpus if c["method"] == route["method"] and
                       re.fullmatch(expression, c["path"].split("?", 1)[0])]
            self.assertTrue(matches, route)

    def test_storage_fixture_bytes_match_baseline_inventory(self):
        manifest = json.loads((CONTRACT / "fixtures.json").read_text())
        for entry in manifest["entries"]:
            data = (CONTRACT.parents[1] / entry["destination"]).read_bytes()
            self.assertEqual(hashlib.sha256(data).hexdigest(), entry["sha256"], entry["source"])


if __name__ == "__main__":
    unittest.main()
