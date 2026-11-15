import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

from adapter import Client, Importer, ImportError


# Synthetic fixtures following the official exec JSONL schema, not real sessions.
def stream(events):
    return io.StringIO("".join(json.dumps(e) + "\n" for e in events))


def fixture():
    return [
        {"type": "thread.started", "thread_id": "private-thread-id"},
        {"type": "turn.started"},
        {"type": "item.started", "item": {"id": "item_0", "type": "todo_list", "items": [
            {"text": "PRIVATE_PLAN_TEXT", "completed": False}]}},
        {"type": "item.completed", "item": {"id": "item_1", "type": "agent_message", "text": "PRIVATE_MESSAGE"}},
        {"type": "item.completed", "item": {"id": "item_2", "type": "reasoning", "text": "PRIVATE_REASONING"}},
        {"type": "item.completed", "item": {"id": "item_3", "type": "command_execution", "command": "PRIVATE_COMMAND",
            "aggregated_output": "PRIVATE_OUTPUT", "exit_code": 0, "status": "completed"}},
        {"type": "item.completed", "item": {"id": "item_4", "type": "file_change", "changes": [{"path": "PRIVATE_PATH", "kind": "update"}], "status": "completed"}},
        {"type": "item.completed", "item": {"id": "item_5", "type": "mcp_tool_call", "server": "PRIVATE_SERVER", "tool": "PRIVATE_TOOL", "arguments": {"secret": "PRIVATE_ARGUMENT"}, "result": {"content": ["PRIVATE_RESULT"]}, "status": "completed"}},
        {"type": "item.updated", "item": {"id": "item_0", "type": "todo_list", "items": [
            {"text": "PRIVATE_PLAN_TEXT", "completed": True}]}},
        {"type": "item.completed", "item": {"id": "item_0", "type": "todo_list", "items": [
            {"text": "PRIVATE_PLAN_TEXT", "completed": True}]}},
        {"type": "turn.completed", "usage": {"input_tokens": 10, "cached_input_tokens": 0, "output_tokens": 5, "reasoning_output_tokens": 0}},
    ]


class UnitTests(unittest.TestCase):
    def test_uncertain_write_identical_retry(self):
        c = Client("track", "/tmp/demo", "p", "owner")
        with patch("adapter.subprocess.run", side_effect=subprocess.TimeoutExpired("track", 15)) as run:
            with self.assertRaises(ImportError):
                c.apply({"source": "owner", "event_id": "fixed", "payload": {}})
            self.assertEqual(run.call_count, 2)
            self.assertEqual(run.call_args_list[0].kwargs["input"], run.call_args_list[1].kwargs["input"])


