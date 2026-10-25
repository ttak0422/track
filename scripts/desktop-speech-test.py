#!/usr/bin/env python3
"""Exercise the real WKWebView speech bridge with synthetic transcripts, never real audio."""
import http.server
import os
from pathlib import Path
import subprocess
import threading

ROOT = Path(__file__).resolve().parent.parent


class Fixture(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        body = b'<!doctype html><title>Speech fixture</title>'
        if self.path == '/frame':
            body += b"<script>window.webkit.messageHandlers.trackSpeech.postMessage({action:'start',id:'frame',locale:'ja-JP'})</script>"
        self.send_response(200)
        self.send_header('Content-Type', 'text/html; charset=utf-8')
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *_args):
        pass


def main():
    binary = ROOT / 'build/speech-bridge-tests'
    binary.parent.mkdir(exist_ok=True)
    environment = dict(os.environ, CLANG_MODULE_CACHE_PATH=str(ROOT / 'build/desktop-clang-module-cache'))
    module_dir = ROOT / 'build/speech-test-core'
    module_dir.mkdir(exist_ok=True)
    compiler = ['sh', str(ROOT / 'scripts/apple-toolchain.sh'), 'desktop', 'swiftc', '-swift-version', '6']
    subprocess.run([*compiler, '-parse-as-library', '-emit-library', '-static', '-emit-module',
                    '-module-name', 'TrackWebCore', '-emit-module-path', str(module_dir / 'TrackWebCore.swiftmodule'),
                    str(ROOT / 'desktop/Sources/TrackWebCore/NativeSpeechSession.swift'),
                    '-o', str(module_dir / 'libTrackWebCore.a')], env=environment, check=True)
    subprocess.run([*compiler, '-parse-as-library', '-I', str(module_dir), '-L', str(module_dir), '-lTrackWebCore',
                    str(ROOT / 'desktop/Sources/TrackWebApp/NativeSpeechBridge.swift'),
                    str(ROOT / 'desktop/Tests/SpeechBridgeTests.swift'), '-o', str(binary)],
                   env=environment, check=True)
    servers = [http.server.ThreadingHTTPServer(('127.0.0.1', 0), Fixture) for _ in range(2)]
    try:
        for server in servers:
            threading.Thread(target=server.serve_forever, daemon=True).start()
        subprocess.run([str(binary), *[f'http://127.0.0.1:{server.server_port}' for server in servers]],
                       env=environment, check=True, timeout=45)
    finally:
        for server in servers:
            server.shutdown()
            server.server_close()


if __name__ == '__main__':
    main()
