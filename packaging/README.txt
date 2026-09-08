Steady Relay - standalone local retry proxy
============================================

This package is self-contained. Python and Go are not required.
This is an unofficial project and is not affiliated with or endorsed by OpenAI.

Before starting, choose a trusted OpenAI-compatible upstream API. This proxy
forwards your API key and request content to that upstream. No upstream is
configured by default.

What problem does this solve?
-----------------------------

Third-party model platforms may return model_capacity, server_is_overloaded,
server_unavailable, usage_limit_reached, HTTP 429, or HTTP 503 when a channel,
account, or model has no available capacity. Some clients treat that response
as a final task failure instead of trying the request again, which can stop a
Codex task mid-run.

Steady Relay retries these temporary failures before sending an error to the
client, as long as the upstream has not sent actual text, output items, or
tool calls. Some newer upstreams send a keepalive/heartbeat SSE event before
reporting capacity; those heartbeat events are not treated as model output and
remain eligible for a safe retry. The default is up to 10 retries after the
initial request (11 attempts total), with exponential backoff. If output has
already started, the request is not replayed, preventing duplicate text or
tool calls.

Optional enhanced buffering mode
---------------------------------

If the upstream sometimes sends output before reporting
server_is_overloaded, start the Go program with:
  ./start.sh --upstream https://api.example.com/v1 --buffer-until-success
On Windows, use:
  start.bat --buffer-until-success
or set BUFFER_UNTIL_SUCCESS=true before starting. In this opt-in mode, SSE
frames stay in local memory and are sent to Codex only after
response.completed. A response.failed event, connection failure, or incomplete
stream discards that attempt and retries it, preventing partial text or tool
calls from leaking to Codex. The default is off because it delays the first
visible output. Each attempt is capped at 64 MiB; if retries are exhausted
without response.completed, the proxy does not release partial output. A final
response.failed/error event is forwarded without the earlier partial frames;
connection failures and buffer-limit failures return an upstream-unavailable
error. This option is supported by the Go standalone program, not the Python
fallback.

Quick start
-----------

The archive is standalone: Python, Go, and developer tools are not required.
Extract the whole archive to a normal folder before starting. Do not run files
from inside the ZIP preview window.

Windows (easiest method):
  1. Open the extracted folder and double-click start.bat.
  2. Paste your trusted upstream API Base URL when the window asks for it.
  3. Keep the black window open while using Codex.

For an older package that does not ask for the URL, click the folder address
bar, type cmd, press Enter, then run:
  set UPSTREAM_BASE_URL=https://api.example.com/v1
  start.bat

If SmartScreen appears, verify the archive SHA-256 first, then choose
More info -> Run anyway. Do not disable Defender or SmartScreen.

macOS (easiest method):
  1. Control-click install-macos.command and choose Open.
  2. Confirm Open in the macOS warning dialog.
  3. Paste your trusted upstream API Base URL when prompted.
  4. Keep the terminal window open while using Codex.

Linux:
  1. Open a terminal in the extracted folder (usually right-click -> Open in Terminal).
  2. Run:
       ./start.sh --upstream https://api.example.com/v1

The upstream URL usually ends in /v1. Your API key remains in Codex; do not
paste it into this guide or into a public script.

macOS first run:
  After verifying the archive, Control-click install-macos.command and choose
  Open. It removes quarantine from this extracted package only. If no upstream
  is configured, it prompts for an API Base URL for this launch and does not
  save it.

Or pass the setting directly:
  ./start.sh --upstream https://api.example.com/v1
  start.bat --upstream https://api.example.com/v1

Set this local URL as the client's API Base URL:
  http://127.0.0.1:8080/v1

If port 8080 is occupied, the program automatically tries the next available
port and prints the selected address. Use that actual port in the client.
The /v1 suffix is required: omitting it causes /responses requests to be
rejected with HTTP 404.

CC Switch example (graphical configuration):
  1. Start Steady Relay, enter the real third-party upstream URL when prompted,
     and note the local API URL printed in its log. The default is
     http://127.0.0.1:8080/v1; use the actual port if it changed.
  2. In CC Switch, open the provider used by Codex and edit the provider.
  3. In the field labelled “API request address” (the red-boxed field in the
     example screenshot), enter the local URL from step 1, keep the /v1 suffix,
     then click Apply or Save.
  4. Fully quit and restart Codex. Keep the Steady Relay window open while
     using Codex.

  This field must contain the local Steady Relay URL, not the remote upstream
  URL; entering the remote URL here bypasses the retry proxy. Configure the
  remote upstream when starting Steady Relay with UPSTREAM_BASE_URL. A
  “[request] ... forwarding” log line confirms that a request entered the
  proxy. Keep the API key in CC Switch/Codex and never put it in a public script.

Keep the terminal window open while using the proxy. Press Ctrl+C to stop.
Health check: http://127.0.0.1:8080/healthz, or the selected port in the log.

Configuration
-------------

Command-line flags take precedence over environment variables:

  --upstream URL          required upstream OpenAI-compatible API Base URL
  --upstream-ip IP        optional fixed IP for that upstream host; omit for DNS
  --listen ADDRESS        local listen address, for example 127.0.0.1:8080
  --max-retries NUMBER    retries after the initial attempt (default 10)
  --retry-backoff TIME    base exponential backoff, for example 500ms
  --request-timeout TIME  upstream response-header timeout, for example 120s
  --max-retry-after TIME  maximum accepted Retry-After delay (default 60s)
  --buffer-until-success  hold SSE output until response.completed (default off)

Supported environment variables:
  UPSTREAM_BASE_URL, UPSTREAM_IP, LISTEN_ADDR, LISTEN_HOST, LISTEN_PORT,
  MAX_RETRIES, RETRY_BACKOFF, REQUEST_TIMEOUT, MAX_RETRY_AFTER
  BUFFER_UNTIL_SUCCESS

The default is 10 retries after the initial attempt (11 attempts total). All
429 responses, including usage_limit_reached, are retried with Retry-After or
exponential backoff. After retries are exhausted, the last upstream HTTP error
is forwarded unchanged.

Security and diagnostics
------------------------

Only listen on 127.0.0.1 unless you intentionally want other computers to
access this proxy; it has no local client authentication. The proxy preserves
request/response bodies and authentication headers for the configured upstream.
Normal request/response bodies and API keys are not logged. Logs include the
path, top-level model, retry state, timing, and truncated upstream error details.

For an optional IP pin, HTTPS still uses the original URL, Host, SNI and
certificate validation. If HTTPS_PROXY is configured, a corporate proxy may
resolve the hostname instead; follow your organisation's policy.

Verify before running
---------------------

This release may be unsigned. Verify the archive SHA-256 from the matching
GitHub Release before bypassing macOS Gatekeeper or Windows SmartScreen. See
TRUST-GUIDE.zh-CN.txt and README.md for detailed Chinese instructions.
