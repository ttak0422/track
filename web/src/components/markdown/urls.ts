// Pure URL/href helpers shared by the markdown link and embed rendering. None of these touch React or
// the DOM, so they are unit-tested directly.
import { STATIC_MODE } from "../../runtime";

// noteCandidateFromHref turns a markdown link target into a track note title to resolve, or "" when the
// href is an anchor, an explicit scheme (http:, mailto:, …), or otherwise not a vault note reference.
export function noteCandidateFromHref(href: string): string {
  const trimmed = href.trim();
  if (
    trimmed === "" ||
    trimmed.startsWith("#") ||
    /^[a-z][a-z0-9+.-]*:/i.test(trimmed)
  ) {
    return "";
  }
  const withoutHash = trimmed.split("#", 1)[0] ?? "";
  const withoutQuery = withoutHash.split("?", 1)[0] ?? "";
  const withoutExt = withoutQuery.replace(/\.md$/i, "");
  try {
    return decodeURIComponent(withoutExt).trim();
  } catch {
    return withoutExt.trim();
  }
}

// webHref upgrades a bare domain ("www.x.com", "example.com/path") to an https URL, leaving anything that
// already has a scheme (or is not domain-like) untouched.
export function webHref(href: string): string {
  const trimmed = href.trim();
  if (/^www\./i.test(trimmed) || /^[\w.-]+\.[a-z]{2,}(?:[/:?#]|$)/i.test(trimmed)) {
    return `https://${trimmed}`;
  }
  return href;
}

// assetHref maps a note-relative attachment reference ("assets/<file>", optionally written "./assets/…")
// to the local server endpoint that serves it from the vault's per-kind assets directory. It returns
// null for anything that is not such a reference (absolute URLs, schemes, anchors, root-absolute paths),
// leaving those to the normal link/embed handling.
export function assetHref(src: string, kind: string): string | null {
  const trimmed = src.trim();
  if (
    trimmed === "" ||
    trimmed.startsWith("/") ||
    trimmed.startsWith("#") ||
    /^[a-z][a-z0-9+.-]*:/i.test(trimmed)
  ) {
    return null;
  }
  const rel = trimmed.replace(/^\.\//, "");
  if (!rel.startsWith("assets/")) {
    return null;
  }
  const name = rel.slice("assets/".length);
  if (name === "") {
    return null;
  }
  // The static export copies attachments to ./assets/<name>, so reference them relatively instead of the
  // live server's /api/asset endpoint.
  if (STATIC_MODE) {
    return `assets/${name}`;
  }
  const params = new URLSearchParams({ kind: kind || "note", name });
  return `/api/asset?${params}`;
}

// tweetIdFromUrl returns the numeric status id of a Twitter/X post URL, or null for any other URL.
export function tweetIdFromUrl(src: string): string | null {
  let url: URL;
  try {
    url = new URL(webHref(src));
  } catch {
    return null;
  }
  const host = url.hostname.replace(/^www\./i, "").replace(/^mobile\./i, "").toLowerCase();
  if (host !== "twitter.com" && host !== "x.com") {
    return null;
  }
  return /^\/[^/]+\/status(?:es)?\/(\d+)/.exec(url.pathname)?.[1] ?? null;
}

export function hostOf(url: string): string {
  try {
    return new URL(url).hostname;
  } catch {
    return url;
  }
}

export function isImageHref(src: string): boolean {
  const path = src.split(/[?#]/, 1)[0] ?? "";
  return /\.(png|jpe?g|gif|webp|avif|svg|bmp|ico)$/i.test(path.trim());
}

// youtubeEmbedUrl turns a YouTube watch/share/shorts/embed URL into a privacy-enhanced embed URL,
// carrying a start time when the original had one (t= or start=). It returns null for non-YouTube URLs
// so the caller can fall back to a PDF/image embed.
export function youtubeEmbedUrl(src: string): string | null {
  let url: URL;
  try {
    url = new URL(webHref(src));
  } catch {
    return null;
  }
  const host = url.hostname.replace(/^www\./i, "").toLowerCase();
  let id = "";
  if (host === "youtu.be") {
    id = url.pathname.slice(1).split("/")[0] ?? "";
  } else if (host === "youtube.com" || host === "m.youtube.com" || host === "youtube-nocookie.com") {
    if (url.pathname === "/watch") {
      id = url.searchParams.get("v") ?? "";
    } else {
      id = /^\/(?:embed|shorts|live|v)\/([^/?#]+)/.exec(url.pathname)?.[1] ?? "";
    }
  }
  if (!/^[\w-]{6,}$/.test(id)) {
    return null;
  }
  const start = youtubeStartSeconds(url.searchParams.get("t") ?? url.searchParams.get("start"));
  const query = start > 0 ? `?start=${start}` : "";
  return `https://www.youtube-nocookie.com/embed/${id}${query}`;
}

// youtubeStartSeconds parses a YouTube timestamp, accepting plain seconds ("90") and the 1h2m3s form.
export function youtubeStartSeconds(raw: string | null): number {
  if (!raw) {
    return 0;
  }
  if (/^\d+$/.test(raw)) {
    return Number(raw);
  }
  const match = /^(?:(\d+)h)?(?:(\d+)m)?(?:(\d+)s)?$/.exec(raw);
  if (!match || (!match[1] && !match[2] && !match[3])) {
    return 0;
  }
  return Number(match[1] ?? 0) * 3600 + Number(match[2] ?? 0) * 60 + Number(match[3] ?? 0);
}

export function isPdfHref(src: string): boolean {
  const path = src.split(/[?#]/, 1)[0] ?? "";
  return /\.pdf$/i.test(path.trim());
}

// isMermaidHref matches a Mermaid source file by extension, so an embedded ![](assets/chart.mmd) is
// rendered as a diagram (the same renderer fenced ```mermaid blocks use) instead of a broken image.
export function isMermaidHref(src: string): boolean {
  const path = src.split(/[?#]/, 1)[0] ?? "";
  return /\.(mmd|mermaid)$/i.test(path.trim());
}

// textAssetLangs maps a text-file asset extension to how its embed should render: "mermaid" renders a
// diagram, every other entry is a CodeBlock language ("" means plain text, shown without highlighting).
const textAssetLangs: Record<string, string> = {
  mmd: "mermaid",
  mermaid: "mermaid",
  txt: "",
  text: "",
  log: "",
  csv: "csv",
  tsv: "tsv",
  json: "json",
  yaml: "yaml",
  yml: "yaml",
  toml: "toml",
  xml: "xml",
  ini: "ini",
  conf: "ini",
  env: "",
  sh: "bash",
  bash: "bash",
  zsh: "bash",
  dot: "dot",
  gv: "dot",
  puml: "plantuml",
  plantuml: "plantuml",
};

// textAssetLang returns the render language for a text-file asset embed, or null when the extension is
// not one we inline (image/PDF/remote links are handled elsewhere).
export function textAssetLang(src: string): string | null {
  const path = src.split(/[?#]/, 1)[0] ?? "";
  const ext = /\.([a-z0-9]+)$/i.exec(path.trim())?.[1]?.toLowerCase() ?? "";
  return ext in textAssetLangs ? textAssetLangs[ext] : null;
}

// isTextAssetHref reports whether src names a text-file asset we render inline (mermaid diagram or code
// block), as opposed to an image/PDF/remote link handled elsewhere.
export function isTextAssetHref(src: string): boolean {
  return textAssetLang(src) !== null;
}

// safeFrameUrl returns the URL only when it is safe to load in an iframe: http(s) or a same-origin
// relative path. It rejects javascript:/data: and other schemes that could run script in the frame.
export function safeFrameUrl(target: string): string | null {
  const trimmed = target.trim();
  if (/^https?:\/\//i.test(trimmed) || trimmed.startsWith("/") || trimmed.startsWith("./")) {
    return trimmed;
  }
  return null;
}
