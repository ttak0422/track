#!/usr/bin/env python3
"""Actual internal CLI transport; every path belongs to a disposable fixture."""
import argparse
import hashlib
import json
import os
import select
from pathlib import Path
import signal
import sqlite3
import socket
import subprocess
import tempfile
import time
import urllib.error
import urllib.request

from process import Fixture, inventory, unchanged, wait_for


def fixture(base):
    f = Fixture(base)
    # No fixture marker is used by this production protocol. Preserve real
    # resolution semantics, including empty XDG entries and tilde config paths.
    f.env = {k: v for k, v in f.env.items()
             if not k.startswith(('TRACK_', 'XDG_'))}
    f.env.update(HOME=str(base / 'home'), PWD=str(base),
                 XDG_CONFIG_HOME='', XDG_CACHE_HOME=str(base / 'home'),
                 TRACK_CONFIG=str(f.config),
                 TRACK_WEB_DESKTOP_PARENT_PID=str(os.getpid()),
                 TRACK_WEB_DESKTOP_READY_TOKEN='a' * 64)
    f.paths.append(base / 'home')
    return f


def request(f, mode='preflight', revision=''):
    return dict(version=1, mode=mode, selector=dict(name='work', path=''),
                private_root=str(f.cache), revision=revision, nonce='b' * 64,
                context=dict(cwd=str(f.base), pwd=f.env.get('PWD'),
                             environment=sorted(k + '=' + v for k, v in f.env.items()
                                                if k == 'HOME' or k.startswith(('TRACK_', 'XDG_')))))


def command(binary, f, args=(), env=None):
    return subprocess.Popen([binary, '__native-read', *args], cwd=f.base,
                            env=f.env if env is None else env, stdin=subprocess.PIPE,
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE)


def exchange(binary, f, value, args=(), env=None):
    payload = value if isinstance(value, bytes) else json.dumps(value).encode()
    p = command(binary, f, args, env)
    try:
        out, err = p.communicate(payload, timeout=15)
        return p.returncode, out, err
    finally:
        if p.poll() is None:
            p.kill()
            p.wait(timeout=10)


def preflight(binary, f):
    before = inventory(f.paths + [f.cache])
    code, out, err = exchange(binary, f, request(f))
    assert code == 0, (out, err)
    m = json.loads(out)
    assert m['selector'] == dict(name='work', path='')
    assert m['active']['canonical_path'] == str(f.base / 'peer')
    assert m['context']['home'] == f.env['HOME']
    assert m['context']['cwd'] == str(f.base)
    assert len(m['revision']) == 64
    assert not list(f.cache.iterdir()), 'preflight wrote runtime files'
    unchanged(before, f.paths + [f.cache])
    return m


def address():
    for _ in range(100):
        with socket.socket() as s, socket.socket() as adjacent:
            s.bind(('127.0.0.1', 0))
            port = s.getsockname()[1]
            if port == 65535:
                continue
            try:
                adjacent.bind(('127.0.0.1', port + 1))
            except OSError:
                continue
            return '127.0.0.1:' + str(port)
    raise AssertionError('no free adjacent ports')


def ports_released(addr):
    port = int(addr.split(':')[1])
    for number in (port, port + 1):
        with socket.socket() as s:
            s.settimeout(.2)
            assert s.connect_ex(('127.0.0.1', number)) != 0, number


class Server:
    def __init__(self, binary, f, manifest):
        self.addr = address()
        self.log = tempfile.TemporaryFile()
        self.p = subprocess.Popen([binary, '__native-read', '--addr', self.addr],
                                  cwd=f.base, env=f.env, stdin=subprocess.PIPE,
                                  stdout=self.log, stderr=self.log)
        self.p.stdin.write(json.dumps(request(f, 'serve', manifest['revision'])).encode())
        self.p.stdin.close()
        self.opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))

    def logs(self):
        # pread does not disturb the file offset shared with the child.
        return os.pread(self.log.fileno(), 1 << 20, 0).decode(errors='replace')

    def ready(self):
        assert self.p.poll() is None, self.logs()
        return 'TRACK_DESKTOP_READY:' + 'a' * 64 + '\n' in self.logs()

    def get(self, path, method='GET', body=None):
        data = None if body is None else json.dumps(body).encode()
        req = urllib.request.Request('http://' + self.addr + path, data=data,
                                     method=method, headers={'Content-Type': 'application/json'})
        try:
            with self.opener.open(req, timeout=3) as r:
                return r.status, r.read().decode()
        except urllib.error.HTTPError as e:
            return e.code, e.read().decode()

    def storage_ready(self):
        status, body = self.get('/api/storage')
        return status == 200 and json.loads(body)['phase'] == 'ready'

    def close(self):
        if self.p.poll() is None:
            self.p.send_signal(signal.SIGTERM)
        try:
            self.p.wait(timeout=15)
        finally:
            if self.p.poll() is None:
                self.p.kill()
                self.p.wait(timeout=10)
            self.log.close()
        ports_released(self.addr)


