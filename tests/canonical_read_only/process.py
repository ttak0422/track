#!/usr/bin/env python3
"""Real-process regression. Build ./cmd/track, then pass --binary explicitly.

Uses only temporary synthetic vaults. Never launches the ordinary/default mode.
Sentinels include directory entries, modes, sizes, mtimes and file SHA256 (not atime).
This tests the application contract, not an OS sandbox or hostile concurrent rename.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import signal
import socket
import subprocess
import tempfile
import time
import urllib.error
import urllib.request


def inventory(paths):
    result = {}
    for i, root in enumerate(paths):
        for p in [root] + (sorted(root.rglob('*')) if root.is_dir() else []):
            st = p.lstat()
            result[(i, str(p.relative_to(root)))] = (
                st.st_mode, st.st_size, st.st_mtime_ns,
                hashlib.sha256(p.read_bytes()).hexdigest() if p.is_file() else '')
    return result


def unchanged(before, paths):
    after = inventory(paths)
    differences = [k for k in before.keys() | after.keys() if before.get(k) != after.get(k)]
    assert not differences, f'canonical/ordinary cache changed: {differences}'


def wait_for(predicate, description):
    deadline = time.monotonic() + 20
    while time.monotonic() < deadline:
        if predicate():
            return
        time.sleep(.025)
    raise AssertionError('timeout: ' + description)


class Fixture:
    def __init__(self, base):
        self.base = base
        for name in ('active', 'peer', 'private', 'ordinary', 'home'):
            (base / name).mkdir(mode=0o700)
        self.cache = base / 'private'
        self.config = base / 'machine.yml'
        self.config.write_text(f'cache_dir: {base / "ordinary"}\nvaults:\n  main: {base / "active"}\n  work: {base / "peer"}\n')
        for name in ('active', 'peer'):
            root = base / name
            (root / 'note').mkdir()
            (root / '.track/notes').mkdir(parents=True)
            (root / 'assets').mkdir()
            (root / 'note/100.md').write_text(f'{name} sentinel\n- [ ] a task\n')
            (root / '.track/notes/100.yaml').write_text('version: 1\ntitle: Sentinel\ntags: []\n')
            (root / 'assets/sentinel.bin').write_bytes(bytes(range(16)))
            os.utime(root / 'note/100.md', (1577880000, 1577880000))
        self.paths = [base / 'active', base / 'peer', self.config, base / 'ordinary']
        # Do not inherit Track selectors, caches, or desktop lifecycle settings.
        self.env = {k: v for k, v in os.environ.items() if not k.startswith('TRACK_')}
        self.env.update(HOME=str(base / 'home'), XDG_CONFIG_HOME=str(base / 'home'),
                        XDG_CACHE_HOME=str(base / 'home'), TRACK_CONFIG=str(self.config),
                        TRACK_VAULT=str(base / 'active'), TRACK_WEB_DESKTOP_PARENT_PID=str(os.getpid()),
                        TRACK_WEB_DESKTOP_READY_TOKEN='sentinel-owned-ready-token')


class Server:
    def __init__(self, binary, f, flags=None):
        # The engine owns workspace port and adjacent static-app port. Check
        # both, not just one; launch still fails on any later bind race.
        for _ in range(100):
            with socket.socket() as sock, socket.socket() as adjacent:
                sock.bind(('127.0.0.1', 0))
                port = sock.getsockname()[1]
                if port == 65535:
                    continue
                try:
                    adjacent.bind(('127.0.0.1', port + 1))
                except OSError:
                    continue
                self.addr = '127.0.0.1:' + str(port)
                break
        else:
            raise AssertionError('no available adjacent loopback ports')
        self.log = tempfile.TemporaryFile()
        args = flags if flags is not None else ['--canonical-read-only', '--read-only-cache-root', str(f.cache)]
        self.p = subprocess.Popen([binary, 'web', '--addr', self.addr] + args,
                                  env=f.env, cwd=f.base, stdout=self.log, stderr=self.log,
                                  start_new_session=True)
        self.opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))

    def request(self, path, method='GET', body=None):
        data = None if body is None else json.dumps(body).encode()
        req = urllib.request.Request('http://' + self.addr + path, data=data, method=method,
                                     headers={'Content-Type': 'application/json'})
        try:
            with self.opener.open(req, timeout=3) as r:
                return r.status, r.read().decode()
        except urllib.error.HTTPError as e:
            return e.code, e.read().decode()

    def ready(self):
        assert self.p.poll() is None, self.logs()
        try:
            status, body = self.request('/api/storage')
            return status == 200 and json.loads(body).get('phase') == 'ready'
        except (OSError, ValueError):
            return False

    def logs(self):
        self.log.seek(0)
        return self.log.read().decode(errors='replace')

    def close(self):
        if self.p.poll() is None:
            self.p.send_signal(signal.SIGTERM)
            try:
                self.p.wait(timeout=10)
            except subprocess.TimeoutExpired:
                os.killpg(self.p.pid, signal.SIGKILL)
                self.p.wait()
                raise AssertionError('graceful shutdown timed out: ' + self.logs())
        assert self.p.returncode == 0, self.logs()
        self.log.close()


def exercise(binary, base):
    f = Fixture(base)
    before = inventory(f.paths)
    s = Server(binary, f)
    try:
        def capability():
            assert s.p.poll() is None, s.logs()
            try:
                code, body = s.request('/api/capabilities')
                return code == 200 and json.loads(body) == dict(protocol='track-canonical-read-only', version=1, canonical_read_only=True)
            except (OSError, ValueError):
                return False
        wait_for(capability, 'capability handshake before readiness')
        try:
            wait_for(s.ready, 'verified startup')
        except AssertionError as e:
            raise AssertionError((str(e), s.request('/api/storage'), s.logs())) from e
        code, body = s.request('/api/capabilities')
        assert code == 200 and json.loads(body) == dict(protocol='track-canonical-read-only', version=1, canonical_read_only=True), body
        unchanged(before, f.paths)
        assert 'TRACK_DESKTOP_READY:sentinel-owned-ready-token' in s.logs(), 'owned listener readiness token missing'
        assert list(f.cache.rglob('*.db')), 'no private derived database'
        assert any('pid' in p.name for p in f.cache.rglob('*')), 'no private PID lease'
        for vault in ('main', 'work'):
            def peer_ready():
                code, body = s.request('/api/note?id=100&vault=' + vault)
                return code == 200 and 'sentinel' in body
            wait_for(peer_ready, vault + ' note indexed')
            for path in ('/api/note?id=100&', '/api/notes?', '/api/tasks?'):
                code, body = s.request(path + 'vault=' + vault)
                assert code == 200, (vault, path, code, body)
            code, body = s.request('/api/render?vault=' + vault, 'POST', {'body': '**sentinel**'})
            assert code == 200 and 'sentinel' in body, body
            spec = {'version': 2, 'mark': 'bar', 'data': {'kind': 'metric', 'records': [{'name': 'A', 'time': 't1', 'value': 3}]}, 'encoding': {'x': {'field': 'name', 'type': 'nominal'}, 'y': [{'field': 'value'}]}}
            code, body = s.request('/api/viewspec?vault=' + vault, 'POST', {'spec': json.dumps(spec)})
            assert code == 200 and 'echarts' in body, body
            code, body = s.request('/api/follow', 'POST', {'note_id': 100, 'file_kind': 'note', 'vault_path': str(base / ('active' if vault == 'main' else 'peer')), 'line': 1})
            assert code == 200 and json.loads(body)['active'], body
        for vault in ('main', 'work'):
            for route in ('calendar-events', 'calendar-activity'):
                code, body = s.request('/api/' + route + '/range?since=2020-01-01&until=2020-01-02&time_zone=UTC&vault=' + vault)
                assert code == 200, (vault, route, code, body)
        for method, path in [('PUT', '/api/note?id=100'), ('POST', '/api/task'), ('POST', '/api/calendar-event'), ('GET', '/apps'), ('POST', '/apps/run'), ('GET', '/api/ogp?url=http://127.0.0.1:1'), ('GET', '/api/unknown')]:
            code, body = s.request(path, method, {} if method != 'GET' else None)
            assert code == 403, (method, path, code, body)
        unchanged(before, f.paths)
        cached_before_edit = inventory([f.cache])
        # Change only the two expected body tuples, calculated from our inputs.
        # Never resnapshot live canonical state: that could bless a watcher write
        # to a sidecar/config/cache while these deliberate edits are happening.
        # Truncating existing files does not change parent directory metadata.
        stamp_ns = 1577966400 * 1_000_000_000
        for index, name in enumerate(('active', 'peer')):
            note = base / name / 'note/100.md'
            body = (name + ' updated sentinel\n').encode()
            key = (index, 'note/100.md')
            mode = before[key][0]
            before[key] = (mode, len(body), stamp_ns, hashlib.sha256(body).hexdigest())
            note.write_bytes(body)
            os.utime(note, ns=(stamp_ns, stamp_ns))
        unchanged(before, f.paths)
        for name, vault in [('active', 'main'), ('peer', 'work')]:
            def updated():
                code, body = s.request('/api/note?id=100&vault=' + vault)
                return code == 200 and name + ' updated sentinel' in body
            wait_for(updated, name + ' updated read')
            def indexed():
                code, body = s.request('/api/search?q=updated&vault=' + vault)
                return code == 200 and bool(json.loads(body).get('results'))
            wait_for(indexed, name + ' updated indexed search')
        assert inventory([f.cache]) != cached_before_edit, 'derived cache did not change'
        old_generation = json.loads(s.request('/api/storage')[1])['generation']
        code, body = s.request('/api/storage', 'POST', {})
        assert code == 200, body
        wait_for(s.ready, 'retry initialization')
        assert json.loads(s.request('/api/storage')[1])['generation'] > old_generation, 'retry did not advance generation'
        unchanged(before, f.paths)
    finally:
        s.close()
    unchanged(before, f.paths)
    assert not any('pid' in p.name for p in f.cache.rglob('*')), 'PID lease survived shutdown'


def unverified_capability(binary, base):
    f = Fixture(base)
    f.config.write_text('vaults: [invalid YAML')
    before = inventory(f.paths)
    s = Server(binary, f)
    try:
        def unavailable():
            assert s.p.poll() is None, s.logs()
            try:
                code, body = s.request('/api/storage')
                return code == 200 and json.loads(body)['phase'] == 'unavailable'
            except (OSError, ValueError):
                return False
        wait_for(unavailable, 'invalid configuration unavailable')
        code, body = s.request('/api/capabilities')
        assert code == 200 and json.loads(body) == dict(protocol='track-canonical-read-only', version=1, canonical_read_only=True), body
        unchanged(before, f.paths)
        assert not list(f.cache.iterdir()), 'invalid configuration created private cache'
    finally:
        s.close()
    unchanged(before, f.paths)


def rejected_launches(binary, base):
    f = Fixture(base)
    before = inventory(f.paths)
    alias = base / 'alias'
    alias.symlink_to(f.cache, target_is_directory=True)
    cases = [
        ['--canonical-read-only'],
        ['--read-only-cache-root', str(f.cache)],
        ['--canonical-read-only', '--read-only-cache-root', 'relative'],
        ['--canonical-read-only', '--read-only-cache-root', str(base / 'absent')],
        ['--canonical-read-only', '--read-only-cache-root', str(alias)],
    ]
    for flags in cases:
        s = Server(binary, f, flags)
        try:
            s.p.wait(timeout=10)
            assert s.p.returncode != 0, ('invalid options accepted', flags, s.logs())
            unchanged(before, f.paths)
            assert not list(f.cache.iterdir()), ('cache written on rejection', flags)
        finally:
            if s.p.poll() is None:
                os.killpg(s.p.pid, signal.SIGKILL)
                s.p.wait()
            s.log.close()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', required=True, type=Path)
    args = parser.parse_args()
    binary = str(args.binary.resolve(strict=True))
    with tempfile.TemporaryDirectory(prefix='track-readonly-process-') as tmp:
        exercise(binary, Path(tmp).resolve())
    with tempfile.TemporaryDirectory(prefix='track-readonly-invalid-') as tmp:
        rejected_launches(binary, Path(tmp).resolve())
    with tempfile.TemporaryDirectory(prefix='track-readonly-unverified-') as tmp:
        unverified_capability(binary, Path(tmp).resolve())
    print('PASS: isolated real-process canonical-read-only sentinels')


if __name__ == '__main__':
    main()
