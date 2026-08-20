#!/bin/sh
# Build self-contained Codex Reroute archives for common desktop/server targets.
# End users do not need Go or Python; only the machine running this script needs Go.
set -eu

PROJECT_ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
VERSION=${1:-dev}
OUTPUT_DIR=${OUTPUT_DIR:-"$PROJECT_ROOT/dist"}
PROGRAM_NAME=codex-reroute

if ! command -v go >/dev/null 2>&1; then
  echo "error: Go is required to build release archives" >&2
  exit 1
fi

case "$VERSION" in
  *[!A-Za-z0-9._-]*|'')
    echo "error: version may contain only letters, digits, dot, underscore and hyphen" >&2
    exit 1
    ;;
esac

STAGING_DIR=$(mktemp -d "${TMPDIR:-/tmp}/codex-reroute-release.XXXXXX")
cleanup() {
  rm -rf "$STAGING_DIR"
}
trap cleanup EXIT HUP INT TERM

mkdir -p "$OUTPUT_DIR"
rm -f "$OUTPUT_DIR"/"$PROGRAM_NAME-$VERSION-"*.tar.gz
rm -f "$OUTPUT_DIR"/"$PROGRAM_NAME-$VERSION-"*.zip
rm -f "$OUTPUT_DIR/SHA256SUMS"

build_target() {
  target_os=$1
  target_arch=$2
  suffix=$3
  archive_kind=$4
  package="$PROGRAM_NAME-$VERSION-$target_os-$target_arch"
  package_dir="$STAGING_DIR/$package"

  mkdir -p "$package_dir"
  echo "building $target_os/$target_arch"
  (
    cd "$PROJECT_ROOT"
    CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" \
      go build -trimpath -ldflags="-s -w -X main.buildVersion=$VERSION" -o "$package_dir/$PROGRAM_NAME$suffix" .
  )

  cp "$PROJECT_ROOT/packaging/README.txt" "$package_dir/README.txt"
  cp "$PROJECT_ROOT/README.md" "$package_dir/README.md"
  cp "$PROJECT_ROOT/LICENSE" "$package_dir/LICENSE"
  cp "$PROJECT_ROOT/packaging/TRUST-GUIDE.zh-CN.txt" "$package_dir/TRUST-GUIDE.zh-CN.txt"
  if [ "$target_os" = windows ]; then
    cp "$PROJECT_ROOT/packaging/start.bat" "$package_dir/start.bat"
  else
    cp "$PROJECT_ROOT/packaging/start.sh" "$package_dir/start.sh"
    if [ "$target_os" = darwin ]; then
      cp "$PROJECT_ROOT/packaging/install-macos.command" "$package_dir/install-macos.command"
      chmod +x "$package_dir/install-macos.command"
    fi
    chmod +x "$package_dir/$PROGRAM_NAME" "$package_dir/start.sh"
  fi

  if [ "$archive_kind" = zip ]; then
    if ! command -v zip >/dev/null 2>&1; then
      echo "error: zip is required to package Windows builds" >&2
      exit 1
    fi
    (cd "$STAGING_DIR" && zip -q -r "$OUTPUT_DIR/$package.zip" "$package")
  else
    (cd "$STAGING_DIR" && tar -czf "$OUTPUT_DIR/$package.tar.gz" "$package")
  fi
}

build_target darwin amd64 "" tar
build_target darwin arm64 "" tar
build_target linux amd64 "" tar
build_target linux arm64 "" tar
build_target windows amd64 .exe zip
build_target windows arm64 .exe zip

(
  cd "$OUTPUT_DIR"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$PROGRAM_NAME-$VERSION-"*.tar.gz "$PROGRAM_NAME-$VERSION-"*.zip > SHA256SUMS
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$PROGRAM_NAME-$VERSION-"*.tar.gz "$PROGRAM_NAME-$VERSION-"*.zip > SHA256SUMS
  else
    echo "warning: sha256sum/shasum unavailable; SHA256SUMS was not generated" >&2
  fi
)

echo "release archives written to $OUTPUT_DIR"
