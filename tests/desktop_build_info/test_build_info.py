import importlib.util
import json
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location("desktop_build_info", ROOT / "scripts/desktop-build-info.py")
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class BuildInfoTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.run_git("init", "-q")
        self.run_git("config", "user.email", "fixture@example.invalid")
        self.run_git("config", "user.name", "Build fixture")
        (self.root / ".gitignore").write_text("/build/\n")
        (self.root / "desktop/Sources/TrackWebCore").mkdir(parents=True)
        (self.root / "desktop/Package.swift").write_text("// fixture\n")
        (self.root / "desktop/Sources/TrackWebCore/BuildInfo.swift").write_text("// fixture\n")
        self.run_git("add", ".")
        self.run_git("commit", "-qm", "fixture")
        self.head = self.run_git("rev-parse", "HEAD")
        self.destination = self.root / "build/desktop-package"

    def run_git(self, *args):
        return subprocess.check_output(["git", "-C", str(self.root), *args]).decode().strip()

    def test_clean_checkout_and_detached_release(self):
        self.assertEqual(module.source_identity(self.root), {"revision": self.head, "state": "clean"})
        self.run_git("tag", "v1.2.3")
        self.run_git("checkout", "--detach", "-q", "v1.2.3")
        self.assertEqual(module.source_identity(self.root), {"revision": self.head, "state": "clean"})

    def test_shallow_clone_and_git_worktree(self):
        with tempfile.TemporaryDirectory() as directory:
            shallow = Path(directory) / "shallow"
            subprocess.run(["git", "clone", "-q", "--depth", "1", self.root.as_uri(), str(shallow)], check=True)
            self.assertEqual(module.source_identity(shallow), {"revision": self.head, "state": "clean"})
            worktree = Path(directory) / "worktree"
            self.run_git("worktree", "add", "--detach", str(worktree), "HEAD")
            self.assertEqual(module.source_identity(worktree), {"revision": self.head, "state": "clean"})

    def test_modified_staged_and_untracked_files(self):
        file = self.root / "desktop/Package.swift"
        file.write_text("// changed\n")
        self.assertEqual(module.source_identity(self.root)["state"], "modified")
        self.run_git("add", ".")
        self.assertEqual(module.source_identity(self.root)["state"], "modified")
        self.run_git("reset", "--hard", "-q", "HEAD")
        (self.root / "new.swift").write_text("// untracked\n")
        self.assertEqual(module.source_identity(self.root)["state"], "modified")

    def test_ignored_outputs_do_not_dirty_source(self):
        self.destination.mkdir(parents=True)
        (self.destination / "output").write_text("build output")
        self.assertEqual(module.source_identity(self.root)["state"], "clean")

    def test_archive_and_parent_checkout_are_unknown(self):
        nested = self.root / "archive"
        nested.mkdir()
        self.assertEqual(module.source_identity(nested), {"revision": "unknown", "state": "unknown"})
        shutil.rmtree(self.root / ".git")
        self.assertEqual(module.source_identity(self.root), {"revision": "unknown", "state": "unknown"})

    def test_missing_git_or_failed_status_is_unknown(self):
        with patch.object(module, "git", side_effect=FileNotFoundError):
            self.assertEqual(module.source_identity(self.root)["revision"], "unknown")
        with patch.object(module, "git", side_effect=[str(self.root), self.head, subprocess.CalledProcessError(1, "git")]):
            self.assertEqual(module.source_identity(self.root), {"revision": "unknown", "state": "unknown"})

    def test_staging_refreshes_sources_preserves_cache_and_stamps_revision(self):
        module.stage(self.root, self.destination)
        stamp = json.loads((self.destination / "build-info.json").read_text())
        self.assertEqual(stamp["revision"], self.head)
        self.assertEqual(stamp["state"], "clean")
        self.assertRegex(stamp["builtAt"], r"^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ$")
        generated = self.destination / "Sources/TrackWebCore/BuildInfo.generated.swift"
        self.assertIn(f'revision: "{self.head}"', generated.read_text())
        (self.destination / "Sources/stale.swift").write_text("stale")
        (self.destination / ".build").mkdir()
        (self.destination / ".build/cache").write_text("keep")
        module.stage(self.root, self.destination)
        self.assertFalse((self.destination / "Sources/stale.swift").exists())
        self.assertEqual((self.destination / ".build/cache").read_text(), "keep")
        module.verify(self.root, self.destination)

    def test_verification_rejects_changed_head_or_state(self):
        module.stage(self.root, self.destination)
        (self.root / "desktop/Package.swift").write_text("changed")
        with self.assertRaises(SystemExit):
            module.verify(self.root, self.destination)
        self.run_git("add", ".")
        self.run_git("commit", "-qm", "next")
        with self.assertRaises(SystemExit):
            module.verify(self.root, self.destination)

    def test_stamped_source_remains_original_after_checkout_changes(self):
        module.stage(self.root, self.destination)
        generated = self.destination / "Sources/TrackWebCore/BuildInfo.generated.swift"
        before = generated.read_text()
        self.run_git("commit", "--allow-empty", "-qm", "next")
        self.assertEqual(generated.read_text(), before)
        module.stage(self.root, self.destination)
        self.assertIn(self.run_git("rev-parse", "HEAD"), generated.read_text())

    def test_executable_must_match_stamp(self):
        module.stage(self.root, self.destination)
        stamp = json.loads((self.destination / "build-info.json").read_text())
        expected = (f"Track for macOS\nCommit: {stamp['revision']}\n"
                    f"Source state: {stamp['state']}\nBuilt (UTC): {stamp['builtAt']}\n")
        with patch.object(module, "source_identity", return_value={"revision": self.head, "state": "clean"}), patch.object(module.subprocess, "check_output", return_value=expected) as run:
            module.verify(self.root, self.destination, Path("/fixture/Track"))
            run.assert_called_once_with(["/fixture/Track", "--version"], text=True, timeout=10)
        with patch.object(module, "source_identity", return_value={"revision": self.head, "state": "clean"}), patch.object(module.subprocess, "check_output", return_value="unknown"):
            with self.assertRaises(SystemExit):
                module.verify(self.root, self.destination, Path("/fixture/Track"))

    def test_cli_rejects_destination_outside_build(self):
        result = subprocess.run(["python3", str(ROOT / "scripts/desktop-build-info.py"), str(self.root), str(self.root / "desktop")], capture_output=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertTrue((self.root / "desktop/Package.swift").exists())


if __name__ == "__main__":
    unittest.main()
