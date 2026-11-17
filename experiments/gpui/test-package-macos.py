#!/usr/bin/env python3
"""Filesystem-only packaging tests; never launch the fixture executable."""
import importlib.util
from pathlib import Path
import plistlib
import tempfile
import unittest

spec = importlib.util.spec_from_file_location("package_macos", Path(__file__).with_name("package-macos.py"))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class Packaging(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.binary = self.root / "fixture-binary"
        self.binary.write_bytes(b"synthetic executable fixture; must never run")
        self.binary.chmod(0o755)
        self.icon = Path(__file__).resolve().parents[2] / "desktop/Resources/Track.icns"
        self.output = self.root / "Native QA.app"

    def test_binary_icon_and_metadata(self):
        module.package(self.binary, self.output, self.icon)
        contents = self.output / "Contents"
        info = plistlib.loads((contents / "Info.plist").read_bytes())
        self.assertEqual(info["CFBundleIconFile"], "Track.icns")
        self.assertEqual(info["CFBundleIdentifier"], "com.ttak0422.track.gpui.experimental")
        self.assertNotEqual(info["CFBundleIdentifier"], "com.ttak0422.track.web")
        self.assertEqual((contents / "Resources/Track.icns").read_bytes(), self.icon.read_bytes())
        executable = contents / "MacOS" / info["CFBundleExecutable"]
        self.assertEqual(executable.read_bytes(), self.binary.read_bytes())
        self.assertTrue(executable.stat().st_mode & 0o111)
        self.assertNotIn("LSEnvironment", info)
        self.assertNotIn("ProgramArguments", info)

    def test_existing_output_is_preserved(self):
        self.output.mkdir()
        marker = self.output / "owner-file"
        marker.write_bytes(b"keep")
        with self.assertRaises(FileExistsError):
            module.package(self.binary, self.output, self.icon)
        self.assertEqual(marker.read_bytes(), b"keep")
        self.assertEqual(list(self.output.iterdir()), [marker])

    def test_dangling_symlink_is_preserved(self):
        self.output.symlink_to(self.root / "missing")
        with self.assertRaises(FileExistsError):
            module.package(self.binary, self.output, self.icon)
        self.assertTrue(self.output.is_symlink())
        self.assertFalse((self.root / "missing").exists())

    def test_missing_binary_does_not_create_output(self):
        with self.assertRaises(FileNotFoundError):
            module.package(self.root / "missing", self.output, self.icon)
        self.assertFalse(self.output.exists())

    def test_non_executable_does_not_create_output(self):
        self.binary.chmod(0o600)
        with self.assertRaises(ValueError):
            module.package(self.binary, self.output, self.icon)
        self.assertFalse(self.output.exists())


if __name__ == "__main__":
    unittest.main()
