#!/usr/bin/env python3
"""Exercise prepare.sh link policy with a stub; this does NOT evaluate Nix."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

root = Path(__file__).resolve().parent
with tempfile.TemporaryDirectory(prefix="track-prepare-") as directory:
    path = Path(directory)
    project, binaries, store = path / "project", path / "bin", path / "store"
    project.mkdir()
    binaries.mkdir()
    store.mkdir()
    shutil.copyfile(root / "prepare.sh", project / "prepare.sh")
    stub = binaries / "nix-build"
    stub.write_text('''#!/bin/sh
set -eu
[ "$2" = --argstr ] && [ "$3" = package ] && [ "$5" = --out-link ]
mkdir -p "$TEST_STORE/$4"
printf '%s\\n' "$4" >> "$TEST_CALLS"
ln -sfn "$TEST_STORE/$4" "$6"
''')
    stub.chmod(0o755)
    calls = path / "calls"
    env = dict(os.environ, PATH=str(binaries) + os.pathsep + os.environ["PATH"], TEST_STORE=str(store), TEST_CALLS=str(calls))
    def prepare(ok):
        result = subprocess.run(["sh", str(project / "prepare.sh")], env=env, capture_output=True)
        assert (result.returncode == 0) == ok, result.stderr.decode()
    prepare(True)
    links = project / ".build-deps"
    assert all((links / name).is_symlink() for name in ("gpui", "xattr"))
    assert calls.read_text().splitlines() == ["gpui", "xattr"]
    # Re-preparation must replace even a dangling old generated link.
    (links / "xattr").unlink()
    (links / "xattr").symlink_to(path / "missing")
    prepare(True)
    assert (links / "xattr").resolve() == store / "xattr"
    for name, kind in (("gpui", "file"), ("xattr", "directory")):
        destination = links / name
        destination.unlink()
        if kind == "file":
            destination.write_text("do not overwrite")
        else:
            destination.mkdir()
            (destination / "sentinel").write_text("do not overwrite")
        before = calls.read_bytes()
        prepare(False)
        assert calls.read_bytes() == before, "preflight must precede both builds"
        if kind == "file":
            assert destination.read_text() == "do not overwrite"
            destination.unlink()
        else:
            assert (destination / "sentinel").read_text() == "do not overwrite"
            (destination / "sentinel").unlink()
            destination.rmdir()
        destination.symlink_to(store / name)
    print("prepare.sh: clean/repeated/dangling links and non-symlink preflight PASS (Nix stub only)")
