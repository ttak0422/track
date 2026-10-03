#!/usr/bin/env python3
"""Bounded native fixture: no Track server, vault, persistent WebKit data, or app installation."""
import os
from pathlib import Path
import subprocess
import tempfile

root = Path(__file__).resolve().parent.parent
env = dict(os.environ)
env.setdefault("DEVELOPER_DIR", "/Library/Developer/CommandLineTools")
sdk = env.get("DESKTOP_SDKROOT") or subprocess.check_output(
    ["xcrun", "--sdk", "macosx", "--show-sdk-path"], env=env, text=True
).strip()
env["SDKROOT"] = sdk
with tempfile.TemporaryDirectory(prefix="track-window-test-") as folder:
    binary = str(Path(folder) / "fixture")
    subprocess.run([
        "swiftc", "-module-cache-path", str(root / "build/desktop-clang-module-cache"),
        str(root / "desktop/Sources/TrackWebApp/WorkspaceWindowChrome.swift"),
        str(root / "desktop/Tests/WindowChrome.swift"), "-o", binary,
    ], env=env, check=True, timeout=180)
    subprocess.run([binary], env=env, check=True, timeout=30)
