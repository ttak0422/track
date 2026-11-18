#!/usr/bin/env python3
"""Exercise the build script in a disposable Git repository and source archive.

Pass an executable compiled from build.rs with rustc; no GPUI, GUI or vault needed.
"""
import os
from pathlib import Path
import subprocess
import sys
import tempfile

script = str(Path(sys.argv[1]).resolve())
with tempfile.TemporaryDirectory(prefix="track-build-identity-") as temp:
    root = Path(temp)
    project = root / "experiments/gpui"
    for path in ("patches/native-termination.patch", ".build-deps/gpui/.track-native-termination.patch"):
        target = project / path
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text("fixture patch\n")

    def run(*args):
        return subprocess.check_output(args, cwd=root, text=True).strip()

    def identity():
        return subprocess.check_output([script], cwd=root, text=True, env={
            **os.environ, "CARGO_MANIFEST_DIR": str(project), "TARGET": "fixture-target",
        })

    assert "TRACK_BUILD_REVISION=unknown" in identity()
    run("git", "init", "-q")
    run("git", "add", ".")
    run("git", "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "fixture")
    head = run("git", "rev-parse", "HEAD")
    clean = identity()
    assert f"TRACK_BUILD_REVISION={head}" in clean
    assert "TRACK_BUILD_SOURCE_STATE=clean" in clean
    assert "TRACK_BUILD_TARGET=fixture-target" in clean
    for line in clean.splitlines():
        if line.startswith("cargo:rerun-if-changed="):
            assert Path(line.split("=", 1)[1]).exists(), line
    assert str(project / "patches/native-termination.patch") in clean
    (root / "untracked").write_text("not covered\n")
    assert "TRACK_BUILD_SOURCE_STATE=clean" in identity()
    for path in ("patches/native-termination.patch", ".build-deps/gpui/.track-native-termination.patch"):
        (project / path).write_text("modified fixture patch\n")
    assert "TRACK_BUILD_SOURCE_STATE=modified" in identity()
    # A linked worktree has a .git file, not a .git directory.
    linked = root / "linked"
    run("git", "worktree", "add", "--detach", str(linked), head)
    project = linked / "experiments/gpui"
    assert "TRACK_BUILD_SOURCE_STATE=clean" in identity()
    assert f"TRACK_BUILD_REVISION={head}" in identity()
print("build identity: archive, clean, modified, untracked and worktree checks passed")
