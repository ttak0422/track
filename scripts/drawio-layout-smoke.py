#!/usr/bin/env python3
"""Exercise the production viewport adapter with synthetic data in WKWebView or Chromium.

Run after npm ci in web/. Default: macOS WKWebView; --engine chromium requires Playwright.
All fixtures, compiler caches and the owned loopback server are temporary.
"""
import argparse
import functools
import http.server
import os
import shutil
import subprocess
import tempfile
import threading
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent


class Handler(http.server.SimpleHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def end_headers(self):
        self.send_header("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline' 'unsafe-eval'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'")
        super().end_headers()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--engine", choices=["wkwebview", "chromium"], default="wkwebview")
    args = parser.parse_args()
    with tempfile.TemporaryDirectory(prefix="track-drawio-layout-") as temporary:
        root = Path(temporary)
        for source, target in [("web/public/drawio-viewer-static.min.js", "viewer.js"),
                               ("web/src/styles.css", "track.css"),
                               ("scripts/drawio-layout-probe.js", "probe.js")]:
            shutil.copyfile(ROOT / source, root / target)
        # Transpile the real adapter, rather than maintaining a second implementation in the probe.
        subprocess.run(["node", "--input-type=module", "-e", """
import ts from './web/node_modules/typescript/lib/typescript.js';
import fs from 'node:fs';
fs.writeFileSync(process.argv[1], ts.transpile(fs.readFileSync('web/src/components/markdown/drawioViewport.ts','utf8'),
  {module:ts.ModuleKind.ESNext,target:ts.ScriptTarget.ES2022}));
""", str(root / "viewport.js")], cwd=ROOT, check=True)
        (root / "index.html").write_text('''<!doctype html><meta charset="utf-8">
<link rel="stylesheet" href="/track.css"><style>.markdown-view{--content-measure:100%;width:1100px}</style>
<div class="markdown-view"></div><script>
window.addEventListener('error',event=>window.webkit?.messageHandlers.result.postMessage('FAIL: '+event.message));
window.addEventListener('unhandledrejection',event=>window.webkit?.messageHandlers.result.postMessage('FAIL: '+String(event.reason)));
window.MathJax={};window.PROXY_URL=window.STYLE_PATH=window.SHAPES_PATH=window.STENCIL_PATH=window.DRAW_MATH_URL='/absent';
</script><script src="/viewer.js"></script><script type="module">
import '/probe.js';
window.probe.then(result=>window.webkit?.messageHandlers.result.postMessage(result))
.catch(error=>window.webkit?.messageHandlers.result.postMessage('FAIL: '+error.message));
</script>''', encoding="utf-8")
        server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), functools.partial(Handler, directory=str(root)))
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        url = f"http://127.0.0.1:{server.server_port}/index.html"
        try:
            if args.engine == "chromium":
                from playwright.sync_api import sync_playwright
                with sync_playwright() as playwright:
                    browser = playwright.chromium.launch(headless=True)
                    try:
                        for ratio in [1, 1.25, 2]:
                            page = browser.new_page(viewport={"width": 1200, "height": 1000}, device_scale_factor=ratio)
                            page.route("**/*", lambda route: route.continue_() if route.request.url.startswith(url.rsplit('/', 1)[0] + '/') else route.abort())
                            page.goto(url, wait_until="networkidle")
                            print(f"Chromium DPR {ratio}: {page.evaluate('window.probe')}")
                            page.close()
                    finally:
                        browser.close()
            else:
                environment = dict(os.environ, CLANG_MODULE_CACHE_PATH=str(root / "modules"))
                subprocess.run(["bash", str(ROOT / "scripts/apple-toolchain.sh"), "desktop", "swiftc",
                                "-module-cache-path", str(root / "modules"), "-framework", "AppKit", "-framework", "WebKit",
                                str(ROOT / "desktop/Tests/DrawioLayout/main.swift"), "-o", str(root / "probe")],
                               env=environment, check=True, timeout=90)
                subprocess.run([str(root / "probe"), url], env=environment, check=True, timeout=45)
        finally:
            server.shutdown()
            server.server_close()
            thread.join()


if __name__ == "__main__":
    main()
