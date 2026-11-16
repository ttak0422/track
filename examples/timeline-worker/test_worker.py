import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time
from types import SimpleNamespace
import unittest
from unittest.mock import patch

from worker import Client, Demo, WorkerError


class RetryTest(unittest.TestCase):
    def test_timeout_reuses_exact_request(self):
        client = Client("track", "/tmp/example", "demo", "worker")
        timeout = subprocess.TimeoutExpired("track", 15)
        success = subprocess.CompletedProcess([], 0, '{"revision":2}', '')
        with patch("worker.subprocess.run", side_effect=[timeout, success]) as run:
            client.apply({"revision": 1}, "project.update", {"title": "Demo"})
            self.assertEqual(run.call_args_list[0].kwargs["input"], run.call_args_list[1].kwargs["input"])

    def test_conflict_is_not_rebased(self):
        with patch("worker.subprocess.run", return_value=subprocess.CompletedProcess([], 1, '{"error":"timeline revision conflict"}', '')) as run:
            with self.assertRaises(WorkerError):
                Client("track", "/tmp/example", "demo", "worker").apply({"revision": 1}, "project.update", {"title": "Demo"})
            self.assertEqual(run.call_count, 1)

    def test_uncertain_timeout_does_not_refresh_or_rebase(self):
        with patch("worker.subprocess.run", side_effect=subprocess.TimeoutExpired("track", 15)) as run:
            with self.assertRaisesRegex(WorkerError, "uncertain"):
                Client("track", "/tmp/example", "demo", "worker").apply(
                    {"revision": 1}, "task.upsert", {"id": "task"})
            self.assertEqual(run.call_count, 2)
            self.assertEqual(run.call_args_list[0].kwargs["input"], run.call_args_list[1].kwargs["input"])

    def test_conflict_retry_is_bounded_and_uses_new_identity(self):
        snapshot = {"revision": 2, "tasks": [], "controls": []}
        conflict = subprocess.CompletedProcess([], 1, '{"error":"timeline revision conflict"}', '')
        read = subprocess.CompletedProcess([], 0, json.dumps(snapshot), '')
        responses = [conflict, read, conflict, read, conflict, read, conflict]
        with patch("worker.subprocess.run", side_effect=responses) as run:
            with self.assertRaisesRegex(WorkerError, "retry limit"):
                Client("track", "/tmp/example", "demo", "worker").apply(
                    {"revision": 1}, "task.upsert", {"id": "task"})
            requests = [json.loads(call.kwargs["input"]) for call in run.call_args_list
                        if call.kwargs["input"]]
            self.assertEqual(len({request["event_id"] for request in requests}), 4)
            self.assertEqual([request["expected_revision"] for request in requests], [1, 2, 2, 2])