def healthy(binary, base):
    f = fixture(base)
    # Tilde and active path aliases are Go-owned resolution, not Rust rewriting.
    (base / 'home/alias').symlink_to(base / 'active', target_is_directory=True)
    f.config.write_text('cache_dir: ~/ordinary\nvaults:\n  main: ~/alias\n  work: ' + str(base / 'peer') + '\n')
    m = preflight(binary, f)
    before = inventory(f.paths)
    server = Server(binary, f, m)
    try:
        wait_for(server.ready, 'exact owned readiness token')
        # Capabilities are checked before storage/registry reads.
        status, body = server.get('/api/capabilities')
        assert status == 200
        assert json.loads(body) == dict(protocol='track-canonical-read-only', version=2,
                                        canonical_read_only=True,
                                        manifest_revision=m['revision'], process_nonce='b' * 64)
        wait_for(server.storage_ready, 'bound storage')
        for vault, label in [('main', 'active'), ('work', 'peer')]:
            path = '/api/note?id=100&vault=' + vault
            wait_for(lambda: server.get(path)[0] == 200, 'peer read availability')
            status, body = server.get(path)
            assert status == 200 and label + ' sentinel' in body, body
        assert server.get('/api/events')[0] == 403
        assert server.get('/api/note?id=100&vault=work', 'PUT', {'body': 'must not save'})[0] == 403
        unchanged(before, f.paths)
        # Drift is sticky even if the exact config bytes are restored.
        original = f.config.read_bytes()
        f.config.write_bytes(original + b'\n')
        assert server.get('/api/capabilities')[0] == 503
        f.config.write_bytes(original)
        assert server.get('/api/note?id=100&vault=work')[0] == 503
        expected = dict(before)
        current_config = inventory([f.config])[(0, '.')]
        expected[(2, '.')] = current_config
        unchanged(expected, f.paths)
    finally:
        server.close()
    unchanged(expected, f.paths)
    assert not list(f.cache.rglob('*.pid')), 'owned lease survived shutdown'



def file_isolation(binary, base):
    f = fixture(base)
    vault = base / 'peer'
    for note_id, title in [(200, 'Cached'), (300, 'HealthyAfterConflict'), (400, 'ColdInvalid')]:
        (vault / f'note/{note_id}.md').write_text(title + ' body\n')
        (vault / f'.track/notes/{note_id}.yaml').write_text('version: 1\ntitle: ' + title + '\n')
    (vault / 'note/100 (conflicted copy).md').write_text('must never replace original\n')
    (vault / 'note/900-DESKTOP-ABC.md').write_text('must never import conflict\n')
    (vault / '.track/notes/400.yaml').write_text('version: 1\ntitle: [\n')
    m = preflight(binary, f)
    before = inventory(f.paths)
    server = Server(binary, f, m)
    try:
        wait_for(server.ready, 'isolation readiness')
        assert server.get('/api/capabilities')[0] == 200
        def listing():
            status, body = server.get('/api/notes?vault=work')
            if status != 200:
                return None
            data = json.loads(body)
            return data if {100, 200, 300} <= {row['note_id'] for row in data['notes']} else None
        wait_for(listing, 'valid notes past conflict and cold-invalid files')
        # Check both the disposable private index and HTTP presentation. The
        # confirmed-files fallback must retain bounded per-file diagnostics.
        def indexed():
            try:
                with sqlite3.connect(Path(m['active']['private_db']).as_uri() + '?mode=ro', uri=True) as db:
                    state = json.loads(db.execute("SELECT value FROM index_state WHERE key='read_state'").fetchone()[0])
                    rows = dict(db.execute('SELECT id, title FROM notes'))
                    return state, rows
            except (sqlite3.Error, TypeError):
                return {}, {}
        wait_for(lambda: any(issue['path'] == str(vault / 'note/400.md')
                             for issue in indexed()[0].get('issues', [])),
                 'completed private index reports individual file issues')
        data = listing()
        assert {row['note_id'] for row in data['notes']} == {100, 200, 300}, data
        state, rows = indexed()
        assert state['partial'] and set(rows) == {100, 200, 300}, (state, rows)
        issues = {row['path'] for row in state['issues']}
        assert str(vault / 'note/100 (conflicted copy).md') in issues, state
        assert str(vault / 'note/400.md') in issues, state
        def http_issues(target):
            status, body = server.get(target)
            return json.loads(body).get('read_state', {}) if status == 200 else {}
        for target in ('/api/notes?vault=work', '/api/search?q=Healthy&vault=work',
                       '/api/note?id=100&vault=work'):
            wait_for(lambda: issues <= {issue['path'] for issue in http_issues(target).get('issues', [])},
                     'HTTP retains completed per-file diagnostics')
        status, body = server.get('/api/note?id=100&vault=work')
        assert status == 200 and 'peer sentinel' in body, body
        unchanged(before, f.paths)
        # Corrupt an already indexed note. The expected sentinel updates only
        # this explicit fixture write; never accept a fresh canonical snapshot.
        broken = b'version: 1\ntitle: [\n'
        path = vault / '.track/notes/200.yaml'
        path.write_bytes(broken)
        stamp = 1577966400000000000
        os.utime(path, ns=(stamp, stamp))
        expected = dict(before)
        key = (1, '.track/notes/200.yaml')
        row = before[key]
        expected[key] = (row[0], len(broken), stamp, hashlib.sha256(broken).hexdigest())
        assert server.get('/api/storage', 'POST', {})[0] == 200
        def retained():
            state, rows = indexed()
            return rows.get(200) == 'Cached' and any(issue.get('note_id') == 200 and issue.get('cached')
                                                   for issue in state.get('issues', []))
        wait_for(retained, 'cached invalid note is retained with a per-file issue')
        for target in ('/api/notes?vault=work', '/api/search?q=Healthy&vault=work',
                       '/api/note?id=300&vault=work'):
            wait_for(lambda: http_issues(target).get('cache_used') and
                     any(issue.get('note_id') == 200 and issue.get('cached')
                         for issue in http_issues(target).get('issues', [])),
                     'HTTP retains cached-invalid distinction')
        assert server.get('/api/note?id=300&vault=work')[0] == 200
        unchanged(expected, f.paths)
    finally:
        server.close()
    unchanged(expected, f.paths)
    print('file isolation: healthy HTTP reads; conflicts skipped; cached invalid note retained; bounded per-file HTTP diagnostics preserved')


