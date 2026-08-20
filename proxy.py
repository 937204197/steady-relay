#!/usr/bin/env python3
"""Small OpenAI-compatible HTTP reverse proxy with retries and SSE support.

Only Python's standard library is used. The proxy accepts requests on /v1/*
and forwards them to the required UPSTREAM_BASE_URL.
"""
from __future__ import annotations

import json
import errno
import logging
import os
import random
import ssl
import uuid
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Optional
from urllib.error import HTTPError, URLError
from urllib.parse import urlsplit, urlunsplit
from urllib.request import Request, urlopen


UPSTREAM_BASE_URL = os.getenv("UPSTREAM_BASE_URL", "").strip()
LISTEN_HOST = os.getenv("LISTEN_HOST", "127.0.0.1")
LISTEN_PORT = int(os.getenv("LISTEN_PORT", "8080"))
MAX_RETRIES = max(0, int(os.getenv("MAX_RETRIES", "10")))
BACKOFF_SECONDS = max(0.0, float(os.getenv("RETRY_BACKOFF", "0.5")))
REQUEST_TIMEOUT = max(1.0, float(os.getenv("REQUEST_TIMEOUT", "120")))
RETRY_STATUS = {408, 425, 429} | set(range(500, 600))
HOP_BY_HOP = {"connection", "keep-alive", "proxy-authenticate", "proxy-authorization",
              "te", "trailer", "transfer-encoding", "upgrade", "host"}


def _upstream_url(path: str) -> str:
    base = UPSTREAM_BASE_URL.rstrip("/")
    # Avoid /v1/v1 when clients send the conventional /v1 prefix.
    if base.endswith("/v1") and path == "/v1":
        return base
    if base.endswith("/v1") and path.startswith("/v1/"):
        path = path[3:]
    return base + (path if path.startswith("/") else "/" + path)


def _safe_upstream_url(value: str) -> str:
    """Return an upstream URL safe to include in logs."""
    try:
        parsed = urlsplit(value)
        host = parsed.hostname
        port = parsed.port
    except ValueError:
        return "<invalid upstream URL>"
    if not parsed.scheme or not host:
        return "<invalid upstream URL>"
    if ":" in host and not host.startswith("["):
        host = f"[{host}]"
    netloc = host if port is None else f"{host}:{port}"
    return urlunsplit((parsed.scheme, netloc, parsed.path, "", ""))


def _retry_delay(attempt: int, retry_after: Optional[str] = None) -> float:
    """Return a bounded delay. Called only after a retryable failure."""
    if retry_after:
        try:
            return min(60.0, max(0.0, float(retry_after)))
        except ValueError:
            pass
    ceiling = min(60.0, BACKOFF_SECONDS * (2 ** attempt))
    if not ceiling:
        return 0.0
    # Equal jitter keeps later retries in the upper half of the window.
    floor = ceiling / 2.0
    return floor + random.uniform(0, ceiling - floor)


def _requested_model(body: bytes) -> str:
    try:
        value = json.loads(body).get("model", "")
        return str(value)[:120] if value is not None else ""
    except Exception:
        return ""


