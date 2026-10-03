#!/usr/bin/env python3
"""Run the real React editor with production WK coordination on an owned loopback pair."""

import errno
import json
import os
import socket
import subprocess
import sys
import tempfile
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
FINAL_BODY = 'WK3 unsaved draft\n日本語 café — second line\nEdited after retry.\n'


def reserve_pair():
    for _ in range(100):
        first, second = socket.socket(), socket.socket()
        try:
            first.bind(('127.0.0.1', 0))
            port = first.getsockname()[1]
            if port >= 65535:
                raise OSError('no adjacent port')
            second.bind(('127.0.0.1', port + 1))
            return port, (first, second)
        except OSError:
            first.close()
            second.close()
    raise RuntimeError('could not reserve a unique loopback port pair')


def ports_closed(port):
    for candidate in (port, port + 1):
        with socket.socket() as connection:
            connection.settimeout(0.2)
            if connection.connect_ex(('127.0.0.1', candidate)) != errno.ECONNREFUSED:
                return False
    return True


def main():
    if len(sys.argv) != 2:
        raise SystemExit('usage: desktop-editor-recovery.py BUILT_TRACK_GO_EXECUTABLE')
    executable = Path(sys.argv[1]).resolve()
    if not executable.is_file():
        raise SystemExit(f'Go executable missing: {executable}; build with make desktop-app')
    with tempfile.TemporaryDirectory(prefix='track-wk3-') as temporary:
        root = Path(temporary)
        vault, cache, package = root / 'vault', root / 'cache', root / 'package'
        for directory in (vault / 'note', vault / '.track' / 'notes', cache, package):
            directory.mkdir(parents=True)
        (vault / '.track' / 'config.yml').write_text('journal: false\ngen: false\nweb:\n  home: \"100\"\n', encoding='utf-8')
        note = vault / 'note' / '100.md'
        config = root / 'machine.yml'
        config.write_text(f'vault_dir: {json.dumps(str(vault))}\ncache_dir: {json.dumps(str(cache))}\n', encoding='utf-8')
        environment = {key: value for key, value in os.environ.items() if not key.startswith('TRACK_')}
        environment.update(TRACK_CONFIG=str(config), TRACK_VAULT=str(vault), TRACK_CACHE_DIR=str(cache))
        environment.update(
            DEVELOPER_DIR=os.environ.get('DESKTOP_DEVELOPER_DIR', '/Library/Developer/CommandLineTools'),
            SDKROOT=os.environ.get('DESKTOP_SDKROOT', '/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk'),
            CLANG_MODULE_CACHE_PATH=str(root / 'module-cache'),
        )
        subprocess.run([str(executable), 'new', '--id', '100', '--title', 'WK3 fixture',
                        '--body', 'WK3 original body\n'], env=environment, check=True, timeout=30)
        # Symlink only reviewed source files into a temporary SwiftPM package. This avoids adding
        # a test executable to the shipped app or depending on another PR's AppDelegate hooks.
        (package / 'Core').symlink_to(ROOT / 'desktop/Sources/TrackWebCore', target_is_directory=True)
        harness = package / 'Harness'
        harness.mkdir()
        for source in (ROOT / 'desktop/Tests/EditorRecovery/main.swift',
                       ROOT / 'desktop/Sources/TrackWebApp/WebViewCoordinator.swift',
                       ROOT / 'desktop/Sources/TrackWebApp/TrackServerProcess.swift'):
            (harness / source.name).symlink_to(source)
        (package / 'Package.swift').write_text('''// swift-tools-version: 6.0
import PackageDescription
let package = Package(name: "WK3", platforms: [.macOS(.v14)], targets: [
    .target(name: "TrackWebCore", path: "Core"),
    .executableTarget(name: "WK3", dependencies: ["TrackWebCore"], path: "Harness"),
])
''', encoding='utf-8')
        subprocess.run(['swift', 'build', '--package-path', str(package)], env=environment, check=True)
        probe = root / 'probe.js'
        probe.write_text('window.__wk3ExpectedVault = ' + json.dumps(str(vault)) + ';\n' +
                         (ROOT / 'scripts/desktop-editor-recovery-probe.js').read_text(encoding='utf-8'),
                         encoding='utf-8')
        port, reservations = reserve_pair()
        print(f'WK3 isolated vault={vault} ports={port},{port + 1}', flush=True)
        # The Go readiness token proves ownership after release/bind. A competitor winning this
        # small race causes a failure; it is never adopted or killed.
        for reservation in reservations:
            reservation.close()
        app = subprocess.Popen([str(package / '.build/debug/WK3'), str(executable), str(port),
                                str(probe)], env=environment)
        try:
            status = app.wait(timeout=95)
        except BaseException:
            # Only this exact child handle is signalled. Go watches its real parent's lifetime.
            app.terminate()
            try:
                app.wait(timeout=5)
            except subprocess.TimeoutExpired:
                app.kill()
                app.wait()
            raise
        finally:
            deadline = time.monotonic() + 10
            while time.monotonic() < deadline:
                if ports_closed(port) and not list(cache.glob('web-*.pid')):
                    break
                time.sleep(0.1)
            else:
                raise RuntimeError('owned server cleanup not confirmed; no foreign process was signalled')
        if status:
            raise RuntimeError(f'WK3 harness exited {status}')
        if note.read_bytes() != FINAL_BODY.encode('utf-8'):
            raise RuntimeError('saved fixture file does not match complete draft')
        print('PASS WK3: exact disk bytes verified; owned listeners and PID lease released')


if __name__ == '__main__':
    main()
