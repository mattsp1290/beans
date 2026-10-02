"""Check that asset comparisons reject changed payloads and framing."""
import hashlib
import unittest

from assets import summarize
from runner import encode


class AssetChecks(unittest.TestCase):
    def test_rejects_changed_embedded_bytes(self):
        case = {"method": "GET", "path": "/assets/app.js"}
        manifest = {"assets/app.js": hashlib.sha256(b"application").hexdigest()}
        result = {"status": 200, "headers": [["content-length", "11"]],
                  "body_b64": encode(b"Application")}
        with self.assertRaisesRegex(ValueError, "differs from pinned"):
            summarize(case, result, manifest)

    def test_rejects_wrong_asset_length(self):
        with self.assertRaisesRegex(ValueError, "Content-Length"):
            summarize({"method": "GET", "path": "/test"},
                      {"status": 200, "headers": [["content-length", "1"]],
                       "body_b64": encode(b"bytes")}, {})

    def test_rejects_asset_head_body(self):
        with self.assertRaisesRegex(ValueError, "HEAD response"):
            summarize({"method": "HEAD", "path": "/test"},
                      {"status": 200, "headers": [], "body_b64": encode(b"bytes")}, {})
