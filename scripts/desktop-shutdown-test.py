#!/usr/bin/env python3
"""Prove deferred app termination waits for a SIGSTOP'd owned Go child to be killed/reaped."""

import errno
import json
import os
import signal
import socket
import subprocess
import sys
import tempfile
import time
import urllib.request
from pathlib import Path


def child_exists(pid: int) -> bool:
    try:
        os.kill(pid, 0)
        return True
    except ProcessLookupError:
        return False
    except PermissionError:
        return True


def child_state(pid: int) -> str:
    return subprocess.check_output(["ps", "-p", str(pid), "-o", "state="], text=True).strip()


def ports_are_closed() -> bool:
    for port in (18765, 18766):
        connection = socket.socket()
        connection.settimeout(0.2)
        result = connection.connect_ex(("127.0.0.1", port))
        connection.close()
        if result != errno.ECONNREFUSED:
            return False
    return True


def main() -> None:
    if len(sys.argv) != 2:
        raise SystemExit("usage: desktop-shutdown-test.py TRACK_WEB_EXECUTABLE")
    executable = Path(sys.argv[1]).resolve()

    with tempfile.TemporaryDirectory(prefix="track-web-shutdown-") as temp:
        root = Path(temp)
        vault = root / "vault"
        cache = root / "cache"
        vault.mkdir()
        cache.mkdir()
        config = root / "machine.yml"
        trigger = root / "terminate-now"
        result_file = root / "result.json"
        config.write_text(
            f"vault_dir: {json.dumps(str(vault))}\ncache_dir: {json.dumps(str(cache))}\n",
            encoding="utf-8",
        )
        environment = {key: value for key, value in os.environ.items() if not key.startswith("TRACK_")}
        environment.update(
            {
                "TRACK_CONFIG": str(config),
                "TRACK_VAULT": str(vault),
                "TRACK_CACHE_DIR": str(cache),
                "TRACK_WEB_SHUTDOWN_TEST_TRIGGER": str(trigger),
                "TRACK_WEB_SHUTDOWN_TEST_RESULT": str(result_file),
            }
        )

        app = subprocess.Popen(
            [str(executable), "--shutdown-test"],
            env=environment,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
        )
        child_pid: int | None = None
        try:
            lease = cache / "web-127.0.0.1-18765.pid"
            deadline = time.monotonic() + 25
            while time.monotonic() < deadline:
                if app.poll() is not None:
                    stdout, stderr = app.communicate()
                    raise RuntimeError(f"app exited before starting its Go child ({app.returncode})\n{stdout}\n{stderr}")
                if lease.is_file():
                    lines = lease.read_text(encoding="utf-8").splitlines()
                    if lines:
                        candidate = int(lines[0])
                        ppid = int(subprocess.check_output(["ps", "-p", str(candidate), "-o", "ppid="], text=True).strip())
                        if ppid != app.pid:
                            raise RuntimeError(f"PID lease did not belong to the app's direct child: {candidate} ppid={ppid}")
                        try:
                            with urllib.request.urlopen("http://127.0.0.1:18765/api/vaults", timeout=0.5) as response:
                                if response.status == 200:
                                    child_pid = candidate
                                    break
                        except OSError:
                            pass
                time.sleep(0.05)
            if child_pid is None:
                raise RuntimeError("isolated Track Web server did not become ready")

            ready_deadline = time.monotonic() + 10
            while time.monotonic() < ready_deadline:
                if app.poll() is not None:
                    stdout, stderr = app.communicate()
                    raise RuntimeError(f"app exited before its supervisor reached ready ({app.returncode})\n{stdout}\n{stderr}")
                if result_file.is_file():
                    progress = json.loads(result_file.read_text(encoding="utf-8"))
                    if progress.get("phase") == "server-ready":
                        break
                time.sleep(0.05)
            else:
                report = result_file.read_text(encoding="utf-8") if result_file.is_file() else "<no supervisor-ready report>"
                raise RuntimeError(f"app did not finish its readiness state before child suspension: {report}")

            # The PID came from this test's private cache lease and its parent was verified above;
            # no process is discovered or signalled by name or port.
            os.kill(child_pid, signal.SIGSTOP)
            stop_deadline = time.monotonic() + 3
            while time.monotonic() < stop_deadline and not child_state(child_pid).startswith("T"):
                time.sleep(0.025)
            if not child_state(child_pid).startswith("T"):
                raise RuntimeError("Go child did not enter SIGSTOP state")

            trigger.touch()
            started = time.monotonic()
            try:
                stdout, stderr = app.communicate(timeout=15)
            except subprocess.TimeoutExpired as error:
                report = result_file.read_text(encoding="utf-8") if result_file.is_file() else "<no shutdown progress report>"
                try:
                    state = child_state(child_pid)
                except (OSError, subprocess.CalledProcessError):
                    state = "<no process>"
                raise RuntimeError(
                    f"app never completed its deferred termination reply; child state={state}; "
                    f"report={report}; stdout={error.stdout!r}; stderr={error.stderr!r}"
                ) from error
            if app.returncode != 0:
                raise RuntimeError(f"app exited with status {app.returncode}\n{stdout}\n{stderr}")
            if child_exists(child_pid):
                raise RuntimeError(f"owned Go child {child_pid} survived app termination\n{stdout}\n{stderr}")
            if not result_file.is_file():
                raise RuntimeError("app exited without its child-exit confirmation report")
            report = json.loads(result_file.read_text(encoding="utf-8"))
            if report.get("ok") is not True or int(report.get("stopDurationMs", 0)) < 2_500:
                raise RuntimeError(f"termination was not deferred through the forced child exit: {report}; stdout={stdout!r}; stderr={stderr!r}")
            if not ports_are_closed():
                raise RuntimeError("a Track Web listener survived app termination")

            print(
                "SIGSTOP shutdown test passed: AppKit waited for the owned Go child to be force-killed "
                f"and reaped ({report['stopDurationMs']} ms); no listener or child remains"
            )
        finally:
            if child_pid is not None and child_exists(child_pid):
                # Failure cleanup is still scoped to the PID read from this test's private lease.
                try:
                    os.kill(child_pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
            if app.poll() is None:
                app.kill()
                app.communicate()


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        raise SystemExit(f"desktop shutdown test failed: {error}") from error
