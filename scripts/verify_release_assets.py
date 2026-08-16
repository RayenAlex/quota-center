#!/usr/bin/env python3
"""Verify a quota-center release archive and its detached checksum file."""
from __future__ import annotations

import argparse
import hashlib
import re
import stat
import zipfile
from pathlib import Path


ALLOWED_LIBRARY_NAMES = {
    "quota-center.dylib",
    "quota-center.so",
    "quota-center.dll",
}
CHECKSUM_LINE = re.compile(r"^([0-9a-f]{64})  ([^/\\\n]+)\n?$")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--archive", type=Path, required=True)
    parser.add_argument("--sha256", type=Path, required=True)
    return parser.parse_args()


def fail(message: str) -> None:
    raise SystemExit(message)


def verify_checksum(archive: Path, checksum_path: Path) -> None:
    try:
        checksum_line = checksum_path.read_text(encoding="utf-8")
    except OSError as error:
        fail(str(error))
    match = CHECKSUM_LINE.fullmatch(checksum_line)
    if not match:
        fail(f"{checksum_path}: expected one sha256sum-compatible line")
    expected_digest, archived_name = match.groups()
    if archived_name != archive.name:
        fail(f"{checksum_path}: archive name {archived_name!r} does not match {archive.name!r}")
    try:
        actual_digest = hashlib.sha256(archive.read_bytes()).hexdigest()
    except OSError as error:
        fail(str(error))
    if actual_digest != expected_digest:
        fail(f"{archive}: sha256 mismatch: expected {expected_digest}, got {actual_digest}")


def verify_archive(archive: Path) -> None:
    try:
        with zipfile.ZipFile(archive) as reader:
            entries = reader.infolist()
    except (OSError, zipfile.BadZipFile) as error:
        fail(str(error))
    if len(entries) != 1:
        fail(f"{archive}: expected exactly one ZIP entry, got {len(entries)}")
    entry = entries[0]
    if entry.is_dir() or entry.filename not in ALLOWED_LIBRARY_NAMES:
        fail(f"{archive}: unexpected ZIP entry {entry.filename!r}")
    mode = (entry.external_attr >> 16) & 0o777
    if mode != 0o755:
        fail(f"{archive}: {entry.filename!r} mode is {stat.filemode(mode)}, want -rwxr-xr-x")


def main() -> int:
    args = parse_args()
    verify_checksum(args.archive, args.sha256)
    verify_archive(args.archive)
    print(f"OK {args.archive.name}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