def database_failure(binary, base):
    f = fixture(base)
    m = preflight(binary, f)
    db = Path(m['active']['private_db'])
    db.parent.mkdir(mode=0o700, parents=True)
    db.write_bytes(b'this is not a SQLite database')
    before = inventory(f.paths)
    server = Server(binary, f, m)
    try:
        wait_for(server.ready, 'failed-store readiness')
        assert server.get('/api/capabilities')[0] == 200
        def unavailable():
            status, body = server.get('/api/storage')
            return status == 200 and json.loads(body)['phase'] == 'unavailable'
        wait_for(unavailable, 'whole-store failure is unavailable, not an isolated file skip')
        status, body = server.get('/api/storage')
        assert json.loads(body)['read_only'], body
        # A fallback may show confirmed files, but must carry partial status.
        status, body = server.get('/api/notes?vault=work')
        if status == 200:
            assert json.loads(body)['read_state']['partial'], body
        else:
            assert status in (503, 500), body
        unchanged(before, f.paths)
    finally:
        server.close()
    unchanged(before, f.paths)
    print('database failure: unavailable storage; canonical fixtures unchanged')


def orphan_cleanup(binary, base):
    f = fixture(base)
    before = inventory(f.paths)
    read_fd, write_fd = os.pipe()
    supervisor = os.fork()
    if supervisor == 0:
        os.close(read_fd)
        server = None
        try:
            f.env['TRACK_WEB_DESKTOP_PARENT_PID'] = str(os.getpid())
            m = preflight(binary, f)
            server = Server(binary, f, m)
            wait_for(server.ready, 'supervised orphan readiness')
            assert server.get('/api/capabilities')[0] == 200
            wait_for(server.storage_ready, 'supervised orphan storage')
            os.write(write_fd, json.dumps(dict(pid=server.p.pid, addr=server.addr)).encode() + b'\n')
            os.close(write_fd)
            signal.pause()
        except BaseException:
            if server is not None:
                server.close()
            os._exit(1)
        os._exit(0)
    os.close(write_fd)
    child = None
    reaped = False
    child_exited = False
    try:
        assert select.select([read_fd], [], [], 25)[0], 'supervisor did not publish ready child'
        with os.fdopen(read_fd, 'rb') as reader:
            data = reader.readline(4096)
        child = json.loads(data)
        os.kill(supervisor, signal.SIGKILL)
        os.waitpid(supervisor, 0)
        reaped = True
        def exited():
            try:
                os.kill(child['pid'], 0)
                return False
            except ProcessLookupError:
                return True
        wait_for(exited, 'owned server exits after its actual parent dies')
        child_exited = True
        ports_released(child['addr'])
        assert not list(f.cache.rglob('*.pid')), 'orphaned child retained lease'
        unchanged(before, f.paths)
    finally:
        if not reaped:
            try:
                os.kill(supervisor, signal.SIGKILL)
            except ProcessLookupError:
                pass
            os.waitpid(supervisor, 0)
        if child is not None and not child_exited:
            try:
                os.kill(child['pid'], signal.SIGKILL)
            except ProcessLookupError:
                pass
    print('orphan cleanup: child exited, both listeners and lease released')


