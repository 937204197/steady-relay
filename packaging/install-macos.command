#!/bin/sh
set -eu

PACKAGE_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

echo "Codex Reroute macOS first-run helper"
echo "Package: $PACKAGE_DIR"
echo
echo "This removes Apple's download quarantine flag from this package only."
echo "It does not disable Gatekeeper or change system-wide security settings."
printf "Continue? [y/N] "
read answer
case "$answer" in
  y|Y|yes|YES)
    xattr -dr com.apple.quarantine "$PACKAGE_DIR"
    chmod +x "$PACKAGE_DIR/codex-reroute" "$PACKAGE_DIR/start.sh"
    echo
    echo "Quarantine removed. Starting Codex Reroute..."
    exec "$PACKAGE_DIR/start.sh"
    ;;
  *)
    echo "Cancelled."
    ;;
esac