@unittest.skipUnless(os.getenv("TRACK_TEST_BIN"), "Set TRACK_TEST_BIN for real CLI integration tests")
class IntegrationTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        root = Path(self.temp.name)
        self.vault = root / "vault"
        self.vault.mkdir()
        env = patch.dict(os.environ, TRACK_CONFIG=str(root / "missing.yml"),
                         TRACK_CACHE_DIR=str(root / "cache"), TRACK_VAULT=str(self.vault))
        env.start()
        self.addCleanup(env.stop)
        self.binary = os.environ["TRACK_TEST_BIN"]
        subprocess.run([self.binary, "init"], check=True, capture_output=True)
        self.client = Client(self.binary, str(self.vault), "demo", "codex-import")
        self.client.apply({"source": "human", "event_id": "create", "expected_revision": 0,
                           "kind": "project.create", "payload": {"title": "Demo"}})

    def run_fixture(self, events=None, run="test-run"):
        importer = Importer(self.client, run)
        importer.run(stream(fixture() if events is None else events))
        return importer

    def test_real_protocol_privacy_counts_and_exact_replay(self):
        importer = self.run_fixture()
        first = self.client.read()
        self.assertEqual([t["status"] for t in first["tasks"]], ["done", "done"])
        turn = first["tasks"][1]
        self.assertIn("1/1 completed", turn["detail"])
        self.assertIn('"command_execution:completed": 1', turn["detail"])
        self.assertEqual(turn["done_minutes"], 0)
        self.assertEqual(len(first["reports"]), 1)
        report = first["reports"][0]
        refs = [e for e in first["events"] if e["sequence"] in report["event_sequences"]]
        self.assertEqual(report["period_start"], refs[0]["at"])
        self.assertEqual(report["period_end"], refs[-1]["at"])
        self.assertEqual(len(first["tasks"]), 2)  # No invented stable plan-step tasks.
        encoded = json.dumps(first)
        self.assertNotIn("PRIVATE_", encoded)
        self.assertNotIn("private-thread-id", encoded)
        self.assertNotIn("item_", encoded)
        self.run_fixture()
        self.assertEqual(self.client.read(), first)
        self.assertTrue(importer.prefix.startswith("codex-"))

    def test_interrupted_eof_and_failure_do_not_claim_success(self):
        with self.assertRaises(ImportError):
            self.run_fixture(fixture()[:3])
        state = self.client.read()
        self.assertTrue(all(t["status"] == "blocked" for t in state["tasks"]))
        events = fixture()[:2] + [{"type": "turn.failed", "error": {"message": "PRIVATE_ERROR"}}]
        with self.assertRaises(ImportError):
            self.run_fixture(events, run="failed-run")
        state = self.client.read()
        self.assertNotIn("PRIVATE_ERROR", json.dumps(state))
        self.assertFalse(state["controls"])

    def test_conflicting_replay_is_refused(self):
        self.run_fixture()
        events = fixture()
        events[2]["item"]["items"][0]["completed"] = True
        before = self.client.read()
        with self.assertRaises(ImportError):
            self.run_fixture(events)
        self.assertEqual(self.client.read(), before)

    def test_external_edit_is_not_overwritten(self):
        importer = Importer(self.client, "test-run")
        for number, event in enumerate(fixture()[:2], 1):
            importer.line = number
            importer.consume(event)
        snapshot = self.client.read()
        task = dict(snapshot["tasks"][1], title="Human change")
        self.client.apply({"source": "human", "event_id": "edit", "expected_revision": snapshot["revision"],
                           "kind": "task.upsert", "payload": task})
        restarted = Importer(self.client, "test-run")
        with self.assertRaises(ImportError):
            restarted.run(stream(fixture()))
        self.assertEqual(self.client.read()["tasks"][1]["title"], "Human change")

    def test_multi_turn_and_unknown_items(self):
        events = fixture() + [{"type": "turn.started"},
            {"type": "item.completed", "item": {"type": "future_type", "secret": "PRIVATE_FUTURE"}},
            {"type": "turn.completed", "usage": {}}]
        self.run_fixture(events)
        result = self.client.read()
        self.assertEqual(len(result["tasks"]), 3)
        self.assertNotIn("PRIVATE_FUTURE", json.dumps(result))

    def test_repeated_item_completion_counted_once(self):
        events = fixture()
        events.insert(6, dict(events[5]))
        self.run_fixture(events)
        self.assertIn('"command_execution:completed": 1', self.client.read()["tasks"][1]["detail"])
        events[6] = {"type": "item.completed", "item": {"id": "item_3", "type": "command_execution", "status": "failed"}}
        with self.assertRaises(ImportError):
            self.run_fixture(events, "different-run")

    def test_changed_terminal_replay_rejected(self):
        self.run_fixture()
        before = self.client.read()
        events = fixture()
        events[-1] = {"type": "turn.failed", "error": {"message": "PRIVATE_ERROR"}}
        with self.assertRaises(ImportError):
            self.run_fixture(events)
        self.assertEqual(self.client.read(), before)

    def test_truncated_replay_cannot_complete_later_turn(self):
        events = fixture() + [{"type": "future_event"}, {"type": "turn.started"}]
        with self.assertRaises(ImportError):
            self.run_fixture(events)
        before = self.client.read()
        with self.assertRaises(ImportError):
            self.run_fixture(fixture())
        self.assertEqual(self.client.read(), before)

    def test_warning_before_turn_and_declined_command(self):
        warning = {"type": "item.completed", "item": {"id": "warning", "type": "error", "message": "PRIVATE_WARNING"}}
        declined = {"type": "item.completed", "item": {"id": "command", "type": "command_execution", "status": "declined", "command": "PRIVATE_COMMAND"}}
        events = [fixture()[0], warning, fixture()[1], declined, fixture()[-1]]
        self.run_fixture(events)
        first = self.client.read()
        self.assertTrue(all(task["status"] == "done" for task in first["tasks"]))
        self.assertIn('"command_execution:declined": 1', first["tasks"][1]["detail"])
        self.assertNotIn("PRIVATE_", json.dumps(first))
        self.assertFalse(first["controls"])
        self.run_fixture(events)
        self.assertEqual(self.client.read(), first)

    def test_declined_is_not_accepted_for_file_or_mcp(self):
        for kind in ("file_change", "mcp_tool_call"):
            events = fixture()[:2] + [{"type": "item.completed", "item": {"id": "bad", "type": kind, "status": "declined"}}]
            with self.assertRaisesRegex(ImportError, "Unsupported typed item status"):
                self.run_fixture(events, run=kind)

    def test_allowlisted_item_before_turn_still_rejected(self):
        events = [fixture()[0], {"type": "item.completed", "item": {"id": "command", "type": "command_execution", "status": "completed"}}]
        with self.assertRaisesRegex(ImportError, "outside an active turn"):
            self.run_fixture(events)

    def test_conflict_retry_bound_then_exact_recovery(self):
        original = self.client.apply
        attempts = 0
        def competing_write(request):
            nonlocal attempts
            # Allow the two initial run records, then force every bounded attempt
            # for the first turn record to conflict with an unrelated task writer.
            if request["event_id"].endswith("-turn"):
                attempts += 1
                snapshot = self.client.read()
                original({"source": "other", "event_id": "race-" + str(attempts),
                          "expected_revision": snapshot["revision"], "kind": "task.upsert",
                          "payload": {"id": "other-task", "title": "Unrelated", "status": "active"}})
            return original(request)
        with patch.object(self.client, "apply", side_effect=competing_write):
            with self.assertRaisesRegex(ImportError, "Conflict retry bound reached"):
                self.run_fixture()
        self.assertEqual(attempts, 4)
        self.run_fixture()
        recovered = self.client.read()
        imported = [task for task in recovered["tasks"] if task["id"] != "other-task"]
        self.assertEqual(len(imported), 2)
        self.assertTrue(all(task["status"] == "done" for task in imported))
        self.assertEqual(len(recovered["reports"]), 1)
        self.run_fixture()
        self.assertEqual(self.client.read(), recovered)

    def test_two_distinct_importers_concurrently(self):
        processes = []
        for run_id in ("left", "right"):
            process = subprocess.Popen([sys.executable, str(Path(__file__).with_name("adapter.py")),
                "--track", self.binary, "--vault", str(self.vault), "--project", "demo",
                "--owner", run_id, "--run-id", run_id], stdin=subprocess.PIPE,
                stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
            processes.append(process)
        try:
            for process in processes:
                process.stdin.write(stream(fixture()).getvalue())
                process.stdin.close()
                process.stdin = None
            failed = []
            for process in processes:
                out, err = process.communicate(timeout=20)
                if process.returncode:
                    self.assertEqual(process.returncode, 1, err)
                    self.assertIn("Import stopped", err)
                    failed.append(process.args)
            # Bounded contention may stop an importer. Once competing writers
            # finish, recover by replaying the exact same stream and run identity.
            for args in failed:
                replay = subprocess.run(args, input=stream(fixture()).getvalue(),
                                        capture_output=True, text=True, timeout=20)
                self.assertEqual(replay.returncode, 0, replay.stderr)
        finally:
            for process in processes:
                if process.poll() is None:
                    process.kill()
                    process.communicate()
        snapshot = self.client.read()
        self.assertEqual(len(snapshot["tasks"]), 4)
        self.assertEqual(len(snapshot["reports"]), 2)
        self.assertTrue(all(task["status"] == "done" for task in snapshot["tasks"]))

    def test_invalid_plan_or_large_record_stops_without_copying(self):
        events = fixture()[:2] + [{"type": "item.updated", "item": {"id": "item_0", "type": "todo_list", "items": [{"text": "PRIVATE", "completed": "yes"}]}}]
        with self.assertRaises(ImportError):
            self.run_fixture(events)
        self.assertNotIn("PRIVATE", json.dumps(self.client.read()))
        with self.assertRaises(ImportError):
            Importer(self.client, "other-run").run(io.StringIO("X" * 1_048_577))


if __name__ == "__main__":
    unittest.main()
