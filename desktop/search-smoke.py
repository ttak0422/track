#!/usr/bin/env python3
"""Exercise Track's native find menu in a real WKWebView without starting Go."""

import json
import os
import subprocess
import sys
import tempfile
from pathlib import Path


def main() -> None:
    if len(sys.argv) != 2:
        raise SystemExit("usage: search-smoke.py TRACK_EXECUTABLE")

    executable = Path(sys.argv[1]).resolve()
    if not executable.is_file():
        raise SystemExit(f"Track executable not found: {executable}")

    with tempfile.TemporaryDirectory(prefix="track-web-search-smoke-") as temp:
        result_file = Path(temp) / "result.json"
        environment = {key: value for key, value in os.environ.items() if not key.startswith("TRACK_")}
        environment["TRACK_WEB_SMOKE_RESULT"] = str(result_file)
        try:
            completed = subprocess.run(
                [str(executable), "--search-test"],
                env=environment,
                text=True,
                capture_output=True,
                timeout=30,
                check=False,
            )
        except subprocess.TimeoutExpired as error:
            for stream, output in ((sys.stdout, error.stdout), (sys.stderr, error.stderr)):
                if output:
                    if isinstance(output, bytes):
                        output = output.decode("utf-8", errors="replace")
                    print(output, end="", file=stream)
            raise SystemExit(f"WKWebView search smoke timed out after 30 seconds: {error}") from error

        if completed.stdout:
            print(completed.stdout, end="")
        if completed.stderr:
            print(completed.stderr, end="", file=sys.stderr)
        if completed.returncode != 0:
            raise SystemExit(f"Track search smoke app exited with status {completed.returncode}")
        if not result_file.is_file():
            raise SystemExit("Track exited without writing the WKWebView search smoke result")

        report = json.loads(result_file.read_text(encoding="utf-8"))
        find = report.get("find", {})
        window = report.get("window", {})
        checks = {
            name: find.get(key) is True
            for key, name in {
                "menuConfigured": "Edit > Find contains the native actions and shortcuts",
                "commandFDispatched": "Cmd+F dispatches",
                "nativeFindUIVisible": "Cmd+F opens a visible native find bar",
                "queryEnteredThroughField": "typing in the real find field selects the first result",
                "queryMatched": "the typed English query matches",
                "selectionTextMatches": "the selected page text matches the query",
                "visibleHighlight": "the selected text is visibly highlighted while the field has focus",
                "nextShortcutDispatched": "Cmd+G dispatches Find Next",
                "nextMoved": "Cmd+G moves to a different result",
                "previousShortcutDispatched": "Shift+Cmd+G dispatches Find Previous",
                "previousMoved": "Shift+Cmd+G moves back to the first result",
                "nextWrapped": "Find Next wraps from last to first",
                "previousWrapped": "Find Previous wraps from first to last",
                "incrementalQueryMatched": "additional typing updates the page match without Return",
                "newestQueryWins": "stale callbacks cannot overwrite a newer query",
                "japaneseMatched": "typing Japanese selects a Japanese result",
                "japaneseNextMoved": "Find Next moves between Japanese results",
                "japanesePreviousMoved": "Find Previous moves between Japanese results",
                "noMatchFeedback": "a missing query displays No matches",
                "noMatchClearsSelection": "a missing query clears the old selection",
                "clearResetsFeedback": "clearing the field resets match feedback",
                "escapeCloses": "Escape closes the find bar",
                "reopenPreservesQuery": "reopening preserves the query and focuses the field",
                "reopenFinds": "reopening restores a real page match",
            }.items()
        }
        checks["native window tabs are disabled"] = window.get("tabbingDisabled") is True
        checks["the titled window and titlebar remain enabled"] = window.get("titlebarPreserved") is True
        failures = [name for name, passed in checks.items() if not passed]
        if report.get("ok") is not True or failures:
            details = "; ".join(f"FAIL {name}" for name in failures)
            raise SystemExit(f"WKWebView search smoke failed: {details}; report={json.dumps(report, ensure_ascii=False)}")

        print("WKWebView search smoke passed: typed English/Japanese queries, painted selection, next/previous movement and wrapping, no-match, clear, Escape/reopen, and window policy verified.")


if __name__ == "__main__":
    main()
