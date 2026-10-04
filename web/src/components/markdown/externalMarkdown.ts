import type { Root } from "mdast";
import { visit } from "unist-util-visit";
import { headingElementID, headingSlug } from "./toc";

// These checks classify authored references, not filesystem permissions. The caller still resolves
// relative paths against its explicitly selected FileList and must refuse anything outside it.
export type ExternalMarkdownLink =
  | { kind: "fragment"; href: string }
  | { kind: "web"; href: string }
  | { kind: "file"; href: string };

function decodedReference(value: string): string | undefined {
  if (!value || value.trim() !== value || /[\u0000-\u001f\u007f\\]/.test(value)) return undefined;
  try {
    const decoded = decodeURIComponent(value);
    return /[\u0000-\u001f\u007f\\]/.test(decoded) ? undefined : decoded;
  } catch {
    return undefined;
  }
}

export function isRelativeMarkdownReference(value: string): boolean {
  const decoded = decodedReference(value);
  if (!decoded || decoded.trim() !== decoded || /^[/?#]/.test(decoded)) return false;
  // Encoded schemes and slashes are checked after decoding too. Never promote a domain-like
  // filename to a web URL, or pass an absolute/API/action URL to the local file resolver.
  return !/^[a-z][a-z\d+.-]*:/i.test(decoded);
}

export function externalMarkdownLink(value: string): ExternalMarkdownLink | undefined {
  const decoded = decodedReference(value);
  if (!decoded) return undefined;
  if (value.startsWith("#")) return { kind: "fragment", href: value };
  if (/^https?:\/\//i.test(value)) {
    try {
      const parsed = new URL(value);
      if (parsed.protocol === "http:" || parsed.protocol === "https:") {
        return { kind: "web", href: value };
      }
    } catch {
      return undefined;
    }
  }
  return isRelativeMarkdownReference(value) ? { kind: "file", href: value } : undefined;
}

export function isRelativeMarkdownImage(value: string): boolean {
  if (!isRelativeMarkdownReference(value)) return false;
  const path = decodeURIComponent(value).split(/[?#]/, 1)[0];
  return !/\.svgz?$/i.test(path);
}

export interface ExternalMarkdownImage {
  // A parent-created URL for a size/signature-validated raster file. The parent owns revocation.
  src: string;
  mimeType: string;
}

const rasterTypes = new Set([
  "image/png",
  "image/jpeg",
  "image/gif",
  "image/webp",
  "image/avif",
  "image/bmp",
  "image/x-icon",
  "image/vnd.microsoft.icon",
]);

export function isExternalMarkdownImage(value: ExternalMarkdownImage | undefined): value is ExternalMarkdownImage {
  return !!value && /^blob:.+/.test(value.src) && rasterTypes.has(value.mimeType);
}

// The same helper serves same-document links and the parent's post-load, cross-file navigation.
// Scope the lookup to this document so authored anchors cannot target workspace controls.
export function scrollExternalMarkdownFragment(root: HTMLElement, rawFragment: string): boolean {
  let fragment: string;
  try {
    fragment = decodeURIComponent(rawFragment.replace(/^#/, ""));
  } catch {
    return false;
  }
  const ids = [fragment, headingElementID(fragment)];
  const target = [...root.querySelectorAll("[id]")].find((element) => ids.includes(element.id));
  if (!target) return false;
  target.scrollIntoView({ block: "start" });
  return true;
}

// External READMEs commonly use Setext headings, so scan the actual CommonMark heading nodes rather
// than the vault's deliberately ATX-only outline. Raw HTML remains text, never a heading/DOM id.
export function remarkExternalHeadingIDs() {
  return (tree: Root) => {
    const used = new Set<string>();
    visit(tree, "heading", (node) => {
      const base = headingSlug(headingText(node));
      let id = base;
      let n = 1;
      while (used.has(id)) id = `${base}-${++n}`;
      used.add(id);
      const data = (node.data ??= {});
      data.hProperties = { ...data.hProperties, id: headingElementID(id) };
    });
  };
}

function headingText(node: { type: string; value?: string; alt?: string | null; children?: Parameters<typeof headingText>[0][] }): string {
  if (node.type === "html") return "";
  if (node.type === "image") return node.alt ?? "";
  return node.value ?? node.children?.map(headingText).join("") ?? "";
}
