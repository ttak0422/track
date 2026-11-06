#!/usr/bin/env python3
"""Stage the desktop package with a source identity compiled into its executable."""
import argparse
from datetime import datetime, timezone
import json
from pathlib import Path
import re
import shutil
import subprocess


def git(root, *args):
    return subprocess.check_output(
        ["git", "-C", str(root), *args], stderr=subprocess.DEVNULL
    ).decode().strip()


def source_identity(root):
    """Do not mistake an archive nested in another checkout for that checkout."""
    unknown = {"revision": "unknown", "state": "unknown"}
    try:
        if Path(git(root, "rev-parse", "--show-toplevel")).resolve() != root.resolve():
            return unknown
        revision = git(root, "rev-parse", "--verify", "HEAD")
        if not re.fullmatch(r"[0-9a-f]{40}|[0-9a-f]{64}", revision):
            return unknown
        state = "modified" if git(root, "status", "--porcelain", "--untracked-files=normal") else "clean"
        return {"revision": revision, "state": state}
    except (OSError, subprocess.SubprocessError, UnicodeError):
        return unknown


def stage(root, destination):
    identity = source_identity(root)
    identity["builtAt"] = datetime.now(timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z")
    # Keep SwiftPM's incremental cache, but never retain deleted source files.
    sources = destination / "Sources"
    if sources.exists():
        shutil.rmtree(sources)
    destination.mkdir(parents=True, exist_ok=True)
    shutil.copy2(root / "desktop/Package.swift", destination / "Package.swift")
    shutil.copytree(root / "desktop/Sources", sources)
    values = ", ".join(f"{key}: {json.dumps(value)}" for key, value in identity.items())
    (sources / "TrackWebCore/BuildInfo.generated.swift").write_text(
        "// Generated for this build only. Never read source-control state at runtime.\n"
        "extension BuildInfo {\n"
        f"    public static let current = BuildInfo({values})\n"
        "}\n", encoding="utf-8"
    )
    (destination / "build-info.json").write_text(json.dumps(identity) + "\n", encoding="utf-8")


def verify(root, destination, executable=None):
    original = json.loads((destination / "build-info.json").read_text())
    if source_identity(root) != {key: original[key] for key in ("revision", "state")}:
        raise SystemExit("Source identity changed during the desktop build; rebuild before distributing the app.")
    if executable is not None:
        expected = (f"Track for macOS\nCommit: {original['revision']}\n"
                    f"Source state: {original['state']}\nBuilt (UTC): {original['builtAt']}\n")
        actual = subprocess.check_output([str(executable), "--version"], text=True, timeout=10)
        if actual != expected:
            raise SystemExit("The desktop executable does not match its build stamp.")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("root", type=Path)
    parser.add_argument("destination", type=Path)
    parser.add_argument("--verify", action="store_true")
    parser.add_argument("--executable", type=Path)
    args = parser.parse_args()
    root = args.root.resolve()
    destination = args.destination.resolve()
    # The script only owns generated staging under this checkout's ignored build directory.
    if not destination.is_relative_to(root / "build") or destination == root / "build":
        parser.error("destination must be a staging directory under root/build")
    if args.verify:
        verify(root, destination, args.executable)
    else:
        if args.executable is not None:
            parser.error("--executable requires --verify")
        stage(root, destination)


if __name__ == "__main__":
    main()
