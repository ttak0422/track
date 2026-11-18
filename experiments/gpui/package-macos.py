#!/usr/bin/env python3
"""Package an explicitly built GPUI binary without installing or launching it."""
import argparse
import os
from pathlib import Path
import plistlib
import shutil


def package(binary: Path, output: Path, icon: Path) -> Path:
    binary = binary.resolve(strict=True)
    icon = icon.resolve(strict=True)
    if not binary.is_file() or not os.access(binary, os.X_OK):
        raise ValueError("binary must be an existing executable file")
    if not icon.is_file():
        raise ValueError("Track.icns must be an existing regular file")
    if output.suffix != ".app":
        raise ValueError("output must have an .app suffix")
    # No installation, parent creation, replacement, or follow-through of a
    # destination symlink. mkdir is the exclusive claim, including dangling links.
    output = output.parent.resolve(strict=True) / output.name
    output.mkdir(mode=0o755)
    try:
        contents = output / "Contents"
        executable_dir = contents / "MacOS"
        resources = contents / "Resources"
        executable_dir.mkdir(parents=True)
        resources.mkdir()
        executable = executable_dir / "track-gpui-poc"
        with binary.open("rb") as source, executable.open("xb") as target:
            shutil.copyfileobj(source, target)
        executable.chmod(0o755)
        with icon.open("rb") as source, (resources / "Track.icns").open("xb") as target:
            shutil.copyfileobj(source, target)
        metadata = {
            "CFBundleIdentifier": "com.ttak0422.track.gpui.experimental",
            "CFBundleName": "Track Native Experimental",
            "CFBundleDisplayName": "Track Native Experimental",
            "CFBundleExecutable": "track-gpui-poc",
            "CFBundleIconFile": "Track.icns",
            "CFBundlePackageType": "APPL",
            "CFBundleVersion": "1",
            "CFBundleShortVersionString": "0.1.0",
            "NSHighResolutionCapable": True,
        }
        with (contents / "Info.plist").open("xb") as target:
            plistlib.dump(metadata, target)
    except Exception as error:
        # Never recursively delete a path that could have been modified by a
        # concurrent process. Leave an explicit partial output for inspection.
        raise RuntimeError(f"Packaging failed; partial output retained at {output}: {error}") from error
    return output


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True, help="already-built macOS GPUI executable")
    parser.add_argument("--output", type=Path, required=True, help="new .app path in an existing directory")
    args = parser.parse_args()
    icon = Path(__file__).resolve().parents[2] / "desktop/Resources/Track.icns"
    try:
        output = package(args.binary, args.output, icon)
    except (OSError, ValueError, RuntimeError) as error:
        parser.exit(1, f"{error}\n")
    print(f"Created {output}; not installed or launched. Launch without arguments uses synthetic data.")


if __name__ == "__main__":
    main()
