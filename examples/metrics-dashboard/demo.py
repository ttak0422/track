#!/usr/bin/env python3
"""Open the synthetic dashboard in an isolated temporary vault. No external services."""
import argparse
from datetime import date, datetime, timezone
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile


def main():
    example = Path(__file__).resolve().parent
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--track", default="track", help="Track CLI executable")
    parser.add_argument("--port", type=int, default=18765, help="Local web port")
    args = parser.parse_args()
    track = shutil.which(args.track)
    if not track:
        parser.error("Track CLI not found; build it and pass --track ./result/bin/track")
    track = str(Path(track).resolve())
    if not 1 <= args.port <= 65535:
        parser.error("port must be in 1..65535")

    rows = [json.loads(line) for line in (example / "data/observations.jsonl").read_text().splitlines()]
    latest = max(date.fromisoformat(row["time"]) for row in rows)
    shift = datetime.now(timezone.utc).date() - latest
    for row in rows:
        row["time"] = str(date.fromisoformat(row["time"]) + shift)

    with tempfile.TemporaryDirectory(prefix="track-metrics-demo-") as folder:
        vault = Path(folder)
        (vault / "data").mkdir()
        (vault / ".track").mkdir()
        (vault / ".track/config.yml").write_text("journal: false\ngen: false\nweb:\n  home: Cache observations\n")
        (vault / "data/observations.jsonl").write_text("".join(json.dumps(row) + "\n" for row in rows))
        env = {key: value for key, value in os.environ.items() if not key.startswith("TRACK_")}
        env.update(TRACK_CONFIG=str(vault / "machine.yml"), TRACK_VAULT=folder,
                   TRACK_CACHE_DIR=str(vault / "cache"))
        subprocess.run([track, "new", "--title", "Cache observations"],
                       input=(example / "dashboard.md").read_text(), text=True, env=env, check=True)
        print("Synthetic data only. Latest sample shifted to today (UTC); values stay unchanged.", flush=True)
        print("Open http://127.0.0.1:%d — Ctrl+C stops the server and removes this temporary vault." % args.port, flush=True)
        try:
            subprocess.run([track, "web", "--addr", "127.0.0.1:%d" % args.port], env=env, check=True)
        except KeyboardInterrupt:
            pass


if __name__ == "__main__":
    main()
