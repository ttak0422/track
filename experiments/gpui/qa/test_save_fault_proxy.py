import http.client
import http.server
import json
import os
import select
import socket
import subprocess
import sys
from pathlib import Path
import tempfile
import threading
import time
import unittest
from save_fault_proxy import Proxy, validate_fixture


class Upstream(http.server.BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def do_GET(self):
        self.reply(200, self.server.body)

    def do_PUT(self):
        body = self.rfile.read(int(self.headers['Content-Length']))
        self.server.puts.append((self.path, body))
        value = json.loads(body)
        if value['etag'] != self.server.etag:
            self.reply(409, b'{"error":"conflict"}')
            return
        self.server.body = body
        self.server.etag = 'new-tag'
        self.reply(200, b'{"saved":true,"etag":"new-tag"}')

    def reply(self, status, body):
        self.send_response(status)
        self.send_header('Content-Length', str(len(body)))
        self.end_headers()
        self.wfile.write(body)


class FaultTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='track-fault-test-')
        self.root = Path(self.temp.name)
        self.upstream = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Upstream)
        self.upstream.puts = []
        self.upstream.body = b'{}'
        self.upstream.etag = 'old-tag'
        self.proxy = Proxy(('127.0.0.1', 0), self.upstream.server_port, self.root)
        for server in (self.upstream, self.proxy):
            threading.Thread(target=server.serve_forever, daemon=True).start()

    def tearDown(self):
        self.proxy.stopping.set()
        for server in (self.proxy, self.upstream):
            server.shutdown()
            server.server_close()
        self.temp.cleanup()

    def request(self, method, path, body=None):
        connection = http.client.HTTPConnection('127.0.0.1', self.proxy.server_port, timeout=2)
        try:
            connection.request(method, path, body=body)
            response = connection.getresponse()
            return response.status, response.read()
        finally:
            connection.close()

    def test_pending_ack_release_and_exactly_one_unicode_put(self):
        body = json.dumps({'body': '日本😀\n\n', 'etag': 'old-tag'}, ensure_ascii=False).encode()
        (self.root / 'arm').write_text('hold')
        result = []
        thread = threading.Thread(target=lambda: result.append(self.request('PUT', '/api/note?id=42&vault=work', body)))
        thread.start()
        deadline = time.monotonic() + 2
        while not (self.root / 'events.jsonl').exists() or 'held' not in (self.root / 'events.jsonl').read_text():
            self.assertLess(time.monotonic(), deadline)
            time.sleep(.01)
        self.assertTrue(thread.is_alive())
        self.assertEqual(self.upstream.puts, [('/api/note?id=42&vault=work', body)])
        (self.root / 'release').touch()
        thread.join(2)
        self.assertEqual(result, [(200, b'{"saved":true,"etag":"new-tag"}')])
        self.assertEqual(len(self.upstream.puts), 1)

    def test_dropped_ack_is_uncertain_readback_and_conflict_remain_real(self):
        body = b'{"body":"committed","etag":"old-tag"}'
        (self.root / 'arm').write_text('drop')
        with self.assertRaises((http.client.RemoteDisconnected, ConnectionResetError)):
            self.request('PUT', '/api/note?id=42', body)
        self.assertEqual(len(self.upstream.puts), 1)
        self.assertEqual(self.request('GET', '/api/note?id=42'), (200, body))
        self.assertEqual(self.request('PUT', '/api/note?id=42', body)[0], 409)
        self.assertEqual(len(self.upstream.puts), 2)  # explicit second call, never an adapter retry

    def test_other_mutations_and_external_proxy_urls_are_forbidden(self):
        self.assertEqual(self.request('PUT', '/api/journal', b'{}')[0], 403)
        self.assertEqual(self.request('GET', 'http://example.com/api/note')[0], 403)
        self.assertEqual(self.request('POST', '/api/journal', b'{}')[0], 501)
        self.assertEqual(self.upstream.puts, [])

    def test_adapter_owned_child_shutdown_and_reap(self):
        self.check_adapter_shutdown(False)

    def test_sigterm_resistant_child_reaped_before_outer_deadline(self):
        self.check_adapter_shutdown(True)

    def check_adapter_shutdown(self, resist_sigterm):
        vault = self.root / 'vault'
        vault.mkdir()
        (vault / '.track-native-edit-fixture').write_text('disposable-fixture-only\n')
        config = self.root / 'config.yml'
        config.write_text('{}\n')
        cache = self.root / 'cache'
        cache.mkdir()
        binary = self.root / 'fake-track'
        binary.write_text('#!' + sys.executable + '\nimport os,sys,time,signal\n' + ('signal.signal(signal.SIGTERM, signal.SIG_IGN)\n' if resist_sigterm else '') + 'print("TRACK_DESKTOP_READY:" + os.environ["TRACK_WEB_DESKTOP_READY_TOKEN"], file=sys.stderr, flush=True)\ntime.sleep(30)\n')
        binary.chmod(0o700)
        # Dedicated test range distinct from adapter upstream range and live QA ports.
        port = None
        for candidate in range(24000, 25000, 2):
            a, b = socket.socket(), socket.socket()
            try:
                a.bind(('127.0.0.1', candidate))
                b.bind(('127.0.0.1', candidate + 1))
                port = candidate
                break
            except OSError:
                pass
            finally:
                a.close()
                b.close()
        self.assertIsNotNone(port)
        env = dict(os.environ, TRACK_QA_ROOT=str(self.root), TRACK_VAULT=str(vault),
                   TRACK_CONFIG=str(config), TRACK_CACHE_DIR=str(cache),
                   TRACK_QA_REAL_BINARY=str(binary), TRACK_WEB_DESKTOP_PARENT_PID=str(os.getpid()),
                   TRACK_WEB_DESKTOP_READY_TOKEN='synthetic-test-token')
        proc = subprocess.Popen([sys.executable, str(Path(__file__).with_name('save_fault_proxy.py')),
                                 'web', '--addr', f'127.0.0.1:{port}'], env=env,
                                stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
        try:
            self.assertTrue(select.select([proc.stderr], [], [], 3)[0], 'no readiness')
            self.assertEqual(proc.stderr.readline().strip(), b'TRACK_DESKTOP_READY:synthetic-test-token')
            started = time.monotonic()
            proc.terminate()
            self.assertEqual(proc.wait(timeout=.7), 0)
            self.assertLess(time.monotonic() - started, .7)
            events = [json.loads(line) for line in (self.root / 'events.jsonl').read_text().splitlines()]
            self.assertEqual(events[-1]['event'], 'reaped')
            if resist_sigterm:
                self.assertEqual(events[-1]['status'], -9)
            child_pid = next(e['child_pid'] for e in events if e['event'] == 'started')
            with self.assertRaises(ProcessLookupError):
                os.kill(child_pid, 0)
            for p in (port, port + 1):
                with socket.socket() as sock:
                    sock.bind(('127.0.0.1', p))
        finally:
            if proc.poll() is None:
                proc.terminate()
                proc.wait(timeout=2)
            proc.stderr.close()

    def test_unmarked_or_outside_fixture_is_rejected(self):
        vault = self.root / 'vault'
        vault.mkdir()
        config = self.root / 'config.yml'
        config.write_text('{}\n')
        cache = self.root / 'cache'
        cache.mkdir()
        env = {'TRACK_QA_ROOT': str(self.root), 'TRACK_VAULT': str(vault), 'TRACK_CONFIG': str(config), 'TRACK_CACHE_DIR': str(cache)}
        with self.assertRaises(FileNotFoundError):
            validate_fixture(env)
        (vault / '.track-native-edit-fixture').write_text('disposable-fixture-only\n')
        self.assertEqual(validate_fixture(env), self.root.resolve())
        with self.assertRaises(ValueError):
            validate_fixture(dict(env, TRACK_VAULT='/'))
        (vault / 'escape').symlink_to(config)
        with self.assertRaises(ValueError):
            validate_fixture(env)


if __name__ == '__main__':
    unittest.main()
