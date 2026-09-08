#!/usr/bin/env python3
"""Download and install a Steady Relay GitHub release.

This helper intentionally uses only Python's standard library.  It is meant to
be called by the setup skill after the repository URL and the install action
have been confirmed by the user.  It never handles API keys and it refuses to
extract archives with path traversal (or symlink) entries.

Examples:
    python release_resolver.py https://github.com/937204197/steady-relay
    python release_resolver.py https://github.com/937204197/steady-relay/releases/tag/v2.2.0 \
        --dest ~/Applications/steady-relay
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import platform
import re
import shutil
import shlex
import stat
import sys
import tarfile
import tempfile
import urllib.error
import urllib.parse
import urllib.request
import zipfile
from pathlib import Path, PurePosixPath
from typing import Any, Iterable


DEFAULT_REPOSITORY = "937204197/steady-relay"
USER_AGENT = "steady-relay-setup/1"
MAX_REDIRECTS = 8


class ResolverError(RuntimeError):
    """An expected, user-actionable resolver failure."""


def fail(message: str) -> ResolverError:
    return ResolverError(message)


def parse_repository(value: str) -> tuple[str, str, str | None]:
    """Return (owner, repo, optional tag) from a GitHub URL or owner/repo."""
    raw = value.strip()
    if not raw:
        raise fail("repository URL is empty")
    if re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", raw):
        owner, repo = raw.split("/", 1)
        return owner, repo.removesuffix(".git"), None

    parsed = urllib.parse.urlparse(raw)
    if parsed.scheme not in {"http", "https"} or parsed.hostname not in {
        "github.com",
        "www.github.com",
    }:
        raise fail("only github.com repository or release URLs are accepted")
    pieces = [part for part in parsed.path.split("/") if part]
    if len(pieces) < 2:
        raise fail("GitHub URL must contain /owner/repository")
    owner, repo = pieces[0], pieces[1].removesuffix(".git")
    if not re.fullmatch(r"[A-Za-z0-9_.-]+", owner) or not re.fullmatch(
        r"[A-Za-z0-9_.-]+", repo
    ):
        raise fail("invalid GitHub owner or repository name")
    tag: str | None = None
    if len(pieces) >= 4 and pieces[2] == "releases" and pieces[3] == "tag":
        if len(pieces) < 5:
            raise fail("release tag is missing from GitHub URL")
        tag = urllib.parse.unquote(pieces[4])
    return owner, repo, tag


def ensure_repository_allowed(owner: str, repo: str, allow_untrusted: bool) -> None:
    if f"{owner}/{repo}".lower() != DEFAULT_REPOSITORY.lower() and not allow_untrusted:
        raise fail(
            f"refusing unverified repository {owner}/{repo}; this helper defaults to "
            f"{DEFAULT_REPOSITORY}. Re-run only after an explicit review with "
            "--allow-untrusted-repository."
        )


def normalize_os(value: str | None) -> str:
    if value and value != "auto":
        value = value.lower()
        aliases = {"macos": "darwin", "mac": "darwin", "win": "windows"}
        value = aliases.get(value, value)
        if value not in {"darwin", "linux", "windows"}:
            raise fail("--os must be darwin, linux, windows, or auto")
        return value
    system = platform.system().lower()
    if system == "darwin":
        return "darwin"
    if system == "linux":
        return "linux"
    if system == "windows":
        return "windows"
    raise fail(f"unsupported operating system: {platform.system()!r}")


def normalize_arch(value: str | None) -> str:
    if value and value != "auto":
        value = value.lower()
        aliases = {
            "x86_64": "amd64",
            "x64": "amd64",
            "amd64": "amd64",
            "aarch64": "arm64",
            "arm64": "arm64",
        }
        if value not in aliases:
            raise fail("--arch must be amd64, arm64, or auto")
        return aliases[value]
    machine = platform.machine().lower()
    if machine in {"x86_64", "amd64", "x64", "i386", "i686"}:
        return "amd64"
    if machine in {"aarch64", "arm64"}:
        return "arm64"
    raise fail(f"unsupported CPU architecture: {platform.machine()!r}")


def http_json(url: str) -> Any:
    request = urllib.request.Request(
        url,
        headers={"Accept": "application/vnd.github+json", "User-Agent": USER_AGENT},
    )
    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            return json.load(response)
    except urllib.error.HTTPError as exc:
        detail = exc.read(400).decode("utf-8", "replace")
        raise fail(f"GitHub API returned HTTP {exc.code} for {url}: {detail}") from exc
    except (urllib.error.URLError, TimeoutError) as exc:
        raise fail(f"cannot reach GitHub API: {exc}") from exc


def fetch_release(owner: str, repo: str, tag: str | None) -> dict[str, Any]:
    if tag:
        endpoint = f"https://api.github.com/repos/{owner}/{repo}/releases/tags/{urllib.parse.quote(tag, safe='')}"
    else:
        endpoint = f"https://api.github.com/repos/{owner}/{repo}/releases/latest"
    data = http_json(endpoint)
    if not isinstance(data, dict) or not isinstance(data.get("assets"), list):
        raise fail("GitHub response is not a release object")
    return data


def asset_name_matches(name: str, target_os: str, target_arch: str) -> bool:
    lowered = name.lower()
    if target_os not in lowered or target_arch not in lowered:
        return False
    return lowered.endswith((".tar.gz", ".tgz", ".zip"))


def choose_asset(assets: list[dict[str, Any]], target_os: str, target_arch: str) -> dict[str, Any]:
    matches = [
        asset
        for asset in assets
        if isinstance(asset, dict)
        and isinstance(asset.get("name"), str)
        and asset_name_matches(asset["name"], target_os, target_arch)
    ]
    if not matches:
        names = ", ".join(str(a.get("name")) for a in assets if isinstance(a, dict))
        raise fail(f"no release asset found for {target_os}/{target_arch}; available: {names}")
    if len(matches) > 1:
        names = ", ".join(str(a.get("name")) for a in matches)
        raise fail(f"multiple release assets match {target_os}/{target_arch}: {names}")
    asset = matches[0]
    if not isinstance(asset.get("browser_download_url"), str):
        raise fail("selected release asset has no download URL")
    return asset


def find_checksums(assets: Iterable[dict[str, Any]]) -> dict[str, str]:
    for asset in assets:
        name = str(asset.get("name", "")).lower()
        if name in {"sha256sums", "sha256sums.txt", "checksums.txt", "sha256.txt"}:
            url = asset.get("browser_download_url")
            if not isinstance(url, str):
                continue
            content = download_bytes(url, limit=2 * 1024 * 1024)
            return parse_checksum_file(content.decode("utf-8", "replace"))
    return {}


def parse_checksum_file(content: str) -> dict[str, str]:
    checksums: dict[str, str] = {}
    for line in content.splitlines():
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        match = re.match(r"^([0-9a-fA-F]{64})\s+[* ]?(.+?)\s*$", line)
        if match:
            checksums[Path(match.group(2)).name] = match.group(1).lower()
    return checksums


def download_bytes(url: str, limit: int | None = None) -> bytes:
    request = urllib.request.Request(url, headers={"User-Agent": USER_AGENT})
    try:
        with urllib.request.urlopen(request, timeout=120) as response:
            data = response.read(limit + 1 if limit is not None else -1)
    except urllib.error.HTTPError as exc:
        raise fail(f"download returned HTTP {exc.code}: {url}") from exc
    except (urllib.error.URLError, TimeoutError) as exc:
        raise fail(f"download failed for {url}: {exc}") from exc
    if limit is not None and len(data) > limit:
        raise fail(f"download is larger than the allowed limit ({limit} bytes): {url}")
    return data


def download_file(url: str, path: Path) -> str:
    request = urllib.request.Request(url, headers={"User-Agent": USER_AGENT})
    digest = hashlib.sha256()
    try:
        with urllib.request.urlopen(request, timeout=300) as response, path.open("wb") as output:
            while True:
                chunk = response.read(1024 * 1024)
                if not chunk:
                    break
                digest.update(chunk)
                output.write(chunk)
    except urllib.error.HTTPError as exc:
        raise fail(f"download returned HTTP {exc.code}: {url}") from exc
    except (urllib.error.URLError, TimeoutError, OSError) as exc:
        raise fail(f"download failed for {url}: {exc}") from exc
    return digest.hexdigest()


def safe_member_path(root: Path, member_name: str) -> Path:
    # Archives use POSIX separators even on Windows. Backslashes are rejected
    # so that a crafted Windows archive cannot change interpretation by host.
    if not member_name or "\x00" in member_name or "\\" in member_name:
        raise fail(f"unsafe archive member path: {member_name!r}")
    pure = PurePosixPath(member_name)
    if pure.is_absolute() or any(part in {"", ".", ".."} for part in pure.parts):
        raise fail(f"unsafe archive member path: {member_name!r}")
    destination = (root / Path(*pure.parts)).resolve()
    root_resolved = root.resolve()
    try:
        destination.relative_to(root_resolved)
    except ValueError as exc:
        raise fail(f"archive member escapes destination: {member_name!r}") from exc
    return destination


def extract_tar(archive: Path, destination: Path) -> None:
    try:
        with tarfile.open(archive, "r:gz") as tar:
            members = tar.getmembers()
            for member in members:
                if member.issym() or member.islnk() or member.isdev():
                    raise fail(f"refusing symlink/link/device in archive: {member.name!r}")
                target = safe_member_path(destination, member.name)
                if member.isdir():
                    target.mkdir(parents=True, exist_ok=True)
                elif member.isfile():
                    target.parent.mkdir(parents=True, exist_ok=True)
                    source = tar.extractfile(member)
                    if source is None:
                        raise fail(f"cannot read archive member: {member.name!r}")
                    with source, target.open("wb") as output:
                        shutil.copyfileobj(source, output)
                    mode = member.mode & 0o777
                    if mode:
                        target.chmod(mode)
                else:
                    raise fail(f"unsupported archive member: {member.name!r}")
    except (tarfile.TarError, OSError) as exc:
        raise fail(f"invalid tar archive: {exc}") from exc


def extract_zip(archive: Path, destination: Path) -> None:
    try:
        with zipfile.ZipFile(archive) as zipped:
            for info in zipped.infolist():
                target = safe_member_path(destination, info.filename)
                mode = (info.external_attr >> 16) & 0o170000
                if mode == stat.S_IFLNK:
                    raise fail(f"refusing symlink in archive: {info.filename!r}")
                if info.is_dir():
                    target.mkdir(parents=True, exist_ok=True)
                    continue
                target.parent.mkdir(parents=True, exist_ok=True)
                with zipped.open(info) as source, target.open("wb") as output:
                    shutil.copyfileobj(source, output)
                file_mode = (info.external_attr >> 16) & 0o777
                if file_mode:
                    target.chmod(file_mode)
    except (zipfile.BadZipFile, OSError) as exc:
        raise fail(f"invalid zip archive: {exc}") from exc


def default_destination(target_os: str | None = None) -> Path:
    home = Path.home()
    system = (target_os or platform.system()).lower()
    if system == "windows":
        return Path(os.environ.get("LOCALAPPDATA", home / "AppData" / "Local")) / "SteadyRelay"
    if system == "darwin":
        return home / "Applications" / "steady-relay"
    return home / ".local" / "share" / "steady-relay"


def install_release(
    release: dict[str, Any],
    target_os: str,
    target_arch: str,
    base_destination: Path,
    force: bool,
) -> dict[str, Any]:
    tag = str(release.get("tag_name", "")).strip()
    if not tag:
        raise fail("release has no tag_name")
    version = tag.removeprefix("v")
    assets = [asset for asset in release["assets"] if isinstance(asset, dict)]
    selected = choose_asset(assets, target_os, target_arch)
    asset_name = str(selected["name"])
    expected = find_checksums(assets).get(asset_name)
    digest_from_api = str(selected.get("digest", ""))
    if not expected and digest_from_api.startswith("sha256:"):
        expected = digest_from_api.removeprefix("sha256:").lower()
    if not expected or not re.fullmatch(r"[0-9a-f]{64}", expected):
        raise fail(f"no SHA-256 checksum was published for {asset_name}; refusing to install")

    destination = (base_destination.expanduser() / version / f"{target_os}-{target_arch}").resolve()
    destination.parent.mkdir(parents=True, exist_ok=True)
    if destination.exists():
        if not force:
            raise fail(f"destination already exists: {destination} (use --force only after checking it)")
        shutil.rmtree(destination)

    with tempfile.TemporaryDirectory(prefix="steady-relay-download-", dir=str(destination.parent)) as temp_dir:
        archive = Path(temp_dir) / asset_name
        actual = download_file(str(selected["browser_download_url"]), archive)
        if actual.lower() != expected.lower():
            raise fail(
                f"SHA-256 mismatch for {asset_name}: expected {expected}, received {actual}"
            )
        staging = Path(temp_dir) / "extracted"
        staging.mkdir()
        if asset_name.lower().endswith((".tar.gz", ".tgz")):
            extract_tar(archive, staging)
        elif asset_name.lower().endswith(".zip"):
            extract_zip(archive, staging)
        else:
            raise fail(f"unsupported archive format: {asset_name}")
        staging.rename(destination)

    top_level = sorted(path for path in destination.iterdir())
    package_dir = top_level[0] if len(top_level) == 1 and top_level[0].is_dir() else destination
    executable = package_dir / ("steady-relay.exe" if target_os == "windows" else "steady-relay")
    launcher = package_dir / ("start.bat" if target_os == "windows" else "start.sh")
    if not executable.is_file() or not launcher.is_file():
        raise fail(
            "release archive was extracted, but it does not contain the expected "
            f"{executable.name} and {launcher.name} files"
        )
    return {
        "repository": f"{release.get('html_url', '')}" or DEFAULT_REPOSITORY,
        "release_url": str(release.get("html_url", "")),
        "asset_url": str(selected.get("browser_download_url", "")),
        "tag": tag,
        "version": version,
        "os": target_os,
        "arch": target_arch,
        "asset": asset_name,
        "sha256": actual,
        "directory": str(package_dir),
        "archive_directory": str(destination),
        "start_command": "start.bat" if target_os == "windows" else "./start.sh",
        "cd_command": (
            f"Set-Location -LiteralPath {str(package_dir).replace(chr(39), chr(39) + chr(39))!r}; "
            f".\\start.bat"
            if target_os == "windows"
            else f"cd -- {shlex.quote(str(package_dir))} && ./start.sh"
        ),
    }


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "repository",
        nargs="?",
        default=f"https://github.com/{DEFAULT_REPOSITORY}",
        help="GitHub repository or release URL (default: official Steady Relay repository)",
    )
    parser.add_argument("--version", help="release tag, for example v2.2.0")
    parser.add_argument("--os", dest="target_os", default="auto", help="darwin, linux, windows, or auto")
    parser.add_argument("--arch", dest="target_arch", default="auto", help="amd64, arm64, or auto")
    parser.add_argument("--dest", type=Path, help="base install directory")
    parser.add_argument("--force", action="store_true", help="replace an existing version directory")
    parser.add_argument(
        "--allow-untrusted-repository",
        action="store_true",
        help="allow a repository other than the official one after explicit review",
    )
    parser.add_argument("--json", action="store_true", help="print the result as JSON")
    return parser


def main(argv: list[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    try:
        owner, repo, url_tag = parse_repository(args.repository)
        ensure_repository_allowed(owner, repo, args.allow_untrusted_repository)
        tag = args.version or url_tag
        target_os = normalize_os(args.target_os)
        target_arch = normalize_arch(args.target_arch)
        print(f"Resolving {owner}/{repo} for {target_os}/{target_arch}...", file=sys.stderr)
        release = fetch_release(owner, repo, tag)
        result = install_release(
            release,
            target_os,
            target_arch,
            args.dest or default_destination(target_os),
            args.force,
        )
        if args.json:
            print(json.dumps(result, ensure_ascii=False, indent=2))
        else:
            print(f"Installed {result['tag']} ({result['asset']})")
            print(f"Directory: {result['directory']}")
            print(f"SHA-256: {result['sha256']}")
            print(f"Change directory and start: {result['cd_command']}")
        return 0
    except ResolverError as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 2
    except KeyboardInterrupt:
        print("error: cancelled", file=sys.stderr)
        return 130


if __name__ == "__main__":
    raise SystemExit(main())
