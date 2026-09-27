#!/usr/bin/env python3
"""Run the bundled app against a disposable vault and inspect real WKWebView results."""

import errno
import json
import os
import socket
import subprocess
import sys
import tempfile
import time
from pathlib import Path


def main() -> None:
    if len(sys.argv) not in (2, 3) or (len(sys.argv) == 3 and sys.argv[2] != "--recovery-test"):
        raise SystemExit("usage: desktop-smoke.py TRACK_WEB_EXECUTABLE [--recovery-test]")
    executable = Path(sys.argv[1]).resolve()
    recovery_test = len(sys.argv) == 3

    with tempfile.TemporaryDirectory(prefix="track-web-smoke-") as temp:
        root = Path(temp)
        vault = root / "vault"
        cache = root / "cache"
        vault.mkdir()
        cache.mkdir()
        config = root / "machine.yml"
        result_file = root / "result.json"
        config.write_text(
            f"vault_dir: {json.dumps(str(vault))}\ncache_dir: {json.dumps(str(cache))}\n",
            encoding="utf-8",
        )

        # Drop inherited TRACK_* variables so user registries, paths, or test overrides cannot leak
        # into this fixture. The three inputs the app consumes are all rooted in this temp directory.
        environment = {key: value for key, value in os.environ.items() if not key.startswith("TRACK_")}
        environment.update(
            {
                "TRACK_CONFIG": str(config),
                "TRACK_VAULT": str(vault),
                "TRACK_CACHE_DIR": str(cache),
                "TRACK_WEB_SMOKE_RESULT": str(result_file),
            }
        )
        try:
            completed = subprocess.run(
                [str(executable), "--recovery-test" if recovery_test else "--smoke-test"],
                env=environment,
                text=True,
                capture_output=True,
                timeout=60,
                check=False,
            )
        except subprocess.TimeoutExpired as error:
            raise SystemExit(f"WKWebView smoke timed out after 60 seconds: {error}") from error

        if completed.stdout:
            print(completed.stdout, end="")
        if completed.stderr:
            print(completed.stderr, end="", file=sys.stderr)
        if completed.returncode != 0:
            raise SystemExit(f"Track Web smoke app exited with status {completed.returncode}")
        if not result_file.is_file():
            raise SystemExit("Track Web exited without writing the WKWebView smoke result")

        report = json.loads(result_file.read_text(encoding="utf-8"))
        if report.get("ok") is not True:
            raise SystemExit(f"WKWebView smoke failed: {json.dumps(report, ensure_ascii=False)}")
        if report.get("apiStatus") != 200:
            raise SystemExit(f"live API did not return 200: {json.dumps(report, ensure_ascii=False)}")
        if int(report.get("rootChildren", 0)) < 1:
            raise SystemExit(f"live UI did not render into #root: {json.dumps(report, ensure_ascii=False)}")
        if recovery_test:
            popup = report.get("targetBlank", {})
            if popup.get("ok") is not True:
                raise SystemExit(f"WKWebView target=_blank policy did not pass: {json.dumps(report, ensure_ascii=False)}")
            recovery = report.get("recovery", {})
            if recovery.get("ok") is not True or recovery.get("sameDocument") is not True:
                raise SystemExit(f"WKWebView document did not survive server retry: {json.dumps(report, ensure_ascii=False)}")
            if not str(recovery.get("draftValue", "")).startswith("unsaved draft "):
                raise SystemExit(f"unsaved input was lost during server retry: {json.dumps(report, ensure_ascii=False)}")
        active_path = report.get("activePath")
        if not active_path or not os.path.samefile(active_path, vault):
            raise SystemExit(f"smoke app did not use its isolated vault: {json.dumps(report, ensure_ascii=False)}")

        deadline = time.monotonic() + 5
        while time.monotonic() < deadline:
            no_listeners = True
            for port in (18765, 18766):
                connection = socket.socket()
                connection.settimeout(0.25)
                result = connection.connect_ex(("127.0.0.1", port))
                connection.close()
                no_listeners = no_listeners and result == errno.ECONNREFUSED
            if no_listeners and not list(cache.glob("web-*.pid")):
                break
            time.sleep(0.1)
        else:
            raise SystemExit("the supervised Go server did not release both fixed ports and its temporary PID lease after app exit")

        summary = "WKWebView smoke passed: live UI rendered, /api/vaults returned 200, "
        if recovery_test:
            summary += "real WKWebView script popups were handled once, and unsaved DOM/input survived server stop + Retry; "
        summary += f"active vault was isolated at {active_path}, and both server ports were released"
        print(summary)


if __name__ == "__main__":
    main()
