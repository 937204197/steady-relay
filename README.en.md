# Steady Relay

[中文](README.md) · English

A local OpenAI-compatible API retry proxy. It accepts requests sent to local `/v1/*` endpoints and transparently forwards them to an upstream service that you explicitly configure and trust. It retries connection errors, timeouts, and temporary `408/425/429/5xx` failures with backoff.

> This is an unofficial project. It is not affiliated with or endorsed by OpenAI.

## What problem does it solve?

Third-party model platforms may return errors such as `model_capacity`, `server_is_overloaded`, `server_unavailable`, or `usage_limit_reached` when channels are busy, an account has exhausted its quota, or service capacity is unavailable. Some clients treat these errors as a final task failure instead of starting another request, which can interrupt a Codex task.

Steady Relay sits between the client and the model platform. It accepts the client request and transparently forwards it to the upstream you configure. If the upstream reports one of these temporary failures before any actual text, output item, or tool-call data has been sent, the proxy retries automatically—up to 10 retries by default (11 upstream attempts including the first request)—before exposing the error to the client. An upstream may send `keepalive`/heartbeat SSE events before reporting a capacity error; those heartbeats are not treated as actual output, so a safe retry can still occur. Therefore, if you search for or encounter `model_capacity`, `server_is_overloaded`, `server_unavailable`, `usage_limit_reached`, `429`, or `503`, you can place Steady Relay in front of the client and point the client's API Base URL at the local proxy.

The proxy does not rewrite request or response bodies. Once the upstream has sent actual output to the client, the proxy will not replay that request, avoiding duplicate text, tool calls, or tool execution.

## Fastest start: let Codex install it for you (recommended)

This repository includes a `steady-relay-setup` Skill for non-developers. After installing it, send the following message to Codex. It can help select a release, download and verify the package, extract it, generate the launch command, and check health; you do not need to understand `cd` or construct download URLs manually:

```text
Please install the Codex Skill from this repository:
https://github.com/937204197/steady-relay/tree/main/skills/steady-relay-setup
```

After installation, if the Skill does not appear immediately, reopen Codex. Then send:

```text
Please use $steady-relay-setup to download and start Steady Relay from this address:
https://github.com/937204197/steady-relay
```

If you have already installed the Skill, you can send the second message directly.

Before downloading, the wizard displays your operating system, processor, release asset, and target directory, and then verifies the SHA-256 checksum after confirmation. It asks for an upstream API Base URL that you trust, checks local `/healthz`, and guides you through configuring Codex/CC Switch. It does not request or record your API Key and does not modify the CC Switch database or Codex configuration by default.

Steady Relay must remain running while you use Codex. After a successful launch, the terminal continuously prints request logs: `[request] ... forwarding` means a request entered the relay, and `[retry] ...` means an upstream retry is in progress. This is normal; do not close the terminal window. When you are finished, stop the relay first and change the Codex/CC Switch API request address back to the original remote address. Otherwise, the next Codex request will still point at the stopped local relay.

### Optional advanced mode: release SSE only after success

If the upstream often sends `response.output_text.delta` or tool-call fragments and only then reports `server_is_overloaded`, enable `--buffer-until-success` (or set `BUFFER_UNTIL_SUCCESS=true`). The proxy buffers the SSE response locally and sends nothing to Codex until it receives `response.completed`. If it receives `response.failed`, a connection interruption, or another incomplete terminal state, it discards the buffer and retries according to the retry policy. This prevents half of a failed attempt's text or tool calls from reaching Codex.

This mode is disabled by default because it increases time to first byte and memory use. Each attempt is limited to a 64 MiB buffer; exceeding the limit discards that attempt and triggers a retry. If retries are exhausted without `response.completed`, the proxy does not release incomplete output. If the final event is `response.failed`/`error`, only the final error event is released, not earlier partial output. If the connection is interrupted or the buffer limit is exceeded, the proxy returns an upstream-unavailable error. This option is available only in the standalone Go program; the Python fallback does not support it.

## Security boundaries and compatibility

- The project does not provide or select any third-party upstream by default. You must set `UPSTREAM_BASE_URL` yourself.
- API Keys, request bodies, and response contents are forwarded to that upstream. Configure only a service you are authorized to use and trust.
- The proxy listens only on `127.0.0.1` by default. Do not change it to `0.0.0.0`; the proxy has no authentication for remote clients.
- Request and response bodies are not rewritten. Only HTTP hop-by-hop headers, which must not be forwarded, are removed.
- Once SSE text, output items, or tool calls have been sent to the client, the proxy does not retry. Temporary failures before actual output can be retried safely.
- Pinning an IP is optional. HTTPS still uses the original hostname for Host, SNI, and certificate verification.

## Quick start (no development environment required)

Release packages are standalone programs. You do not need Python, Go, or other development tools. Download the archive for your computer from the Release section above and **extract it completely** into a normal folder; do not run the program from a ZIP preview window.

### Download v2.2.0

Choose the package for your operating system and processor. The link text uses a human-readable system name; the architecture in parentheses helps confirm that it matches your computer:

