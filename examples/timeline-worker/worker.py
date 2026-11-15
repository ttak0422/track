#!/usr/bin/env python3
"""Explicit, finite timeline worker demonstration; no dispatcher or arbitrary jobs."""
import argparse
import datetime
import json
import os
import subprocess
import time
import uuid


class WorkerError(Exception):
    pass


def now():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


class Client:
    def __init__(self, binary, vault, project, owner):
        self.argv = [binary, "timeline-project"]
        self.env = dict(os.environ, TRACK_VAULT=os.path.abspath(vault))
        self.project, self.owner = project, owner

    def call(self, action, payload=None):
        # Timeout retries use exactly the same serialized request and event identity.
        data = None if payload is None else json.dumps(payload)
        for attempt in range(2):
            try:
                result = subprocess.run(
                    self.argv + [action, "--id", self.project], input=data,
                    capture_output=True, text=True, env=self.env, timeout=15,
                    check=False,
                )
            except subprocess.TimeoutExpired:
                if attempt == 0:
                    continue
                raise WorkerError("Request outcome uncertain after identical retry; inspect the ledger before restarting")
            try:
                response = json.loads(result.stdout)
            except ValueError:
                raise WorkerError("Track returned invalid JSON; inspect the ledger before restarting")
            if result.returncode or not isinstance(response, dict) or response.get("error"):
                error = response.get("error") if isinstance(response, dict) else "invalid response"
                raise WorkerError(f"Track rejected request: {error}. No automatic rebase or restart.")
            return response

    def read(self):
        return self.call("get")

    def apply(self, snapshot, kind, payload):
        self.last_event_id = "demo-" + uuid.uuid4().hex
        return self.call("apply", {
            "source": self.owner, "event_id": self.last_event_id,
            "expected_revision": snapshot["revision"], "kind": kind, "payload": payload,
        })


class Demo:
    def __init__(self, client, task_id, steps, interval, pause_timeout):
        self.client, self.task_id = client, task_id
        self.steps, self.interval, self.pause_timeout = steps, interval, pause_timeout
        self.done, self.paused, self.paused_since = 0, False, None
        self.events = []

    def write(self, snapshot, kind, payload):
        updated = self.client.apply(snapshot, kind, payload)
        # A timeout retry can return a later snapshot: use the actual source event,
        # not the latest revision, as provenance for this worker's report.
        self.events.extend(event["sequence"] for event in updated["events"]
                           if event["event_id"] == self.client.last_event_id
                           and event["source"] == self.client.owner
                           and event["sequence"] not in self.events)
        return updated

    def task(self, snapshot, status):
        current = next((task for task in snapshot["tasks"] if task["id"] == self.task_id), None)
        if current is not None and current.get("owner") != self.client.owner:
            raise WorkerError("Task ownership changed; refusing to overwrite it")
        task = dict(current or {"id": self.task_id, "title": "Finite local demo",
                               "owner": self.client.owner, "weight": 1, "mode": "serial"})
        task.update(status=status, done_minutes=self.done * self.interval / 60,
                    remaining_minutes=(self.steps - self.done) * self.interval / 60)
        return self.write(snapshot, "task.upsert", task)

    def controls(self, snapshot):
        for control in list(snapshot["controls"]):
            if control["task_id"] != self.task_id or control["owner"] != self.client.owner:
                continue
            if control["status"] == "acknowledged":
                raise WorkerError("Existing acknowledged control needs manual reconciliation; not claiming completion")
            if control["status"] != "requested":
                continue
            snapshot = self.write(snapshot, "control.update", {"id": control["id"], "status": "acknowledged"})
            # We are between demo steps. Enter the actual local safe-point state
            # before recording task status and the completion receipt.
            self.paused = control["action"] == "pause"
            self.paused_since = time.monotonic() if self.paused else None
            snapshot = self.task(snapshot, "paused" if self.paused else "active")
            snapshot = self.write(snapshot, "control.update", {
                "id": control["id"], "status": "completed",
                "result": "Demo loop is paused at a safe point" if self.paused else "Demo loop is allowed to advance",
            })
        return snapshot

    def run(self):
        snapshot = self.client.read()
        if any(task["id"] == self.task_id for task in snapshot["tasks"]):
            raise WorkerError("Use a new task ID; this demo does not resume or take over existing tasks")
        started = now()
        snapshot = self.task(snapshot, "active")
        while self.done < self.steps:
            snapshot = self.controls(self.client.read())
            if self.paused:
                if time.monotonic() - self.paused_since >= self.pause_timeout:
                    raise WorkerError("Pause wait expired; task remains paused, no completion was claimed")
                time.sleep(min(self.interval, 0.25))
                continue
            # Deterministic local work: wait for one finite step. No commands,
            # network, model invocation or private data collection.
            time.sleep(self.interval)
            self.done += 1
            snapshot = self.task(self.client.read(), "done" if self.done == self.steps else "active")
        snapshot = self.write(snapshot, "report.add", {
            "id": "report-" + uuid.uuid4().hex, "task_id": self.task_id,
            "author": self.client.owner, "period_start": started, "period_end": now(),
            "summary": f"Completed {self.steps} local demonstration steps.",
            "event_sequences": self.events[:],
        })
        return snapshot


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--track", default="track")
    for option in ("vault", "project", "task", "owner"):
        parser.add_argument("--" + option, required=True)
    parser.add_argument("--steps", type=int, default=10)
    parser.add_argument("--interval", type=float, default=1)
    parser.add_argument("--pause-timeout", type=float, default=60)
    args = parser.parse_args()
    if not (1 <= args.steps <= 100 and 0.01 <= args.interval <= 10 and 1 <= args.pause_timeout <= 600):
        parser.error("steps must be 1..100, interval 0.01..10 seconds, pause-timeout 1..600 seconds")
    try:
        result = Demo(Client(args.track, args.vault, args.project, args.owner), args.task,
                      args.steps, args.interval, args.pause_timeout).run()
    except (WorkerError, OSError) as error:
        parser.exit(1, f"{error}\n")
    print(json.dumps({"project_id": result["project_id"], "revision": result["revision"], "task_id": args.task}))


if __name__ == "__main__":
    main()
