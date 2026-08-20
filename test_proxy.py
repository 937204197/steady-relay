import io
import json
import unittest
from email.message import Message
from unittest.mock import MagicMock, patch
from urllib.error import HTTPError

import proxy


class FakeResponse:
    def __init__(self, status=200, body=b'{"ok":true}', headers=None):
        self.status = status
        self._body = io.BytesIO(body)
        self.headers = headers or {"Content-Type": "application/json"}

    def __enter__(self): return self
    def __exit__(self, *args): return False
    def read(self, n=-1): return self._body.read(n)


class HelpersTest(unittest.TestCase):

    def test_429_is_retryable(self):
        self.assertIn(429, proxy.RETRY_STATUS)

    def test_upstream_url_does_not_duplicate_v1(self):
        old = proxy.UPSTREAM_BASE_URL
        try:
            proxy.UPSTREAM_BASE_URL = "https://example.test/v1"
            self.assertEqual(proxy._upstream_url("/v1/chat/completions"), "https://example.test/v1/chat/completions")
        finally:
            proxy.UPSTREAM_BASE_URL = old

    def test_safe_upstream_url_redacts_sensitive_components(self):
        value = "https://user:password@example.test:8443/v1?api_key=secret#fragment"
        self.assertEqual(proxy._safe_upstream_url(value), "https://example.test:8443/v1")

    def test_health_response_does_not_expose_upstream_url(self):
        handler = object.__new__(proxy.ProxyHandler)
        handler.path = "/healthz"
        handler._send_bytes = MagicMock()
        handler.do_GET()
        status, data, content_type = handler._send_bytes.call_args.args
        self.assertEqual(status, 200)
        self.assertEqual(content_type, "application/json")
        self.assertEqual(json.loads(data), {"status": "ok"})

    def test_retry_after_is_respected(self):
        self.assertEqual(proxy._retry_delay(0, "2"), 2.0)

    @patch("proxy.urlopen")
    def test_usage_limit_429_retries_then_succeeds(self, open_mock):
        headers = Message()
        headers["Content-Type"] = "application/json"
        error = HTTPError(
            "https://example.test/v1/responses", 429, "Too Many Requests",
            headers, io.BytesIO(b'{"error":{"type":"usage_limit_reached"}}'))
        open_mock.side_effect = [error, FakeResponse(body=b"ok-from-next-channel")]
        handler = object.__new__(proxy.ProxyHandler)
        handler.path = "/v1/responses"
        handler.command = "POST"
        handler.headers = {}
        handler.rfile = io.BytesIO()
        handler.wfile = io.BytesIO()
        handler.send_response = MagicMock()
        handler.send_header = MagicMock()
        handler.end_headers = MagicMock()
        old_retries = proxy.MAX_RETRIES
        old_backoff = proxy.BACKOFF_SECONDS
        old_upstream = proxy.UPSTREAM_BASE_URL
        proxy.MAX_RETRIES = 1
        proxy.BACKOFF_SECONDS = 0
        proxy.UPSTREAM_BASE_URL = "https://example.test/v1"
        try:
            handler._proxy()
        finally:
            proxy.MAX_RETRIES = old_retries
            proxy.BACKOFF_SECONDS = old_backoff
            proxy.UPSTREAM_BASE_URL = old_upstream
        self.assertEqual(open_mock.call_count, 2)
        self.assertEqual(handler.wfile.getvalue(), b"ok-from-next-channel")


if __name__ == "__main__":
    unittest.main()
