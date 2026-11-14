#!/usr/bin/env python3
"""Run the exact pure gate tests from the generated dependency, not a copied model.

This does not simulate AppKit or verify native application termination.
"""
import os
from pathlib import Path
import re
import subprocess
import tempfile

root = Path(__file__).resolve().parent
source = (root / ".build-deps/gpui/src/platform/mac/platform.rs").read_text()
blocks = []
for declaration in ("fn should_cancel_native_quit", "mod termination_gate_tests"):
    matches = re.findall(r"(?ms)^" + declaration + r"\b.*?^\}\n", source)
    if len(matches) != 1:
        raise SystemExit(f"Expected one pinned source block: {declaration}")
    blocks.append(matches[0])
with tempfile.TemporaryDirectory(prefix="track-native-gate-") as directory:
    path = Path(directory)
    unit = path / "gate.rs"
    unit.write_text("\n".join(blocks))
    binary = path / "gate-tests"
    subprocess.run([os.environ.get("RUSTC", "rustc"), "--test", str(unit), "-o", str(binary)], check=True)
    subprocess.run([str(binary)], check=True)
    # The Cargo build guard must reject absent or stale preparation, not quietly
    # use a previously generated dependency after a failed update.
    guard = path / "build-guard"
    subprocess.run([os.environ.get("RUSTC", "rustc"), str(root / "build.rs"), "-o", str(guard)], check=True)
    expected = path / "patches/native-termination.patch"
    prepared = path / ".build-deps/gpui/.track-native-termination.patch"
    expected.parent.mkdir(parents=True)
    prepared.parent.mkdir(parents=True)
    expected.write_bytes(b"expected patch")
    environment = dict(os.environ, CARGO_MANIFEST_DIR=str(path))
    for content, succeeds in ((None, False), (b"stale patch", False), (b"expected patch", True)):
        if content is not None:
            prepared.write_bytes(content)
        result = subprocess.run([str(guard)], env=environment, capture_output=True)
        assert (result.returncode == 0) == succeeds, result.stderr.decode()
    print("Preparation guard: absent/stale rejected; matching source accepted.")
