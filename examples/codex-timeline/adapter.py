#!/usr/bin/env python3
"""Import explicitly piped Codex lifecycle metadata, never launch or control Codex."""
import argparse
import hashlib
import json
import os
import subprocess
import sys
import time


class ImportError(Exception):
    pass


class RevisionConflict(ImportError):
    pass


class Client:
    def __init__(self, binary, vault, project, owner):
        self.argv = [binary, "timeline-project"]
        self.env = dict(os.environ, TRACK_VAULT=os.path.abspath(vault))
        self.project, self.owner = project, owner

    def call(self, operation, request=None):
        encoded = None if request is None else json.dumps(request, separators=(",", ":"))
        for attempt in range(2):
            try:
                result = subprocess.run(self.argv + [operation, "--id", self.project],
                                        input=encoded, text=True, capture_output=True,
                                        timeout=15, env=self.env, check=False)
            except subprocess.TimeoutExpired:
                if attempt == 0:
                    continue
                raise ImportError("Uncertain Track outcome; replay the same stream and run ID")
            try:
                response = json.loads(result.stdout)
            except ValueError:
                raise ImportError("Invalid Track response; inspect the ledger")
            if result.returncode or not isinstance(response, dict) or response.get("error"):
                if result.returncode and isinstance(response, dict) and response.get("error") in (
                        "timeline revision conflict", "apply timeline event: timeline revision conflict"):
                    raise RevisionConflict("Revision conflict")
                # Do not print remote error text or input contents.
                raise ImportError("Track rejected the record; inspect revision/identity conflicts")
            return response

    def read(self):
        return self.call("get")

    def apply(self, request):
        return self.call("apply", request)


