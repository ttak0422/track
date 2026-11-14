#!/usr/bin/env python3
"""Disposable-fixture-only track-bin adapter. No production API hooks."""
import http.client
import http.server
import json
import os
from pathlib import Path
import signal
import socket
import subprocess
import sys
import tempfile
import threading
import time
from urllib.parse import urlsplit

LIMIT = 16 * 1024 * 1024


def validate_fixture(env):
    root = Path(env['TRACK_QA_ROOT']).resolve(strict=True)
    if not root.is_relative_to(Path(tempfile.gettempdir()).resolve()) or root == Path(tempfile.gettempdir()).resolve():
        raise ValueError('QA root must be a dedicated temporary directory')
    for name in ('TRACK_VAULT', 'TRACK_CONFIG', 'TRACK_CACHE_DIR'):
        path = Path(env[name]).resolve(strict=True)
        if not path.is_relative_to(root):
            raise ValueError(f'{name} must be inside QA root')
    vault = Path(env['TRACK_VAULT'])
    if (vault / '.track-native-edit-fixture').read_text() != 'disposable-fixture-only\n':
        raise ValueError('disposable fixture marker required')
    if any(p.is_symlink() for p in vault.rglob('*')):
        raise ValueError('fixture symlinks forbidden')
    return root


class Proxy(http.server.ThreadingHTTPServer):
    daemon_threads = True
    allow_reuse_address = False

    def __init__(self, address, upstream, root):
        super().__init__(address, Handler)
        self.upstream = upstream
        self.root = root
        self.lock = threading.Lock()
        self.stopping = threading.Event()
        self.received_signal = None

    def record(self, event):
        with self.lock:
            with (self.root / 'events.jsonl').open('a') as output:
                output.write(json.dumps(event) + '\n')

    def fault(self):
        with self.lock:
            arm = self.root / 'arm'
            if not arm.exists():
                return ''
            mode = arm.read_text().strip()
            arm.unlink()
            if mode not in ('hold', 'drop'):
                raise ValueError('arm must contain hold or drop')
            (self.root / 'release').unlink(missing_ok=True)
            return mode


class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def do_GET(self):
        self.forward(False)

    def do_PUT(self):
        self.forward(True)

    def forward(self, mutation):
        path = urlsplit(self.path)
        if path.scheme or path.netloc or not path.path.startswith('/api/') or (mutation and path.path != '/api/note'):
            self.send_error(403)
            return
        try:
            size = int(self.headers.get('Content-Length', '0'))
            if size < 0 or size > LIMIT or self.headers.get('Transfer-Encoding'):
                self.send_error(413)
                return
            body = self.rfile.read(size)
            if len(body) != size:
                raise ValueError('incomplete request')
            mode = self.server.fault() if mutation else ''
            conn = http.client.HTTPConnection('127.0.0.1', self.server.upstream, timeout=5)
            try:
                conn.request(self.command, self.path, body=body, headers={
                    'Content-Type': self.headers.get('Content-Type', 'application/json'),
                    'Cache-Control': 'no-cache, no-store',
                })
                response = conn.getresponse()
                status, content_type = response.status, response.getheader('Content-Type', 'application/json')
                payload = response.read(LIMIT + 1)
                if len(payload) > LIMIT:
                    raise ValueError('oversized upstream response')
            finally:
                conn.close()
            if mutation:
                self.server.record({'event': 'put_ack', 'status': status, 'fault': mode})
            if mutation and status == 200 and mode == 'hold':
                self.server.record({'event': 'held'})
                deadline = time.monotonic() + 30
                while not (self.server.root / 'release').exists() and not self.server.stopping.wait(.02):
                    if time.monotonic() >= deadline:
                        break
            if mutation and status == 200 and mode == 'drop':
                self.server.record({'event': 'dropped_ack'})
                self.connection.shutdown(socket.SHUT_RDWR)
                self.close_connection = True
                return
            self.send_response(status)
            self.send_header('Content-Type', content_type)
            self.send_header('Content-Length', str(len(payload)))
            self.end_headers()
            self.wfile.write(payload)
        except (OSError, ValueError, http.client.HTTPException) as error:
            self.server.record({'event': 'proxy_error', 'type': type(error).__name__})
            self.close_connection = True


