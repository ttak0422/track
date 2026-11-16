#!/usr/bin/env python3
"""Prepare an isolated transport test crate, not a GPUI/native acceptance build."""
import argparse
import json
from pathlib import Path
import tomllib

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("output", type=Path, help="new, nonexistent harness directory")
args = parser.parse_args()
experiment = Path(__file__).resolve().parents[1]
source = experiment / "src"
output = args.output.resolve()
output.mkdir(parents=True, exist_ok=False)

modules = ["live", "editing", "live_events", "event_awareness", "session_store", "backend", "markdown_handoff",
           "editor_follow", "numeric_chart", "inline_viewspec", "included_notes", "raster",
           "pdf_probe", "graph", "tabs", "navigation", "draft_recovery", "recent_notes"]
lib = "#![allow(dead_code)]\n" + "".join(
    f"#[path = {json.dumps(str(source / (name + '.rs')), ensure_ascii=False)}]\nmod {name};\n" for name in modules)


def excerpt(filename, start, end):
    text = (source / filename).read_text()
    if text.count(start) != 1 or text.count(end) != 1:
        raise ValueError(f"source extraction boundary changed: {filename}")
    return text[text.index(start):text.index(end, text.index(start))]


# Actual independent helpers, not success-returning replacements. The surrounding
# UI implementations are deliberately excluded from this transport-only crate.
lib += "mod workspace_edit { use std::path::{Path, PathBuf};\n" + excerpt(
    "workspace_edit.rs", "pub fn validate_fixture(", "\nfn fixture_note(") + "\n}\n"
lib += "mod workspace_activity { use crate::live;\n" + excerpt(
    "workspace_activity.rs", "#[derive(Clone, Copy, Debug, PartialEq, Eq)]\nstruct Day",
    "\nfn adjacent_in_range") + "\n}\n"
lib += "mod reader_pdf {\n" + excerpt(
    "reader_pdf.rs", "pub fn asset_name(", "\npub fn references(") + "\n}\n"
(output / "lib.rs").write_text(lib)

packages = tomllib.loads((experiment / "Cargo.lock").read_text())["package"]
def version(name):
    versions = {p["version"] for p in packages if p["name"] == name}
    if len(versions) != 1:
        raise ValueError(f"ambiguous lock version: {name}")
    return "=" + versions.pop()

features = {"serde": ["derive"], "reqwest": ["blocking", "json"],
            "tokio": ["rt", "time", "sync", "net"], "image": ["png", "jpeg"]}
toml = '[package]\nname="track-native-bound-harness"\nversion="0.1.0"\nedition="2024"\n[lib]\npath="lib.rs"\n[dependencies]\n'
for name in ["serde", "serde_json", "reqwest", "libc", "tokio", "image"]:
    fields = ["version=" + json.dumps(version(name))]
    if name in {"reqwest", "image"}:
        fields.append("default-features=false")
    if name in features:
        fields.append("features=" + json.dumps(features[name]))
    toml += name + "={" + ",".join(fields) + "}\n"
toml += 'gpui={path="gpui-stub"}\n'
(output / "Cargo.toml").write_text(toml)
stub = output / "gpui-stub"
stub.mkdir()
(stub / "Cargo.toml").write_text('[package]\nname="gpui"\nversion="0.0.0"\nedition="2024"\n[lib]\npath="lib.rs"\n[dependencies]\nimage={version=' + json.dumps(version("image")) + ',default-features=false}\n')
(stub / "lib.rs").write_text('''// Image carrier only. No window, renderer, events or native acceptance proof.
pub struct RenderImage(Vec<image::Frame>);
impl RenderImage {
    pub fn new(frames: impl IntoIterator<Item=image::Frame>) -> Self { Self(frames.into_iter().collect()) }
    pub fn as_bytes(&self, i:usize) -> Option<&[u8]> { self.0.get(i).map(|f| f.buffer().as_raw().as_slice()) }
}
impl std::fmt::Debug for RenderImage {
    fn fmt(&self, f: &mut std::fmt::Formatter) -> std::fmt::Result { f.write_str("HarnessRenderImage") }
}
''')
print(output / "Cargo.toml")