@unittest.skipUnless(os.getenv("TRACK_TEST_BIN"), "Set TRACK_TEST_BIN to a built Track executable")
class IntegrationTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        root = Path(self.tmp.name)
        self.vault = root / "vault"
        self.vault.mkdir()
        self.env = patch.dict(os.environ, {"TRACK_CONFIG": str(root / "missing.yml"), "TRACK_CACHE_DIR": str(root / "cache"), "TRACK_VAULT": str(self.vault)})
        self.env.start()
        self.addCleanup(self.env.stop)
        self.binary = os.environ["TRACK_TEST_BIN"]
        subprocess.run([self.binary, "init"], check=True, capture_output=True, text=True)
        self.client = Client(self.binary, str(self.vault), "demo", "worker")
        self.client.apply({"revision": 0}, "project.create", {"title": "Demo"})

    def test_two_independent_worker_processes(self):
        script = str(Path(__file__).with_name("worker.py"))
        workers = []
        for identity in ("left", "right"):
            workers.append(subprocess.Popen([
                sys.executable, script, "--track", self.binary, "--vault", str(self.vault),
                "--project", "demo", "--task", identity, "--owner", identity,
                "--steps", "5", "--interval", "0.03",
            ], stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True))
        try:
            for process in workers:
                out, err = process.communicate(timeout=20)
                self.assertEqual(process.returncode, 0, err)
                self.assertEqual(json.loads(out)["project_id"], "demo")
        finally:
            for process in workers:
                if process.poll() is None:
                    process.kill()
                    process.communicate()
        snapshot = self.client.read()
        self.assertEqual({task["owner"] for task in snapshot["tasks"]}, {"left", "right"})
        self.assertTrue(all(task["status"] == "done" for task in snapshot["tasks"]))
        self.assertEqual(len(snapshot["reports"]), 2)

    def test_finite_run_and_report(self):
        snapshot = Demo(self.client, "task", 2, 0.01, 1).run()
        self.assertEqual(snapshot["tasks"][0]["status"], "done")
        self.assertEqual(snapshot["progress"]["fraction"], 1)
        report = snapshot["reports"][0]
        self.assertEqual(len(report["event_sequences"]), 3)
        self.assertTrue(all(seq < snapshot["revision"] for seq in report["event_sequences"]))
        with self.assertRaisesRegex(WorkerError, "new task ID"):
            Demo(self.client, "task", 2, 0.01, 1).run()

    def test_unrelated_task_conflict_reconciles_but_same_task_does_not(self):
        demo = Demo(self.client, "task", 2, 0.01, 1)
        stale = demo.task(self.client.read(), "active")
        fresh = self.client.apply(stale, "task.upsert", {
            "id": "unrelated", "title": "Other worker", "owner": "other", "status": "active"})
        updated = demo.task(stale, "active")
        self.assertEqual(updated["revision"], fresh["revision"] + 1)
        changed = dict(updated["tasks"][0], title="Concurrent edit")
        self.client.apply(updated, "task.upsert", changed)
        with self.assertRaisesRegex(WorkerError, "Affected task"):
            demo.task(updated, "done")
        self.assertEqual(self.client.read()["tasks"][0]["title"], "Concurrent edit")

    def test_control_change_blocks_stale_progress_and_receipts(self):
        demo = Demo(self.client, "task", 2, 0.01, 1)
        stale = demo.task(self.client.read(), "active")
        requested = self.client.apply(stale, "control.request", {
            "id": "pause", "task_id": "task", "owner": "worker", "action": "pause"})
        with self.assertRaisesRegex(WorkerError, "Affected task"):
            demo.task(stale, "done")
        self.client.apply(requested, "control.update", {"id": "pause", "status": "acknowledged"})
        with self.assertRaisesRegex(WorkerError, "Affected task"):
            self.client.apply(requested, "control.update", {"id": "pause", "status": "rejected"})
        self.assertEqual(self.client.read()["controls"][0]["status"], "acknowledged")
    def test_same_owner_edit_during_step_sleep_is_preserved(self):
        external = None
        def edit_during_sleep(_):
            nonlocal external
            snapshot = self.client.read()
            task = dict(snapshot["tasks"][0], status="blocked", done_minutes=5, remaining_minutes=20)
            external = self.client.apply(snapshot, "task.upsert", task)
        with patch("worker.time", SimpleNamespace(sleep=edit_during_sleep, monotonic=time.monotonic)):
            with self.assertRaisesRegex(WorkerError, "Task changed outside"):
                Demo(self.client, "task", 1, 0.01, 1).run()
        self.assertEqual(self.client.read(), external)
        self.assertEqual(external["reports"], [])

    def test_newer_task_in_identical_retry_receipt_is_not_adopted(self):
        demo = Demo(self.client, "task", 2, 0.01, 1)
        snapshot = demo.task(self.client.read(), "active")
        original_apply = self.client.apply
        def late_receipt(snapshot, kind, payload):
            own = original_apply(snapshot, kind, payload)
            own_id = self.client.last_event_id
            changed = dict(own["tasks"][0], status="blocked", done_minutes=5, remaining_minutes=20)
            newer = original_apply(own, "task.upsert", changed)
            self.client.last_event_id = own_id
            return newer
        with patch.object(self.client, "apply", side_effect=late_receipt):
            with self.assertRaisesRegex(WorkerError, "Task changed outside"):
                demo.task(snapshot, "active")
        self.assertEqual(demo.accepted_task["status"], "active")
        self.assertEqual(self.client.read()["tasks"][0]["status"], "blocked")

    def test_changed_task_is_not_acknowledged_by_controls(self):
        demo = Demo(self.client, "task", 2, 0.01, 1)
        snapshot = demo.task(self.client.read(), "active")
        snapshot = self.client.apply(snapshot, "control.request", {
            "id":"pause", "task_id":"task", "owner":"worker", "action":"pause"})
        snapshot = self.client.apply(snapshot, "task.upsert", dict(snapshot["tasks"][0], status="blocked"))
        with self.assertRaisesRegex(WorkerError, "Task changed outside"):
            demo.controls(snapshot)
        self.assertEqual(self.client.read(), snapshot)

    def test_pause_resume_at_safe_point_and_other_owner_ignored(self):
        demo = Demo(self.client, "task", 2, 0.01, 1)
        snapshot = demo.task(self.client.read(), "active")
        snapshot = self.client.apply(snapshot, "task.upsert", {"id":"other", "title":"Other", "owner":"other", "status":"active"})
        snapshot = self.client.apply(snapshot, "control.request", {"id":"other-pause", "task_id":"other", "owner":"other", "action":"pause"})
        snapshot = self.client.apply(snapshot, "control.request", {"id":"pause", "task_id":"task", "owner":"worker", "action":"pause"})
        snapshot = demo.controls(snapshot)
        self.assertTrue(demo.paused)
        self.assertEqual(demo.done, 0)
        self.assertEqual(snapshot["tasks"][0]["status"], "paused")
        self.assertEqual(snapshot["controls"][0]["status"], "requested")
        self.assertEqual(snapshot["controls"][1]["status"], "completed")
        snapshot = self.client.apply(snapshot, "control.request", {"id":"resume", "task_id":"task", "owner":"worker", "action":"resume"})
        snapshot = demo.controls(snapshot)
        self.assertFalse(demo.paused)
        self.assertEqual(snapshot["tasks"][0]["status"], "active")
        self.assertEqual(snapshot["controls"][-1]["status"], "completed")
        snapshot = self.client.apply(snapshot, "control.request", {"id":"interrupted", "task_id":"task", "owner":"worker", "action":"pause"})
        snapshot = self.client.apply(snapshot, "control.update", {"id":"interrupted", "status":"acknowledged"})
        with self.assertRaisesRegex(WorkerError, "manual reconciliation"):
            demo.controls(snapshot)


if __name__ == "__main__":
    unittest.main()
