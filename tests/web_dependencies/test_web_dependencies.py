"""Exercise the real Makefile and desktop entry point without building an app."""

import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[2]


class WebDependenciesTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="track-web-deps-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.web = self.root / "web"
        self.web.mkdir()
        (self.root / "scripts").mkdir()
        shutil.copy2(ROOT / "Makefile", self.root / "Makefile")
        shutil.copy2(ROOT / "scripts/desktop-build.sh", self.root / "scripts/desktop-build.sh")
        for name in ("package.json", "package-lock.json"):
            (self.web / name).write_text("{}\n")
        self.stamp = self.web / "node_modules/.track-deps-ready"
        self.log = self.root / "calls"
        self.bin = self.root / "bin"
        self.bin.mkdir()
        self.env = os.environ.copy()
        self.env.update(PATH=str(self.bin) + os.pathsep + self.env["PATH"],
                        WEB_DEPS_TEST_LOG=str(self.log))
        # Do not let an outer make or a developer's desktop overrides affect the fixture.
        for name in ("MAKEFLAGS", "MFLAGS", "MAKELEVEL", "DESKTOP_APP"):
            self.env.pop(name, None)
        self.executable(self.bin / "uname", "printf 'Darwin\\n'\n")
        for name in ("go", "node", "python3"):
            self.executable(self.bin / name, "exit 99\n")
        self.executable(self.bin / "npm", '''set -eu
test "$*" = ci
printf 'npm\\n' >> "$WEB_DEPS_TEST_LOG"
mkdir -p node_modules
rm -f node_modules/.track-deps-ready
test "${WEB_DEPS_TEST_FAIL:-0}" = 0
''')

    def executable(self, path, body):
        path.write_text("#!/bin/sh\n" + body)
        path.chmod(0o755)

    def installed_tools(self):
        path = self.web / "node_modules/.bin"
        path.mkdir(parents=True, exist_ok=True)
        # Stop at tsc so the real desktop script cannot reach app or vault operations.
        self.executable(path / "tsc", 'printf "tsc\\n" >> "$WEB_DEPS_TEST_LOG"\nexit 78\n')
        self.executable(path / "vite", "exit 99\n")

    def run_command(self, *args, succeeds=True):
        self.log.write_text("")
        result = subprocess.run(args, cwd=self.root, env=self.env,
                                text=True, capture_output=True)
        self.assertEqual(result.returncode == 0, succeeds, result.stdout + result.stderr)
        return self.log.read_text().splitlines()

    def sync(self, succeeds=True):
        return self.run_command("make", "--no-print-directory", "web/node_modules", succeeds=succeeds)

    def test_fresh_install_is_reused(self):
        self.assertEqual(self.sync(), ["npm"])
        self.assertTrue(self.stamp.is_file())
        self.assertEqual(self.sync(), [])

    def test_manifest_changes_refresh_even_if_directory_is_newer(self):
        for name in ("package.json", "package-lock.json"):
            with self.subTest(manifest=name):
                self.sync()
                os.utime(self.stamp, (1, 1))
                (self.web / name).write_text('{"changed": true}\n')
                (self.web / "node_modules/new-file").touch()
                self.assertEqual(self.sync(), ["npm"])
                self.assertEqual(self.sync(), [])

    def test_failed_install_stops_desktop_and_retries(self):
        self.installed_tools()
        self.sync()
        os.utime(self.stamp, (1, 1))
        self.env["WEB_DEPS_TEST_FAIL"] = "1"
        self.assertEqual(self.run_command("make", "desktop-app", succeeds=False), ["npm"])
        self.assertFalse(self.stamp.exists())
        self.env.pop("WEB_DEPS_TEST_FAIL")
        self.assertEqual(self.sync(), ["npm"])
        self.assertTrue(self.stamp.is_file())

    def test_removed_node_modules_is_reinstalled(self):
        self.sync()
        shutil.rmtree(self.web / "node_modules")
        self.assertEqual(self.sync(), ["npm"])

    def test_desktop_syncs_existing_tools_and_reuses_install(self):
        self.installed_tools()
        self.assertEqual(self.run_command("make", "desktop-app", succeeds=False), ["npm", "tsc"])
        self.assertTrue(self.stamp.is_file())
        self.assertEqual(self.run_command("make", "desktop-app", succeeds=False), ["tsc"])

    def test_direct_desktop_script_uses_same_dependency_rule(self):
        self.installed_tools()
        self.assertEqual(self.run_command("sh", "scripts/desktop-build.sh", succeeds=False), ["npm", "tsc"])
        os.utime(self.stamp, (1, 1))
        self.assertEqual(self.run_command("sh", "scripts/desktop-build.sh", succeeds=False), ["npm", "tsc"])


if __name__ == "__main__":
    unittest.main()
