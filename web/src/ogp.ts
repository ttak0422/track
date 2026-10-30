import type { OgpResponse } from "./types";

export async function browserOgp(url: string): Promise<OgpResponse> {
  try {
    const response = await fetch(url);
    const type = response.headers.get("content-type") ?? "";
    if (!response.ok || (type !== "" && !type.toLowerCase().includes("html"))) {
      return { url };
    }
    // response.url is the final URL after redirects, which is what a relative og:image resolves against.
    return parseOgp(await response.text(), response.url || url, url);
  } catch {
    // A refused cross-origin read or an offline reader: the bare card, never a dead end.
    return { url };
  }
}

// parseOgp mirrors the server's parser (internal/track/webui/ogp.go): the same tag precedence, the same
// relative-image resolution and http(s)-only guard, the same lengths — so a card reads the same whether
// its metadata came from the server or from the reader's own browser. Parsing through DOMParser gives an
// inert document: no script runs and no subresource loads, so the fetched page cannot act here.
export function parseOgp(html: string, pageURL: string, url: string): OgpResponse {
  // Only <head> is scanned, so body content is never mistaken for metadata.
  const head = new DOMParser().parseFromString(html, "text/html").head;
  const props = new Map<string, string>();
  for (const tag of head.querySelectorAll("meta")) {
    const key = (tag.getAttribute("property") ?? tag.getAttribute("name") ?? "").toLowerCase();
    const content = tag.getAttribute("content");
    // First value wins, so a page's primary og:* tag is not overwritten by a later duplicate.
    if (key === "" || content === null || props.has(key)) continue;
    props.set(key, content.trim());
  }
  const first = (...keys: string[]) => keys.map((key) => props.get(key) ?? "").find((value) => value !== "") ?? "";

  const result: OgpResponse = { url };
  const title = first("og:title", "twitter:title") || (head.querySelector("title")?.textContent ?? "").trim();
  const description = first("og:description", "twitter:description", "description");
  const image = absoluteImage(first("og:image", "og:image:url", "twitter:image", "twitter:image:src"), pageURL);
  const siteName = first("og:site_name") || hostname(pageURL);
  if (title !== "") result.title = clip(title, 200);
  if (description !== "") result.description = clip(description, 320);
  if (image !== "") result.image = image;
  if (siteName !== "") result.site_name = siteName;
  return result;
}

// absoluteImage resolves a possibly-relative image reference against the page, dropping anything that is
// not http(s) so the card never renders a javascript:/data: image source.
function absoluteImage(ref: string, pageURL: string): string {
  if (ref === "") return "";
  try {
    const resolved = new URL(ref, pageURL);
    return resolved.protocol === "http:" || resolved.protocol === "https:" ? resolved.href : "";
  } catch {
    return "";
  }
}

function hostname(pageURL: string): string {
  try {
    return new URL(pageURL).hostname;
  } catch {
    return "";
  }
}

// clip counts code points, like the server's rune-based truncation, so a surrogate pair is never split.
function clip(text: string, limit: number): string {
  const points = [...text];
  return points.length > limit ? points.slice(0, limit).join("") : text;
}
