# Changelog

## 2.2.0

- Added the opt-in `--buffer-until-success` / `BUFFER_UNTIL_SUCCESS=true` mode.
  SSE output is held until `response.completed`, so late capacity failures can
  be retried without leaking partial text or tool calls to Codex.
- Added a 64 MiB per-attempt buffer limit and standalone-package documentation.

## 2.1.4

- Retry pre-output capacity failures even when the upstream first announces an
  empty output item or content part in the SSE stream.
- Added regression coverage to ensure those structural announcements and the
  failed attempt are not leaked into the retried response.

## 2.1.2

- Fixed retries for upstream SSE streams that send `keepalive`, `ping`, or
  `heartbeat` events before a pre-output `response.failed` capacity error.
  Heartbeat frames remain buffered and are not mistaken for committed model
  output.

## 2.1.1

- Documented the model-capacity and temporary upstream failure use case, including
  searchable error names such as `model_capacity` and `server_is_overloaded`.

## 2.1.0

- Renamed the project and release binary to Steady Relay (`steady-relay`).

## 2.0.1

- macOS `install-macos.command` now prompts for an upstream API Base URL when
  none is configured, uses it only for that launch, and then starts the proxy.

## 2.0.0

### Breaking changes

- Removed the built-in third-party upstream URL and fixed IP. `UPSTREAM_BASE_URL`
  or `--upstream` is now required.
- `UPSTREAM_IP` / `--upstream-ip` is optional for any explicitly configured
  upstream and defaults to system DNS.

### Added

- Open-source project files, CI, contribution guidance, security reporting
  guidance, issue templates, and release preparation documentation.