class ProxyHandler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.0"  # close-delimited responses simplify SSE forwarding

    def send_response(self, code, message=None):
        """Avoid BaseHTTPRequestHandler's synthetic Server/Date headers."""
        self.log_request(code)
        self.send_response_only(code, message)

    def do_GET(self):
        if self.path in ("/healthz", "/readyz"):
            payload = {"status": "ok"}
            self._send_bytes(200, json.dumps(payload).encode(), "application/json")
            return
        self._proxy()

    def do_POST(self):
        self._proxy()

    def do_HEAD(self):
        self._proxy()

    def do_OPTIONS(self):
        self._proxy()

    def do_PUT(self):
        self._proxy()

    def do_PATCH(self):
        self._proxy()

    def do_DELETE(self):
        self._proxy()

    def _send_bytes(self, status: int, data: bytes, content_type: str = "application/octet-stream"):
        self.send_response(status)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(data)))
        self.send_header("Cache-Control", "no-cache")
        self.end_headers()
        self.wfile.write(data)

    def _proxy(self):
        request_id = "req-" + uuid.uuid4().hex[:8]
        started_at = time.monotonic()
        if not self.path.startswith("/v1"):
            logging.info("[request] id=%s %s %s rejected status=404", request_id, self.command, self.path)
            self._send_bytes(404, b'{"error":"path must start with /v1"}', "application/json")
            return
        length = int(self.headers.get("Content-Length", "0") or 0)
        body = self.rfile.read(length) if length else b""
        requested_model = _requested_model(body)
        has_content_length = "Content-Length" in self.headers
        url = _upstream_url(self.path)
        logging.info("[request] id=%s method=%s path=%s model=%r forwarding", request_id, self.command, self.path, requested_model)
        request_headers = {
            k: v for k, v in self.headers.items()
            if k.lower() not in HOP_BY_HOP
        }
        last_error: Optional[str] = None
        downstream_started = False
        for attempt in range(MAX_RETRIES + 1):
            # Reuse the exact bytes for every attempt. Preserve body absence
            # for requests that did not carry Content-Length at all.
            req = Request(url, data=body if has_content_length else None,
                          headers=request_headers, method=self.command)
            try:
                with urlopen(req, timeout=REQUEST_TIMEOUT, context=ssl.create_default_context()) as resp:
                    status = getattr(resp, "status", 200)
                    content_type = (resp.headers.get("Content-Type") or "").lower()
                    is_sse = "text/event-stream" in content_type
                    # Buffer ordinary responses before sending their headers,
                    # so a connection drop during a JSON response can still
                    # be retried without exposing a partial HTTP response.
                    first_chunk = None
                    if is_sse and self.command != "HEAD":
                        # Do not commit response headers until the upstream
                        # has produced its first byte; a pre-first-byte drop
                        # remains safely retryable.
                        first_chunk = resp.read(64 * 1024)
                        if not first_chunk and attempt < MAX_RETRIES:
                            last_error = "upstream closed before stream data"
                            delay = _retry_delay(attempt)
                            logging.info("[retry] %s %s attempt=%d/%d reason=%s wait=%.3fs", self.command, self.path, attempt + 1, MAX_RETRIES, last_error, delay)
                            time.sleep(delay)
                            continue
                    data = None if is_sse or self.command == "HEAD" else resp.read()
                    downstream_started = True
                    self.send_response(status)
                    for key, value in resp.headers.items():
                        if key.lower() not in HOP_BY_HOP:
                            self.send_header(key, value)
                    self.end_headers()
                    if data is not None and self.command != "HEAD":
                        self.wfile.write(data)
                    elif is_sse and self.command != "HEAD":
                        if first_chunk:
                            self.wfile.write(first_chunk)
                            self.wfile.flush()
                        while True:
                            chunk = resp.read(64 * 1024)
                            if not chunk:
                                break
                            self.wfile.write(chunk)
                            self.wfile.flush()
                    logging.info("[response] id=%s %s %s status=%d attempts=%d duration=%.3fs", request_id, self.command, self.path, status, attempt + 1, time.monotonic() - started_at)
                    return
            except HTTPError as exc:
                if downstream_started:
                    logging.warning("upstream stream terminated with HTTP %s after response started", exc.code)
                    return
                status = exc.code
                try:
                    error_body = exc.read()
                except Exception:
                    error_body = b""
                if status in RETRY_STATUS and attempt < MAX_RETRIES:
                    last_error = f"upstream HTTP {status}"
                    delay = _retry_delay(attempt, exc.headers.get("Retry-After") if exc.headers else None)
                    logging.info("[retry] %s %s attempt=%d/%d upstream_status=%d wait=%.3fs", self.command, self.path, attempt + 1, MAX_RETRIES, status, delay)
                    time.sleep(delay)
                    continue
                self.send_response(status)
                if exc.headers:
                    for key, value in exc.headers.items():
                        if key.lower() not in HOP_BY_HOP:
                            self.send_header(key, value)
                if not exc.headers or "Content-Length" not in exc.headers:
                    self.send_header("Content-Length", str(len(error_body)))
                self.end_headers()
                if self.command != "HEAD":
                    self.wfile.write(error_body)
                logging.info("[response] %s %s status=%d attempts=%d duration=%.3fs", self.command, self.path, status, attempt + 1, time.monotonic() - started_at)
                return
            except (URLError, TimeoutError, OSError) as exc:
                if downstream_started:
                    logging.warning("upstream stream interrupted after response started: %s", exc)
                    return
                last_error = str(exc)
                if attempt < MAX_RETRIES:
                    delay = _retry_delay(attempt)
                    logging.info("[retry] %s %s attempt=%d/%d reason=%s wait=%.3fs", self.command, self.path, attempt + 1, MAX_RETRIES, last_error, delay)
                    time.sleep(delay)
                    continue
                break
        message = {"error": "upstream unavailable", "detail": last_error or "request failed"}
        self._send_bytes(502, json.dumps(message).encode(), "application/json")
        logging.info("[response] %s %s status=502 attempts=%d duration=%.3fs", self.command, self.path, MAX_RETRIES + 1, time.monotonic() - started_at)

    def log_message(self, fmt, *args):
        logging.info("%s - %s", self.address_string(), fmt % args)


def main() -> None:
    logging.basicConfig(level=os.getenv("LOG_LEVEL", "INFO"), format="%(asctime)s %(levelname)s %(message)s")
    if not UPSTREAM_BASE_URL:
        raise SystemExit("UPSTREAM_BASE_URL is required, for example https://api.example.com/v1")
    server = None
    for candidate in range(LISTEN_PORT, 65536):
        try:
            server = ThreadingHTTPServer((LISTEN_HOST, candidate), ProxyHandler)
            break
        except OSError as exc:
            if exc.errno != errno.EADDRINUSE or LISTEN_PORT == 0:
                raise
    if server is None:
        raise OSError(f"no available TCP port from {LISTEN_HOST}:{LISTEN_PORT} through 65535")
    actual_port = int(server.server_port)
    if actual_port != LISTEN_PORT:
        logging.info("listen address %s:%d is occupied; using %s:%d", LISTEN_HOST, LISTEN_PORT, LISTEN_HOST, actual_port)
    logging.info("proxy listening on http://%s:%d -> %s", LISTEN_HOST, actual_port, _safe_upstream_url(UPSTREAM_BASE_URL))
    logging.info("[startup] local_api_base_url=http://%s:%d/v1 health=http://%s:%d/healthz", LISTEN_HOST, actual_port, LISTEN_HOST, actual_port)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()


if __name__ == "__main__":
    main()
