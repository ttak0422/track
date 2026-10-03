#!/usr/bin/env python3
"""Verify tabs and settings across two real app processes using an owned persistent WK store."""

import json
import os
import plistlib
import shutil
import signal
import socket
import subprocess
import sys
import tempfile
import time
import uuid
from pathlib import Path


def ports_free():
    for port in (18765, 18766):
        with socket.socket() as connection:
            connection.settimeout(0.25)
            if connection.connect_ex(('127.0.0.1', port)) == 0:
                return False
    return True


def run_owned(command, environment, timeout=60):
    # A fresh process group lets a timeout terminate only this fixture's app and its server.
    process = subprocess.Popen(command, env=environment, start_new_session=True,
                               stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
    try:
        stdout, stderr = process.communicate(timeout=timeout)
    except subprocess.TimeoutExpired:
        os.killpg(process.pid, signal.SIGTERM)
        try:
            process.communicate(timeout=5)
        except subprocess.TimeoutExpired:
            os.killpg(process.pid, signal.SIGKILL)
            process.communicate()
        raise RuntimeError('fixture process timed out')
    if stdout:
        print(stdout, end='')
    if stderr:
        print(stderr, end='', file=sys.stderr)
    if process.returncode:
        raise RuntimeError(f'fixture process exited {process.returncode}')
    return process.pid


def main():
    if len(sys.argv) != 2:
        raise SystemExit('usage: desktop-restart-test.py PATH/TO/Track.app')
    source = Path(sys.argv[1]).resolve()
    if not (source / 'Contents/MacOS/Track').is_file():
        raise SystemExit('missing built Track.app')
    if not ports_free():
        raise SystemExit('fixed ports are occupied; stop here without touching the listener')
    identifier = str(uuid.uuid4())
    # Keep this directory on failure so cleanup can be retried with the exact owned identity.
    root = Path(tempfile.gettempdir()) / f'track-restart-{identifier}'
    root.mkdir(mode=0o700)
    (root / 'owner').write_text(identifier)
    app = root / 'RestartFixture.app'
    shutil.copytree(source, app)
    plist_path = app / 'Contents/Info.plist'
    plist = plistlib.loads(plist_path.read_bytes())
    plist['CFBundleIdentifier'] = f'com.ttak0422.track.restart-fixture.{identifier}'
    plist['CFBundleName'] = 'Track Restart Fixture'
    plist_path.write_bytes(plistlib.dumps(plist))
    (root / 'cache').mkdir()
    for vault in ('alpha', 'beta'):
        (root / vault).mkdir()
    (root / 'machine.yml').write_text(json.dumps({
        'default_vault': 'alpha', 'vaults': {v: str(root / v) for v in ('alpha', 'beta')},
        'cache_dir': str(root / 'cache')
    }))
    environment = {k: v for k, v in os.environ.items() if not k.startswith('TRACK_')}
    environment.update(TRACK_CONFIG=str(root / 'machine.yml'), TRACK_CACHE_DIR=str(root / 'cache'),
                       TRACK_WEB_RESTART_ROOT=str(root), TRACK_WEB_RESTART_ID=identifier)
    executable = str(app / 'Contents/MacOS/Track')
    cli = str(app / 'Contents/Resources/track')
    probe = Path(__file__).with_name('desktop-restart-probe.js').read_text()
    cleaned = False
    try:
        for vault in ('alpha', 'beta'):
            run_owned([cli, '--vault', vault, 'new', '--id', '100', '--title', 'Shared',
                       '--body', vault.upper() + '_FIXTURE_BODY'], environment)
        pids = []
        for phase in ('seed', 'restore'):
            (root / f'{phase}.js').write_text(
                f'const PHASE = {json.dumps(phase)}; const FIXTURE_ROOT = {json.dumps(str(root))};\n' + probe)
            environment.update(TRACK_WEB_RESTART_PHASE=phase,
                               TRACK_WEB_SMOKE_RESULT=str(root / f'{phase}.json'))
            pids.append(run_owned([executable, '--restart-test'], environment))
            report = json.loads((root / f'{phase}.json').read_text())
            if report.get('ok') is not True or report.get('phase') != phase:
                raise RuntimeError(f'{phase} failed: {json.dumps(report)}')
            deadline = time.monotonic() + 5
            while not ports_free() and time.monotonic() < deadline:
                time.sleep(0.1)
            if not ports_free() or list((root / 'cache').glob('web-*.pid')):
                raise RuntimeError('fixture server did not shut down cleanly')
            print(json.dumps({'pid': pids[-1], **report}, sort_keys=True))
        if pids[0] == pids[1]:
            raise RuntimeError('restart did not produce distinct app processes')
    finally:
        # The cleanup launch does not create a window or start a server. WebKit removes just this UUID.
        environment.update(TRACK_WEB_RESTART_PHASE='cleanup', TRACK_WEB_SMOKE_RESULT=str(root / 'cleanup.json'))
        try:
            run_owned([executable, '--restart-test'], environment)
            cleaned = (root / 'cleaned').read_text() == 'cleaned'
        finally:
            if cleaned:
                shutil.rmtree(root)
                print(f'Cleanup confirmed: owned store {identifier} removed; fixture directory removed.')
            else:
                print(f'Owned fixture retained for cleanup: {root}', file=sys.stderr)
    print('Persistent WKWebView restart passed: two processes, restored tabs/settings, vault bodies verified; owned store removed.')


if __name__ == '__main__':
    main()
