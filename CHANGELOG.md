# Changelog

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
