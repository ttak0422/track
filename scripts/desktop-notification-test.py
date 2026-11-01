#!/usr/bin/env python3
"""Exercise the notification bridge with a mock center; never request OS permission or post notices."""
import http.server
import os
from pathlib import Path
import subprocess
import threading

ROOT = Path(__file__).resolve().parent.parent


class Fixture(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        body = b'<!doctype html><title>Notification fixture</title>'
        if self.path == '/frame':
            body += b"<script>window.webkit.messageHandlers.trackNotifications.postMessage({action:'activity',noteID:'frame',day:'2026-11-01'}).finally(()=>window.finished=true)</script>"
        if self.path.startswith('/api/asset'):
            body += b"<script>try{parent.__trackNativeNotifications.postMessage({action:'activity',noteID:'asset',day:'2026-11-01'})}catch(e){parent.postMessage('asset-blocked','*')}</script>"
        else:
            body += b"<script>addEventListener('message',e=>{if(e.data==='asset-blocked')window.assetBlocked=true})</script>"
        self.send_response(200)
        if self.path.split('?')[0] == '/api/asset':
            self.send_header('Content-Security-Policy', 'sandbox allow-scripts allow-downloads allow-modals allow-popups allow-popups-to-escape-sandbox')
        self.send_header('Content-Type', 'text/html; charset=utf-8')
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *_args):
        pass


def main():
    binary = ROOT / 'build/notification-bridge-tests'
    binary.parent.mkdir(exist_ok=True)
    environment = dict(os.environ, CLANG_MODULE_CACHE_PATH=str(ROOT / 'build/desktop-clang-module-cache'))
    subprocess.run(['sh', str(ROOT / 'scripts/apple-toolchain.sh'), 'desktop', 'swiftc',
                    '-swift-version', '6', '-parse-as-library',
                    str(ROOT / 'desktop/Sources/TrackWebApp/NativeNotificationBridge.swift'),
                    str(ROOT / 'desktop/Tests/NotificationBridgeTests.swift'), '-o', str(binary)],
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
