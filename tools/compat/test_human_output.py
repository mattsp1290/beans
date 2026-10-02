"""Human-output identity binding must not conceal user bytes or invalid clocks."""
import base64
from datetime import datetime, timezone
from pathlib import Path
import tempfile
import unittest

from journey import Identities


class HumanOutputChecks(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        (self.root / "hub").mkdir()
        self.identities = Identities(self.root)
        self.identities.commits["1234567" + "a" * 33] = "${COMMIT_0}"
        self.stamp = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
        self.identities.end = datetime.now(timezone.utc)

    def test_user_title_commit_prefix_stays_literal(self):
        data = b"alpha-r-aaaa A user title (1234567)\n\nUser prose 1234567\n"
        actual = self.identities.human_output(data, {"argv": ["request", "show", "alpha-r-aaaa"]}, self.identities.start)
        self.assertEqual(base64.b64decode(actual), data)

    def test_only_persisted_log_dates_are_bound(self):
        (self.root / "hub/issue.md").write_text(f"---\nid: alpha-aaaa\n---\nUser date {self.stamp}\n\n## Log\n- {self.stamp} contract: created\n")
        display = datetime.fromisoformat(self.stamp).strftime("%Y-%m-%d %H:%M")
        data = f"alpha-aaaa Title\n\nUser date {self.stamp}\n\nlog:\n  {display} contract: created\n".encode()
        actual = base64.b64decode(self.identities.human_output(data, {"argv": ["show", "alpha-aaaa"]}, self.identities.start))
        self.assertIn(self.stamp.encode(), actual)
        self.assertIn(b"  ${NOW_MINUTE} contract: created\n", actual)
        with self.assertRaisesRegex(ValueError, "persisted event/date"):
            self.identities.human_output(data.replace(display.encode(), b"1999-01-01 00:00"),
                                         {"argv": ["show", "alpha-aaaa"]}, self.identities.start)

    def test_unknown_commit_prefix_is_rejected(self):
        with self.assertRaisesRegex(ValueError, "one observed commit"):
            self.identities.human_output(b"updated alpha-aaaa (abcdef0)\n", {"argv": ["update", "alpha-aaaa"]}, self.identities.start)

    def test_ambiguous_commit_prefix_is_rejected(self):
        self.identities.commits["1234567" + "b" * 33] = "${COMMIT_1}"
        with self.assertRaisesRegex(ValueError, "one observed commit"):
            self.identities.human_output(b"updated alpha-aaaa (1234567)\n", {"argv": ["update", "alpha-aaaa"]}, self.identities.start)

    def test_fetch_age_must_match_persisted_time(self):
        (self.root / "beans/cache").mkdir(parents=True)
        (self.root / "beans/cache/last-fetch").write_text(self.stamp + "\n")
        with self.assertRaisesRegex(ValueError, "persisted time/process interval"):
            self.identities.human_output(b"last fetch: 100s ago\n", {"argv": ["status"]}, self.identities.start)
