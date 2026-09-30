"""Prove that HTTP normalizers retain ordering, framing and user content checks."""
from datetime import datetime, timezone
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import Mock, patch

from http_journey import html_output, normalized_response
from journey import Identities
from runner import encode


class HTTPJourneyChecks(unittest.TestCase):
    def test_only_owned_html_log_dates_vary(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "hub").mkdir()
            identities = Identities(root)
            stamp = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
            identities.end = datetime.now(timezone.utc)
            html = f'<p>User date {stamp}</p>\n<h2 id="log">Log</h2>\n<ul>\n<li>{stamp} contract: event</li>\n</ul>\n<h2 id="after">After</h2>\n<p>User date {stamp}</p>'
            data = json.dumps({"html": html}, separators=(",", ":")).encode()
            normalized = json.loads(html_output(identities, data))["html"]
            self.assertEqual(normalized.count(stamp), 2)
            self.assertIn("<li>${NOW_SECOND} contract: event", normalized)

    def test_rejects_incorrect_body_length(self):
        response = {"status": 200, "headers": [["content-length", "1"]], "body_b64": encode(b"{}")}
        with patch("http_journey.response", return_value=response):
            with self.assertRaisesRegex(ValueError, "actual body"):
                normalized_response(0, {"method": "GET", "path": "/api/test"}, Path("/tmp/test"), {}, Mock())

    def test_rejects_head_body(self):
        response = {"status": 200, "headers": [], "body_b64": encode(b"unexpected")}
        with patch("http_journey.response", return_value=response):
            with self.assertRaisesRegex(ValueError, "HEAD response contains"):
                normalized_response(0, {"method": "HEAD", "path": "/api/test"}, Path("/tmp/test"), {}, Mock())

    def test_rejects_reversed_graph_nodes(self):
        data = b'{"nodes":[{"id":"alpha-ffff"},{"id":"alpha-aaaa"}]}'
        response = {"status": 200, "headers": [["content-length", str(len(data))]], "body_b64": encode(data)}
        with patch("http_journey.response", return_value=response):
            with self.assertRaisesRegex(ValueError, "ordered by actual ID"):
                normalized_response(0, {"method": "GET", "path": "/api/graph"}, Path("/tmp/test"), {}, Mock())

    def test_success_journey_rejects_error_envelopes(self):
        with patch("http_journey.response", return_value={"status": 400}):
            with self.assertRaisesRegex(ValueError, "status 400"):
                normalized_response(0, {"method": "POST", "path": "/api/test"}, Path("/tmp/test"), {}, Mock())


if __name__ == "__main__":
    unittest.main()
