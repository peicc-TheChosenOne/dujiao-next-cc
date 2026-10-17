#!/usr/bin/env python3
"""Check repository inputs and optional release archives without printing secrets."""
import argparse
import ipaddress
import os
from pathlib import Path, PurePosixPath
import re
import subprocess
import tarfile
import zipfile

ROOT = Path(__file__).resolve().parents[1]
RELEASE_FILES = {"config.yml.example", "config.private.yml.example", "README.md", "scripts/dujiao-next-manager.sh"}
ARCHIVE_FILES = RELEASE_FILES | {"dujiao-next", "dujiao-next.exe", "dujiao-next-manager.sh"}
IPV4 = re.compile(rb"(?<![\d.])(?:\d{1,3}\.){3}\d{1,3}(?![\d.])")


def private_path(name):
    path = PurePosixPath(name)
    if any(part in {".ops-local", "output", "docs", "secrets", "backups", "db", "uploads", "logs", "node_modules", "dist"} for part in path.parts):
        return True
    if name.startswith(("deploy/config/", "deploy/data/", "deploy/tls/", "deploy/private/", "deploy/releases/")):
        return True
    base = path.name
    return (base.startswith(".env") and not base.endswith(".example")) or bool(re.fullmatch(r"config(?:\.[^.]+)*\.ya?ml", base)) or path.suffix.lower() in {".pem", ".key", ".ppk", ".db", ".log"}


def public_ips(data):
    result = set()
    for match in IPV4.finditer(data):
        try:
            if ipaddress.ip_address(match.group().decode()).is_global:
                result.add(match.group())
        except ValueError:
            pass
    return result


def local_private_values(root):
    values = set()
    names = [root / "config.yml", root / "config.private.yml", root / "deploy/config/config.yml", root / "deploy/secrets/config.private.yml"]
    names += [Path(os.environ[key]) for key in ("DUJIAO_CONFIG_FILE", "DUJIAO_PRIVATE_CONFIG_FILE") if os.environ.get(key)]
    for path in names:
        if not path.is_file():
            continue
        data = path.read_bytes()
        values.update(public_ips(data))
        for match in re.finditer(rb"(?m)^\s*(?:secret_key|secret|password|default_admin_password|bot_token|client_secret|dsn):\s*(.*?)\s*$", data):
            value = match.group(1).strip(b"\"'")
            if len(value) >= 8 and not any(marker in value.lower() for marker in (b"your-", b"change-me", b"change_me")):
                values.add(value)
            for password in re.finditer(rb"password=(?:'([^']*)'|\"([^\"]*)\"|([^\s]+))", value):
                token = next(item for item in password.groups() if item is not None)
                if len(token) >= 8 and b"change" not in token.lower():
                    values.add(token)
    return values


def content_errors(name, data, values):
    errors = []
    if any(value in data for value in values):
        errors.append(f"{name}: contains a local private value (value hidden)")
    if re.search(rb"-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----\s+[A-Za-z0-9+/=]{32}", data):
        errors.append(f"{name}: contains a private key")
    if (name.startswith("deploy/") or name in RELEASE_FILES) and public_ips(data):
        errors.append(f"{name}: contains a public IPv4 literal; use a local private file")
    return errors


def check_sources(root):
    names = subprocess.check_output(["git", "ls-files", "-z", "--cached", "--others", "--exclude-standard"], cwd=root).decode().split("\0")
    values = local_private_values(root)
    errors = []
    for name in filter(None, names):
        if private_path(name):
            errors.append(f"{name}: private/runtime/generated file is included in Git inputs")
        path = root / name
        if path.is_file():
            errors.extend(content_errors(name, path.read_bytes(), values))
    text = (root / ".goreleaser.yaml").read_text(encoding="utf-8")
    block = text.split("\narchives:\n", 1)[-1].split("\nchangelog:", 1)[0]
    listed = re.findall(r"^      - \./(.*)$", block, re.MULTILINE)
    if set(listed) != RELEASE_FILES or len(listed) != len(RELEASE_FILES):
        errors.append(".goreleaser.yaml: archive files must match the explicit public allowlist")
    if re.search(r"(?m)^\s+extra_files:", text):
        errors.append(".goreleaser.yaml: release extra_files bypasses the archive allowlist")
    for app in ("admin", "user"):
        for name in (".env", ".env.local", ".env.production", ".env.production.local"):
            if (root / "frontend" / app / name).exists():
                errors.append(f"frontend/{app}/{name}: may inject private values into the production frontend")
    if os.environ.get("VITE_API_BASE_URL"):
        errors.append("VITE_API_BASE_URL: release frontends must use the same origin")
    return errors


def archive_entry(name, regular, directory=False):
    path = PurePosixPath(name)
    allowed = {".", "scripts"} if directory else ARCHIVE_FILES
    if path.is_absolute() or ".." in path.parts or "\\" in name or str(path) not in allowed or not (regular or directory):
        return [f"archive entry {name}: outside the public allowlist or not a regular file"]
    return []


def check_archive(path, values=()):
    errors = []
    names = []
    if path.name.endswith(".zip"):
        with zipfile.ZipFile(path) as archive:
            for entry in archive.infolist():
                if entry.is_dir():
                    errors.extend(archive_entry(entry.filename, False, directory=True))
                    continue
                names.append(str(PurePosixPath(entry.filename)))
                regular = (entry.external_attr >> 16) & 0o170000 != 0o120000
                errors.extend(archive_entry(entry.filename, regular))
                errors.extend(content_errors(entry.filename, archive.read(entry), values))
    else:
        with tarfile.open(path) as archive:
            for entry in archive.getmembers():
                if entry.isdir():
                    errors.extend(archive_entry(entry.name, False, directory=True))
                    continue
                names.append(str(PurePosixPath(entry.name)))
                errors.extend(archive_entry(entry.name, entry.isfile()))
                if entry.isfile():
                    errors.extend(content_errors(entry.name, archive.extractfile(entry).read(), values))
    if not any(PurePosixPath(name).name in {"dujiao-next", "dujiao-next.exe"} for name in names):
        errors.append(f"{path.name}: missing application binary")
    if len(names) != len(set(names)):
        errors.append(f"{path.name}: duplicate archive entries")
    return errors


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--archives", type=Path, help="also inspect all tar.gz/zip archives in this directory")
    args = parser.parse_args()
    errors = check_sources(ROOT)
    if args.archives:
        paths = sorted(set(args.archives.rglob("*.tar.gz")) | set(args.archives.rglob("*.zip")))
        if not paths:
            errors.append("archive directory contains no release archives")
        for path in paths:
            errors.extend(check_archive(path, local_private_values(ROOT)))
    if errors:
        for error in errors:
            print(error)
        return 1
    print("Release safety checks passed; no secret values were printed.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
