#!/bin/sh
set -eu

PACKAGE_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
if [ -z "${UPSTREAM_BASE_URL:-}" ] && [ "$#" -eq 0 ]; then
  echo "UPSTREAM_BASE_URL is required. Example:" >&2
  echo "  UPSTREAM_BASE_URL=https://api.example.com/v1 ./start.sh" >&2
  echo "Or pass: ./start.sh --upstream https://api.example.com/v1" >&2
  exit 2
fi
exec "$PACKAGE_DIR/steady-relay" "$@"
