#!/usr/bin/env python3
"""Map a staged Vite build into Go's embed view without touching tracked dist/ files."""

import json
import sys
from pathlib import Path


def main() -> None:
    if len(sys.argv) != 4:
        raise SystemExit("usage: desktop-embed-overlay.py REPO_ROOT FRONTEND_DIST OVERLAY_JSON")

    root = Path(sys.argv[1]).resolve()
    frontend = Path(sys.argv[2]).resolve()
    overlay_path = Path(sys.argv[3]).resolve()
    embed_dir = root / "internal/track/webui/dist"
    index = frontend / "index.html"

    if not index.is_file():
        raise SystemExit(f"frontend build is missing {index}")
    html = index.read_text(encoding="utf-8")
    if "<script type=\"module\"" not in html or "/assets/" not in html:
        raise SystemExit("refusing to embed the tracked placeholder instead of a Vite production build")

    frontend_files = {
        path.relative_to(frontend).as_posix(): path
        for path in frontend.rglob("*")
        if path.is_file()
    }
    if not any(name.startswith("assets/") and name.endswith((".js", ".mjs")) for name in frontend_files):
        raise SystemExit("frontend build contains no JavaScript bundle under assets/")

    replacements: dict[str, str] = {}
    for relative, source in frontend_files.items():
        destination = embed_dir / relative
        replacements[str(destination.resolve())] = str(source.resolve())

    # Remove stale ignored build files from Go's virtual view; otherwise an old bundle in dist/ could
    # leak into a new app even though the build itself never writes to that directory.
    for destination in embed_dir.rglob("*"):
        if destination.is_file() and destination.relative_to(embed_dir).as_posix() not in frontend_files:
            replacements[str(destination.resolve())] = ""

    overlay_path.parent.mkdir(parents=True, exist_ok=True)
    overlay_path.write_text(json.dumps({"Replace": replacements}, indent=2) + "\n", encoding="utf-8")


if __name__ == "__main__":
    main()