class Importer:
    def __init__(self, client, run_id):
        self.client = client
        self.prefix = "codex-" + hashlib.sha256(run_id.encode()).hexdigest()[:24]
        self.snapshot = client.read()
        self.line, self.turn, self.active = 0, 0, False
        self.started, self.terminal, self.failed = False, False, False
        self.counts = {}
        self.completed_items = {}
        self.plan = None

    def write(self, suffix, task):
        self.record(suffix, "task.upsert", task)

    def record(self, suffix, kind, payload):
        event_id = self.prefix + "-" + str(self.line) + "-" + suffix
        original = self.snapshot

        def preconditions(snapshot):
            task_id = payload["id"] if kind == "task.upsert" else payload["task_id"]
            task = next((t for t in snapshot["tasks"] if t["id"] == task_id), None)
            parent_id = payload.get("parent_id") if kind == "task.upsert" else (task or {}).get("parent_id")
            parent = next((t for t in snapshot["tasks"] if t["id"] == parent_id), None)
            controls = [c for c in snapshot["controls"] if c["task_id"] == task_id]
            report = next((r for r in snapshot["reports"] if r["id"] == payload["id"]), None) if kind == "report.add" else None
            refs = [e for e in snapshot["events"] if e["sequence"] in payload.get("event_sequences", [])]
            latest = next((e for e in reversed(snapshot["events"])
                           if e["kind"] == "task.upsert" and e["payload"]["id"] == task_id), None)
            return task, parent, controls, report, refs, latest

        for attempt in range(4):
            old = next((e for e in self.snapshot["events"]
                        if e["source"] == self.client.owner and e["event_id"] == event_id), None)
            if old is None:
                for event in self.snapshot["events"]:
                    if event["source"] != self.client.owner or not event["event_id"].startswith(self.prefix + "-"):
                        continue
                    tail = event["event_id"][len(self.prefix) + 1:].split("-", 1)[0]
                    if tail.isdecimal() and int(tail) > self.line:
                        raise ImportError("Earlier stream prefix cannot append over a later import")
            current, _, _, _, _, latest = preconditions(self.snapshot)
            if old is None and current:
                if (not latest or latest["source"] != self.client.owner
                        or not latest["event_id"].startswith(self.prefix + "-")
                        or current.get("owner") != self.client.owner):
                    raise ImportError("Target was edited outside this run; refusing to overwrite it")
            request = {"source": self.client.owner, "event_id": event_id,
                       "expected_revision": self.snapshot["revision"], "kind": kind, "payload": payload}
            try:
                self.snapshot = self.client.apply(request)
                return
            except RevisionConflict:
                if attempt == 3:
                    raise ImportError("Conflict retry bound reached")
                time.sleep(0.05 * (2 ** attempt))
                fresh = self.client.read()
                if preconditions(original) != preconditions(fresh):
                    raise ImportError("Affected records changed; refusing to rebase")
                # A definitive conflict committed nothing. The logical action and
                # event identity remain unchanged; only its checked revision moves.
                self.snapshot = fresh

    def report(self, status):
        identity = self.prefix + "-turn-" + str(self.turn)
        terminal_id = self.prefix + "-" + str(self.line) + "-turn"
        terminal = next(e for e in self.snapshot["events"]
                        if e["source"] == self.client.owner and e["event_id"] == terminal_id)
        events = [e for e in self.snapshot["events"] if e["source"] == self.client.owner
                  and e["kind"] == "task.upsert" and e["payload"]["id"] == identity
                  and e["event_id"].startswith(self.prefix + "-")
                  and e["sequence"] <= terminal["sequence"]]
        # Immutable ledger times and references make exact replay deterministic.
        self.record("report", "report.add", {
            "id": identity + "-report", "task_id": identity, "author": self.client.owner,
            "period_start": events[0]["at"], "period_end": terminal["at"],
            "summary": "Observed Codex turn " + status + ". " + terminal["payload"]["detail"],
            "uncertainty": "Lifecycle metadata only; not verification of the requested work.",
            "event_sequences": [e["sequence"] for e in events],
        })

    def task(self, identity, title, status, detail, parent=None):
        result = {"id": identity, "title": title, "status": status,
                  "owner": self.client.owner, "weight": 1, "mode": "serial",
                  "done_minutes": 0, "remaining_minutes": 0, "detail": detail}
        if parent:
            result["parent_id"] = parent
        return result

    def root(self, status):
        self.write("run", self.task(self.prefix, "Codex execution", status,
                   "Explicit JSONL import; observed lifecycle only, not process control."))

    def turn_task(self, status):
        detail = "Observed item counts: " + json.dumps(self.counts, sort_keys=True)
        if self.plan is not None:
            detail += "; latest plan: " + str(self.plan[0]) + "/" + str(self.plan[1]) + " completed"
        self.write("turn", self.task(self.prefix + "-turn-" + str(self.turn),
                   "Codex turn " + str(self.turn), status, detail, self.prefix))

    def consume(self, event):
        if not isinstance(event, dict) or not isinstance(event.get("type"), str):
            raise ImportError("Expected an event object with a type")
        kind = event["type"]
        if kind == "thread.started":
            if self.started or self.turn:
                raise ImportError("Only one invocation stream is accepted per run ID")
            if not isinstance(event.get("thread_id"), str) or not event["thread_id"]:
                raise ImportError("Invalid thread lifecycle record")
            self.started = True
            self.root("active")
        elif kind == "turn.started":
            if not self.started or self.active:
                raise ImportError("Unexpected turn start")
            self.turn += 1
            self.active, self.terminal = True, False
            self.counts, self.plan, self.completed_items = {}, None, {}
            self.root("active")
            self.turn_task("active")
        elif kind in ("turn.completed", "turn.failed"):
            if not self.active:
                raise ImportError("Unexpected turn terminal record")
            self.active, self.terminal = False, True
            self.failed = self.failed or kind == "turn.failed"
            self.turn_task("done" if kind == "turn.completed" else "blocked")
            self.report("completed" if kind == "turn.completed" else "failed")
        elif kind == "error":
            # Never persist upstream error messages, which can include private data.
            self.failed = True
            if self.active:
                self.turn_task("blocked")
            if self.started:
                self.root("blocked")
        elif kind in ("item.started", "item.updated", "item.completed"):
            if not self.active:
                raise ImportError("Item outside an active turn")
            item = event.get("item")
            if not isinstance(item, dict) or not isinstance(item.get("type"), str):
                raise ImportError("Invalid item envelope")
            item_kind = item["type"]
            if item_kind == "todo_list":
                items = item.get("items")
                if (not isinstance(item.get("id"), str) or not isinstance(items, list)
                        or len(items) > 1000 or any(not isinstance(i, dict)
                        or type(i.get("completed")) is not bool for i in items)):
                    raise ImportError("Invalid plan schema")
                self.plan = (sum(i["completed"] for i in items), len(items))
                self.turn_task("active")
            elif item_kind in ("command_execution", "file_change", "mcp_tool_call"):
                # Fixed type/status labels only; no command, path, tool name or output.
                status = item.get("status")
                if status not in ("in_progress", "completed", "failed"):
                    raise ImportError("Unsupported typed item status")
                if kind == "item.completed":
                    item_id = item.get("id")
                    if not isinstance(item_id, str) or not item_id or len(item_id) > 256:
                        raise ImportError("Invalid completed item identity")
                    signature = (item_kind, status)
                    if item_id in self.completed_items:
                        if self.completed_items[item_id] != signature:
                            raise ImportError("Contradictory repeated item completion")
                        return
                    self.completed_items[item_id] = signature
                    key = item_kind + ":" + status
                    self.counts[key] = self.counts.get(key, 0) + 1
                    self.turn_task("active")
            # Messages, reasoning, searches, collab payloads and unknown items
            # are deliberately ignored, never printed or copied to Track.
        # Unknown top-level events are ignored for additive protocol evolution.

    def finish(self):
        if not self.started:
            raise ImportError("No supported lifecycle observed")
        self.line += 1
        if self.active:
            self.turn_task("blocked")
        status = "done" if self.terminal and not self.failed and not self.active else "blocked"
        self.root(status)
        if status != "done":
            raise ImportError("Stream incomplete or failed; no successful completion claimed")

    def run(self, stream):
        total_bytes = 0
        while True:
            raw = stream.readline(1_048_577)
            if not raw:
                break
            self.line += 1
            line_bytes = len(raw.encode("utf-8"))
            total_bytes += line_bytes
            if self.line > 10_000 or line_bytes > 1_048_576 or total_bytes > 16_777_216:
                raise ImportError("Input exceeds bounded stream limits")
            try:
                event = json.loads(raw)
            except ValueError:
                raise ImportError("Invalid JSONL record at line " + str(self.line))
            self.consume(event)
        self.finish()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--track", default="track")
    for option in ("vault", "project", "owner", "run-id"):
        parser.add_argument("--" + option, required=True)
    args = parser.parse_args()
    if not args.run_id or len(args.run_id) > 256:
        parser.error("run-id must contain 1..256 characters")
    try:
        Importer(Client(args.track, args.vault, args.project, args.owner), args.run_id).run(sys.stdin)
    except (ImportError, OSError, UnicodeError):
        # Input and subprocess diagnostics are untrusted and may contain secrets.
        parser.exit(1, "Import stopped; inspect Track state. Replay only the same stream/run ID after resolving the issue.\n")
    print("Codex lifecycle metadata imported")


if __name__ == "__main__":
    main()