| System and processor | Download |
| --- | --- |
| macOS (Intel, x86_64) | [Download macOS Intel](https://github.com/937204197/steady-relay/releases/download/v2.2.0/steady-relay-2.2.0-darwin-amd64.tar.gz) |
| macOS (Apple Silicon, ARM64; M1/M2/M3, etc.) | [Download macOS Apple Silicon](https://github.com/937204197/steady-relay/releases/download/v2.2.0/steady-relay-2.2.0-darwin-arm64.tar.gz) |
| Windows (Intel/AMD 64-bit, x64) | [Download Windows x64](https://github.com/937204197/steady-relay/releases/download/v2.2.0/steady-relay-2.2.0-windows-amd64.zip) |
| Windows (ARM64) | [Download Windows ARM64](https://github.com/937204197/steady-relay/releases/download/v2.2.0/steady-relay-2.2.0-windows-arm64.zip) |
| Linux (Intel/AMD 64-bit, x86_64) | [Download Linux x86_64](https://github.com/937204197/steady-relay/releases/download/v2.2.0/steady-relay-2.2.0-linux-amd64.tar.gz) |
| Linux (ARM64, aarch64) | [Download Linux ARM64](https://github.com/937204197/steady-relay/releases/download/v2.2.0/steady-relay-2.2.0-linux-arm64.tar.gz) |

You can also download the [SHA256 checksum file](https://github.com/937204197/steady-relay/releases/download/v2.2.0/SHA256SUMS) to verify the archive. The complete file list and older versions are available in [GitHub Releases](https://github.com/937204197/steady-relay/releases).

### Simplest launch method

Prepare an OpenAI-compatible model API Base URL that you trust and are authorized to use, normally ending in `/v1`, for example `https://api.example.com/v1`. Codex continues to store and send the API Key; Steady Relay does not ask you to put the key in a script.

#### Windows

1. Open the extracted folder and confirm that it contains `start.bat` and `steady-relay.exe`.
2. Double-click `start.bat`. If no upstream address has been configured, the program prompts you to paste one and press Enter. If you downloaded an older package without this prompt, click the folder's address bar, type `cmd`, and press Enter. Then paste the following two lines; you do not need to run `cd` yourself:

   ```bat
   set UPSTREAM_BASE_URL=https://api.example.com/v1
   start.bat
   ```

3. If Windows SmartScreen appears, confirm that the file came from this project's GitHub Release, then choose **More info → Run anyway**.
4. Keep the black window open while Codex is in use. Configure Codex with the local address shown by the program.

#### macOS

1. Open the extracted folder and confirm that it contains `install-macos.command` and `steady-relay`.
2. Control-click `install-macos.command`, choose **Open**, and click **Open** in the confirmation dialog. This is the normal first-run warning for an unsigned program.
3. At the terminal prompt, paste your upstream API address (normally ending in `/v1`) and press Enter.
4. Keep the terminal window open.

If macOS does not show **Open**, right-click the file and choose **Open**. If it still cannot launch, open **System Settings → Privacy & Security**, click **Open Anyway** at the bottom, and repeat step 2.

#### Linux

1. Extract the archive completely.
2. In your file manager, right-click an empty area and choose **Open in Terminal** (the wording varies by distribution).
3. In the terminal that opens, paste the following line and press Enter:

   ```bash
   ./start.sh --upstream https://api.example.com/v1
   ```

If you see a permission error, run `chmod +x start.sh steady-relay`, then repeat the previous step.

### Configure Codex

After startup, the terminal prints the actual local port. Set Codex's API Base URL to:

```text
http://127.0.0.1:8080/v1
```

If port 8080 is occupied, the program automatically tries later ports such as 8081 and 8082. Use the actual port printed in the startup log, for example `http://127.0.0.1:8081/v1`. Do not remove the trailing `/v1`; otherwise Codex will send `/responses` to the wrong path and receive a 404.

#### Configure Codex with CC Switch (GUI example)

If you use CC Switch to manage Codex providers:

1. Start Steady Relay and enter the real third-party upstream API address when prompted. Note the local API address in the terminal log, normally `http://127.0.0.1:8080/v1`; if the port is occupied, use the actual port shown in the log.
2. Open CC Switch and edit the provider that Codex will use.
3. In the **API Request URL** field highlighted in the screenshot, enter the local address from step 1, then click **Apply** or **Save**. Keep the `/v1` suffix.
4. Fully quit and restart Codex so the new address takes effect. Keep Steady Relay running while Codex is in use.

The **API Request URL** in CC Switch must be the local Steady Relay address, not the remote third-party API address. Otherwise requests bypass the relay and cannot use automatic retries. Enter the remote third-party API address only when starting Steady Relay as `UPSTREAM_BASE_URL`. A `[request] ... forwarding` log line confirms that a request entered the relay. Keep the API Key in CC Switch/Codex's own configuration; never put it in a public script.

### Command-line launch (for users comfortable with a terminal)

First change to the directory containing the extracted files.

Windows Command Prompt:

```bat
set UPSTREAM_BASE_URL=https://api.example.com/v1
start.bat
```

macOS / Linux:

```bash
UPSTREAM_BASE_URL=https://api.example.com/v1 ./start.sh
```

You can also pass the value explicitly:

```bash
./start.sh --upstream https://api.example.com/v1
```

Health check:

```text
http://127.0.0.1:8080/healthz
```

## Configuration

Command-line arguments take precedence over environment variables.

| Argument | Environment variable | Default | Description |
| --- | --- | --- | --- |
| `--upstream URL` | `UPSTREAM_BASE_URL` | None; required | Upstream OpenAI-compatible API Base URL |
| `--upstream-ip IP` | `UPSTREAM_IP` | Empty | Optional pinned IP for the standalone Go program; system DNS is used when empty |
| `--listen ADDRESS` | `LISTEN_ADDR` | `127.0.0.1:8080` | Local listen address |
| `--max-retries N` | `MAX_RETRIES` | `10` | Maximum retries after the first request |
| `--retry-backoff TIME` | `RETRY_BACKOFF` | `500ms` | Exponential backoff base |
| `--request-timeout TIME` | `REQUEST_TIMEOUT` | `120s` | Maximum wait for upstream response headers |
| `--max-retry-after TIME` | `MAX_RETRY_AFTER` | `60s` | Maximum wait from an upstream `Retry-After` value |
| `--buffer-until-success` | `BUFFER_UNTIL_SUCCESS` | `false` | Buffer SSE until `response.completed` before releasing it |

By default, the proxy retries up to 10 times after the first request, for at most 11 upstream requests. Backoff uses jittered exponential waits; later retries use the latter part of each wait window, with a maximum of 60 seconds per wait. All `429` responses, including `usage_limit_reached`, are retryable so an upstream can switch among multiple accounts or channels. If every attempt fails, the final upstream HTTP error is forwarded to the client unchanged.

Enable the advanced mode:

```bash
./start.sh --upstream https://api.example.com/v1 --buffer-until-success
```

Or in Windows Command Prompt:

```bat
set BUFFER_UNTIL_SUCCESS=true
start.bat
```

### Optional: pin an IP to bypass broken DNS

If a network or VPN resolves your upstream hostname incorrectly, you may explicitly provide an IP:

```bat
start.bat --upstream https://api.example.com/v1 --upstream-ip 203.0.113.10
```

This does not change the URL, HTTP Host, TLS SNI, or certificate verification to the IP. If `HTTPS_PROXY` is set, an HTTP proxy may resolve the hostname itself and the pinned IP may not take effect. Follow your organization's network policy.

## Logs and troubleshooting

After startup, Steady Relay continuously prints request, retry, and response logs in the terminal that launched it; no separate log file is required. `[request] ... forwarding` means a Codex request entered the local relay. `[retry]` means a temporary upstream failure is being retried according to the backoff policy.

Normal logs do not contain API Keys, complete request bodies, or complete response bodies. They record the path, top-level `model`, status, retry count, duration, and a truncated upstream error summary.

- `[retry] ... attempt=3/10`: the local proxy is performing a safe retry.
- `status=429 attempts=11`: the first request plus all 10 retries failed.
- `stream=incomplete ... context canceled`: the client disconnected after output had been committed; retrying would not be safe.
- `POST /responses rejected status=404`: the client's Base URL is missing `/v1`.

## Run from source and test

Go 1.22+:

```bash
go test ./...
go vet ./...
UPSTREAM_BASE_URL=https://api.example.com/v1 go run .
```

Python standard-library fallback:

```bash
python3 -m unittest -v
UPSTREAM_BASE_URL=https://api.example.com/v1 python3 proxy.py
```

The Python fallback does not support `UPSTREAM_IP`; it uses system DNS. Its `RETRY_BACKOFF` environment variable is measured in seconds (for example, `0.5`), while the Go version uses Go durations (for example, `500ms`).

## Build release packages

```bash
./scripts/build-release.sh 2.2.0
```

Artifacts are written to `dist/`. When publishing, upload the six platform archives and `SHA256SUMS` to a GitHub Release. Do not commit `dist/` to the source repository.

## Trust a release package for the first run

The current binaries are not code-signed. Download only from a trusted GitHub Release and verify the SHA-256 checksum first.

- macOS: after confirming the source, right-click and open `install-macos.command`. It only clears the quarantine marker in the current extracted directory; it does not disable Gatekeeper. If no upstream is configured, it asks for an API Base URL in the terminal for this launch only and does not save it.
- Windows: after confirming the source and SHA-256, choose **More info → Run anyway** in SmartScreen. Do not disable Defender, SmartScreen, or the firewall.

See `README.txt` and `TRUST-GUIDE.zh-CN.txt` inside a release package for more usage details.

## Contributing and security reports

Please read [CONTRIBUTING.md](CONTRIBUTING.md). Do not disclose security vulnerabilities in a public Issue; see [SECURITY.md](SECURITY.md) for the reporting process. This project is released under the [MIT License](LICENSE).