def reserve_pair():
    for port in range(26000, 30000, 2):
        sockets = []
        try:
            for p in (port, port + 1):
                sock = socket.socket()
                sockets.append(sock)
                sock.bind(('127.0.0.1', p))
            return port, sockets
        except OSError:
            for sock in sockets:
                sock.close()
    raise RuntimeError('no free adjacent upstream ports')


def main():
    root = validate_fixture(os.environ)
    if len(sys.argv) != 4 or sys.argv[1:3] != ['web', '--addr']:
        raise ValueError('adapter only accepts web --addr 127.0.0.1:PORT')
    host, raw_port = sys.argv[3].split(':')
    port = int(raw_port)
    if host != '127.0.0.1' or port in (19022, 19062, 19082) or not 1024 <= port < 65535:
        raise ValueError('use a dedicated loopback QA port')
    binary = Path(os.environ['TRACK_QA_REAL_BINARY'])
    if not binary.is_absolute() or not binary.is_file():
        raise ValueError('absolute real Track binary required')
    parent = int(os.environ['TRACK_WEB_DESKTOP_PARENT_PID'])
    upstream, reservations = reserve_pair()
    # Own the second public port too; never share/adopt an existing listener.
    second = socket.socket()
    second.bind((host, port + 1))
    proxy = Proxy((host, port), upstream, root)
    for sock in reservations:
        sock.close()
    # Install termination handling before any child readiness can reach GPUI.
    def stop(signum, _frame):
        # Signal handlers must not acquire the audit lock (may interrupt record).
        proxy.received_signal = signum
        proxy.stopping.set()
    signal.signal(signal.SIGTERM, stop)
    signal.signal(signal.SIGINT, stop)
    env = dict(os.environ, TRACK_WEB_DESKTOP_PARENT_PID=str(os.getpid()))
    child = subprocess.Popen([str(binary), 'web', '--addr', f'127.0.0.1:{upstream}'], env=env,
                             stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
    def drain():
        for line in iter(child.stderr.readline, b''):
            # Forward owned child's readiness token, not arbitrary logs or note data.
            expected = ('TRACK_DESKTOP_READY:' + env['TRACK_WEB_DESKTOP_READY_TOKEN']).encode()
            if line.strip() == expected:
                sys.stderr.buffer.write(line)
                sys.stderr.buffer.flush()
        child.stderr.close()
    threading.Thread(target=drain, daemon=True).start()
    threading.Thread(target=proxy.serve_forever, daemon=True).start()
    proxy.record({'event': 'started', 'adapter_pid': os.getpid(), 'child_pid': child.pid, 'port': port, 'upstream': upstream})
    try:
        while not proxy.stopping.wait(.05) and child.poll() is None:
            if os.getppid() != parent:
                proxy.record({'event': 'parent_changed', 'parent_pid': os.getppid(), 'expected_parent': parent})
                break
    finally:
        cleanup_started = time.monotonic()
        proxy.record({'event': 'cleanup_enter', 'signal': proxy.received_signal, 'child_status': child.poll()})
        proxy.stopping.set()
        if child.poll() is None:
            child.terminate()
            proxy.record({'event': 'child_term_sent'})
            try:
                child.wait(timeout=.2)
            except subprocess.TimeoutExpired:
                proxy.record({'event': 'child_kill_sent'})
                child.kill()
        child.wait(timeout=.1)
        # Record actual wait completion before listener cleanup. Outer GPUI grace is .7s.
        proxy.record({'event': 'reaped', 'status': child.returncode, 'elapsed_ms': round((time.monotonic() - cleanup_started) * 1000)})
        proxy.server_close()
        second.close()


if __name__ == '__main__':
    try:
        main()
    except BaseException as error:
        # Exception type only: no request content, paths, or readiness secrets.
        try:
            root = Path(os.environ['TRACK_QA_ROOT'])
            with (root / 'events.jsonl').open('a') as output:
                output.write(json.dumps({'event': 'adapter_exception', 'type': type(error).__name__}) + '\n')
        except Exception:
            pass
        raise