def rejected(binary, base):
    for case in ('missing-owner', 'wrong-owner', 'bad-token', 'nonpipe', 'truncated',
                 'oversize', 'duplicate', 'context', 'wrong-revision', 'preflight-addr',
                 'serve-no-addr', 'nonloopback', 'adjacent-overflow', 'missing-peer',
                 'overlap', 'config-drift', 'bind-main', 'bind-adjacent'):
        root = base / case
        root.mkdir()
        f = fixture(root)
        m = preflight(binary, f)
        env = dict(f.env)
        value = request(f)
        args = []
        occupied = None
        addr = address()
        if case == 'missing-owner':
            env.pop('TRACK_WEB_DESKTOP_PARENT_PID')
        elif case == 'wrong-owner':
            env['TRACK_WEB_DESKTOP_PARENT_PID'] = '1'
        elif case == 'bad-token':
            env['TRACK_WEB_DESKTOP_READY_TOKEN'] = 'invalid'
        elif case == 'truncated':
            value = b'{'
        elif case == 'oversize':
            value = b'x' * ((256 << 10) + 1)
        elif case == 'duplicate':
            value = b'{"version":1,' + json.dumps(value).encode()[1:]
        elif case == 'context':
            value['context']['pwd'] = 'changed'
        elif case == 'preflight-addr':
            args = ['--addr', addr]
        else:
            if case != 'nonpipe':
                value = request(f, 'serve', m['revision'])
                args = ['--addr', addr]
            if case == 'wrong-revision':
                value['revision'] = '0' * 64
            elif case == 'serve-no-addr':
                args = []
            elif case == 'nonloopback':
                args = ['--addr', '0.0.0.0:19000']
            elif case == 'adjacent-overflow':
                args = ['--addr', '127.0.0.1:65535']
            elif case == 'missing-peer':
                (root / 'peer').rename(root / 'removed-peer')
            elif case == 'overlap':
                value['private_root'] = str(root / 'active')
            elif case == 'config-drift':
                f.config.write_bytes(f.config.read_bytes() + b'\n')
            elif case in ('bind-main', 'bind-adjacent'):
                port = int(addr.split(':')[1]) + (case == 'bind-adjacent')
                occupied = socket.socket()
                occupied.bind(('127.0.0.1', port))
                occupied.listen()
        before = inventory([root])
        try:
            if case == 'nonpipe':
                with tempfile.TemporaryFile() as source:
                    source.write(json.dumps(value).encode())
                    source.seek(0)
                    p = subprocess.run([binary, '__native-read'], cwd=root, env=env,
                                       stdin=source, capture_output=True, timeout=15)
                code, out, err = p.returncode, p.stdout, p.stderr
            else:
                code, out, err = exchange(binary, f, value, args, env)
            assert code != 0 and b'TRACK_DESKTOP_READY' not in err, (case, out, err)
            assert json.loads(out) == {'error': 'native read launch rejected'}, (case, out)
            unchanged(before, [root])
        finally:
            if occupied is not None:
                occupied.close()
        ports_released(addr)
        print('rejected:', case)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--binary', required=True)
    args = parser.parse_args()
    binary = str(Path(args.binary).resolve(strict=True))
    with tempfile.TemporaryDirectory(prefix='track-bound-cli-') as temporary:
        base = Path(temporary).resolve()
        (base / 'healthy').mkdir()
        healthy(binary, base / 'healthy')
        (base / 'file-isolation').mkdir()
        file_isolation(binary, base / 'file-isolation')
        (base / 'database-failure').mkdir()
        database_failure(binary, base / 'database-failure')
        (base / 'orphan').mkdir()
        orphan_cleanup(binary, base / 'orphan')
        (base / 'rejected').mkdir()
        rejected(binary, base / 'rejected')
    print('bound internal CLI: healthy reads, sticky drift and 18 rejection cases passed')


if __name__ == '__main__':
    main()
