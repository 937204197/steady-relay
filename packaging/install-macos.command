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
    chmod +x "$PACKAGE_DIR/codex-reroute" "$PACKAGE_DIR/start.sh" "$PACKAGE_DIR/install-macos.command"
    echo
    echo "Quarantine removed."

    has_upstream_flag=false
    for argument in "$@"; do
      case "$argument" in
        --upstream|--upstream=*) has_upstream_flag=true ;;
      esac
    done
    if [ -z "${UPSTREAM_BASE_URL:-}" ] && [ "$has_upstream_flag" = false ]; then
      echo
      echo "Enter a trusted OpenAI-compatible upstream API Base URL."
      echo "It usually ends in /v1 and is used only for this launch."
      printf "Upstream API Base URL: "
      IFS= read -r entered_upstream
      if [ -z "$entered_upstream" ]; then
        echo "No upstream URL was entered. Nothing was started."
        exit 2
      fi
      UPSTREAM_BASE_URL=$entered_upstream
      export UPSTREAM_BASE_URL
    fi

    echo "Starting Codex Reroute..."
    exec "$PACKAGE_DIR/start.sh" "$@"
    ;;
  *)
    echo "Cancelled."
    ;;
esac
