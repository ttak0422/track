#!/usr/bin/env python3
"""Exercise production AppDelegate close/quit/OS callbacks with isolated React + Go."""
import importlib.util
import json
import os
import plistlib
import shutil
import signal
import subprocess
import sys
import tempfile
import time
import uuid
from pathlib import Path

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parent.parent
spec = importlib.util.spec_from_file_location('recovery', ROOT / 'scripts/desktop-editor-recovery.py')
recovery = importlib.util.module_from_spec(spec)
spec.loader.exec_module(recovery)
SCENARIOS = ['clean-close', 'clean-quit', 'clean-os', 'empty-close', 'saved-quit', 'multi-clean',
             'dirty-cancel-save', 'dirty-discard', 'dirty-os-cancel', 'initial-os',
             'timeout-cancel', 'probe-error-cancel', 'unknown-cancel', 'stopped-child-quit', 'startup-os', 'pending-cancel', 'ime-cancel', 'dirty-multi-cancel', 'dirty-cancel-close', 'clean-edit-quit']


def main():
    executable = Path(sys.argv[1]).resolve()
    scenarios = sys.argv[2:] or SCENARIOS
    with tempfile.TemporaryDirectory(prefix='track-termination-') as temporary:
        root = Path(temporary)
        environment = {k: v for k, v in os.environ.items() if not k.startswith('TRACK_')}
        environment.update(DEVELOPER_DIR=os.environ.get('DESKTOP_DEVELOPER_DIR') or subprocess.check_output(
            ['/usr/bin/xcode-select', '-p'], text=True).strip(), CLANG_MODULE_CACHE_PATH=str(ROOT / 'build/desktop-clang-module-cache'))
        environment['SDKROOT'] = subprocess.check_output(['/usr/bin/xcrun', '--sdk', 'macosx', '--show-sdk-path'],
            env={k: v for k, v in environment.items() if k != 'SDKROOT'}, text=True).strip()
        package = root / 'package'
        harness = package / 'Harness'
        harness.mkdir(parents=True)
        (package / 'Core').symlink_to(ROOT / 'desktop/Sources/TrackWebCore', target_is_directory=True)
        for source in (ROOT / 'desktop/Sources/TrackWebApp').glob('*.swift'):
            if source.name != 'TrackWebMain.swift':
                (harness / source.name).symlink_to(source)
        (harness / 'main.swift').symlink_to(ROOT / 'desktop/Tests/Termination/main.swift')
        (package / 'Package.swift').write_text('''// swift-tools-version: 6.0
import PackageDescription
let package = Package(name: "Termination", platforms: [.macOS(.v14)], targets: [
    .target(name: "TrackWebCore", path: "Core"),
    .executableTarget(name: "Termination", dependencies: ["TrackWebCore"], path: "Harness"),
])
''')
        subprocess.run(['swift', 'build', '--package-path', str(package), '--scratch-path', str(ROOT / 'build/termination-harness')], env=environment, check=True)
        bundle = root / 'TerminationFixture.app/Contents'
        (bundle / 'MacOS').mkdir(parents=True)
        (bundle / 'Resources').mkdir()
        shutil.copy2(ROOT / 'build/termination-harness/debug/Termination', bundle / 'MacOS/Termination')
        (bundle / 'Resources/track').symlink_to(executable)
        (bundle / 'Info.plist').write_bytes(plistlib.dumps({
            'CFBundleIdentifier': f'com.ttak0422.track.termination-fixture.{uuid.uuid4()}',
            'CFBundleName': 'Track Termination Fixture', 'CFBundleExecutable': 'Termination',
            'CFBundlePackageType': 'APPL', 'NSHighResolutionCapable': True,
        }))
        for scenario in scenarios:
            fixture = root / scenario
            vault, cache = fixture / 'vault', fixture / 'cache'
            (vault / '.track').mkdir(parents=True)
            cache.mkdir()
            (vault / '.track/config.yml').write_text('journal: false\ngen: false\n')
            config = fixture / 'machine.yml'
            config.write_text(json.dumps({'vault_dir': str(vault), 'cache_dir': str(cache)}))
            env = environment | {'TRACK_CONFIG': str(config), 'TRACK_VAULT': str(vault), 'TRACK_CACHE_DIR': str(cache)}
            for note_id, title, body in [('100', 'Fixture', 'Original fixture\n\n[[101]]'), ('101', 'Other note', 'Second note')]:
                subprocess.run([str(executable), 'new', '--id', note_id, '--title', title, '--body', body],
                               env=env, check=True, stdout=subprocess.DEVNULL)
            original = (vault / 'note/100.md').read_bytes()
            port, reservations = recovery.reserve_pair()
            for reservation in reservations:
                reservation.close()
            result = fixture / 'result.json'
            print(f'RUN {scenario}: owned ports {port},{port+1}', flush=True)
            app = subprocess.Popen([str(bundle / 'MacOS/Termination'), scenario, str(port), str(result)], env=env, start_new_session=True)
            status = None
            try:
                status = app.wait(timeout=35)
            finally:
                if status != 0:
                    # Only the process group created for this fixture, including a SIGSTOP'd child.
                    try:
                        os.killpg(app.pid, signal.SIGKILL)
                    except ProcessLookupError:
                        pass
                    app.wait()
                deadline = time.monotonic() + 10
                while time.monotonic() < deadline:
                    leases = list(cache.glob('web-*.pid'))
                    # SIGKILL cannot run Go's lease defer. Its private stale lease is safe only
                    # when that PID is gone, and TemporaryDirectory removes it after the check.
                    lease_ok = not leases
                    if scenario == 'stopped-child-quit' and leases:
                        lease_ok = True
                        for lease in leases:
                            try:
                                os.kill(int(lease.read_text().splitlines()[0]), 0)
                                lease_ok = False
                            except ProcessLookupError:
                                pass
                    if recovery.ports_closed(port) and lease_ok:
                        break
                    time.sleep(0.05)
                else:
                    raise RuntimeError('owned listener/lease cleanup not confirmed; no foreign process signalled')
            report = json.loads(result.read_text()) if result.is_file() else {}
            if status or report.get('ok') is not True:
                raise RuntimeError(f'{scenario} failed (exit={status}): {report}')
            expected = 'Fixture unsaved 日本語\n'.encode() if scenario in ('saved-quit', 'dirty-cancel-save', 'dirty-os-cancel', 'pending-cancel', 'dirty-multi-cancel', 'dirty-cancel-close') else original
            if (vault / 'note/100.md').read_bytes() != expected:
                raise RuntimeError(f'{scenario}: saved/discarded disk bytes did not match')
            print(f'PASS {scenario}: disk bytes, owned listener cleanup and lease state verified', flush=True)


if __name__ == '__main__':
    main()
