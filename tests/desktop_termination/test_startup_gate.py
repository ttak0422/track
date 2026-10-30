import importlib.util
import os
import tempfile
import threading
import unittest
from pathlib import Path
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location("termination_driver", ROOT / "scripts/desktop-clean-termination.py")
driver = importlib.util.module_from_spec(spec)
spec.loader.exec_module(driver)


class StartupGateTests(unittest.TestCase):
    def test_release_already_present_needs_no_delay(self):
        with tempfile.TemporaryDirectory() as directory:
            release = Path(directory) / "gate.release"
            release.touch()
            driver.wait_for_startup_release(release, 0)

    def test_missing_release_expires_with_diagnostic(self):
        with tempfile.TemporaryDirectory() as directory:
            with patch.object(driver.time, "monotonic", side_effect=[0.0, 0.0, 0.04]), \
                    patch.object(driver.time, "sleep"):
                with self.assertRaisesRegex(TimeoutError, "not released before its deadline"):
                    driver.wait_for_startup_release(Path(directory) / "missing", 0.03)

    def test_unreleased_child_records_pid_without_exec(self):
        with tempfile.TemporaryDirectory() as directory:
            gate = Path(directory) / "gate"
            with patch.dict(os.environ, {"TRACK_TERMINATION_STARTUP_GATE": str(gate)}), \
                    patch.object(driver, "wait_for_startup_release", side_effect=TimeoutError("held")), \
                    patch.object(driver.os, "execv") as execute:
                with self.assertRaisesRegex(TimeoutError, "held"):
                    driver.startup_gate(Path("/fixture/track"), ["web", "--addr", "127.0.0.1:12345"])
                execute.assert_not_called()
                self.assertEqual(gate.read_text(), str(os.getpid()))

    def test_regular_scenarios_exec_same_arguments_without_gate(self):
        with patch.dict(os.environ, {}, clear=True), patch.object(driver.os, "execv") as execute:
            driver.startup_gate(Path("/fixture/track"), ["web", "--addr", "127.0.0.1:12345"])
            execute.assert_called_once_with("/fixture/track", ["/fixture/track", "web", "--addr", "127.0.0.1:12345"])

    def test_released_gate_execs_with_original_arguments(self):
        with tempfile.TemporaryDirectory() as directory:
            gate = Path(directory) / "gate"
            gate.with_suffix(".release").touch()
            with patch.dict(os.environ, {"TRACK_TERMINATION_STARTUP_GATE": str(gate)}), \
                    patch.object(driver.os, "execv") as execute:
                driver.startup_gate(Path("/fixture/track"), ["web"])
                self.assertEqual(gate.read_text(), str(os.getpid()))
                execute.assert_called_once_with("/fixture/track", ["/fixture/track", "web"])

    def test_partial_receipt_is_not_published(self):
        with tempfile.TemporaryDirectory() as directory:
            gate = Path(directory) / "gate"
            gate.with_suffix(".release").touch()
            writing, release_write = threading.Event(), threading.Event()
            errors = []

            def partial_write(path, value, *args, **kwargs):
                with path.open("w") as stream:
                    stream.write(value[:1])
                    stream.flush()
                    writing.set()
                    if not release_write.wait(5):
                        raise TimeoutError("test did not release receipt write")
                    stream.write(value[1:])

            def child():
                try:
                    driver.startup_gate(Path("/fixture/track"), ["web"])
                except Exception as error:
                    errors.append(error)

            with patch.dict(os.environ, {"TRACK_TERMINATION_STARTUP_GATE": str(gate)}), \
                    patch.object(Path, "write_text", partial_write), patch.object(driver.os, "execv"):
                thread = threading.Thread(target=child)
                thread.start()
                try:
                    self.assertTrue(writing.wait(5), "child did not start writing receipt")
                    self.assertFalse(gate.exists(), "reader could observe a partial PID")
                finally:
                    release_write.set()
                    thread.join(5)
                self.assertFalse(thread.is_alive())
                self.assertEqual(errors, [])
                self.assertEqual(gate.read_text(), str(os.getpid()))
