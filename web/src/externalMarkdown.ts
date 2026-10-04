// A user-chosen set of local files is the entire authority of the external reader. No path is
// handed to the Go server, and no body/filename is persisted in the vault or browser storage.
export const MAX_MARKDOWN_BYTES = 2 * 1024 * 1024;
export const MAX_IMAGE_BYTES = 10 * 1024 * 1024;
const MAX_SELECTION_FILES = 5000;
const MAX_IMAGE_CACHE_BYTES = 32 * 1024 * 1024;
const MAX_IMAGE_CACHE_COUNT = 128;

export function isMarkdownPath(path: string): boolean {
  return /\.(md|markdown)$/i.test(path);
}

const imageExtension = /\.(png|jpe?g|gif|webp)$/i;

export interface ExternalDocument {
  id: string;
  path: string;
  file: File;
}

export interface LocalTarget {
  path: string;
  fragment: string;
}

// URL-decode once, before normalizing. Escaping the selected root, absolute paths, Windows paths,
// URL schemes, queries, and encoded path separators cannot turn a link into extra file authority.
export function resolveLocalTarget(from: string, href: string): LocalTarget | undefined {
  if (!href || /^[\s/\\]/.test(href) || /[\u0000-\u001f\u007f\\]/.test(href)) return;
  const hash = href.indexOf("#");
  const rawPath = hash < 0 ? href : href.slice(0, hash);
  const fragment = hash < 0 ? "" : href.slice(hash + 1);
  if (rawPath.includes("?") || /^[a-z][a-z\d+.-]*:/i.test(rawPath) || /%2f|%5c/i.test(rawPath)) return;
  let decoded: string;
  try { decoded = decodeURIComponent(rawPath); } catch { return; }
  if (/^[\s/\\]/.test(decoded) || /[\u0000-\u001f\u007f\\:]/.test(decoded)) return;
  if (!decoded) return { path: from, fragment };
  const parts = from.split("/").slice(0, -1);
  for (const part of decoded.split("/")) {
    if (part === "." || part === "") continue;
    if (part === "..") {
      if (parts.length === 0) return;
      parts.pop();
    } else {
      parts.push(part);
    }
  }
  return { path: parts.join("/"), fragment };
}

function selectedPath(file: File): string {
  const path = file.webkitRelativePath || file.name;
  // The browser supplies these, not the document. Reject malformed/ambiguous inputs rather than
  // silently letting one selected file shadow another.
  if (!path || path.startsWith("/") || /[\\\u0000-\u001f\u007f]/.test(path) || path.split("/").some((part) => !part || part === "." || part === "..")) {
    throw new Error("The selection contains an unsupported file path.");
  }
  return path;
}

export class ExternalMarkdownSource {
  readonly documents: ExternalDocument[];
  readonly label: string;
  activeDocumentID: string;
  private files = new Map<string, File>();
  private images = new Map<string, Promise<{ src: string; mimeType: string } | undefined>>();
  private urls = new Set<string>();
  private imageBytes = 0;
  private disposed = false;

  constructor(files: readonly File[]) {
    if (files.length > MAX_SELECTION_FILES) throw new Error("Choose a smaller folder (at most 5,000 files).");
    for (const file of files) {
      const path = selectedPath(file);
      if (!isMarkdownPath(path) && !imageExtension.test(path)) continue;
      if (this.files.has(path)) throw new Error(`Two selected files have the same path: ${path}`);
      this.files.set(path, file);
    }
    this.documents = [...this.files].filter(([path]) => isMarkdownPath(path))
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([path, file]) => ({
        // IDs are session navigation keys, not capabilities. randomUUID is secure-context-only,
        // while a deliberately LAN-bound workspace can also serve this local-only reader.
        id: globalThis.crypto?.randomUUID?.() ?? `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`,
        path, file,
      }));
    if (!this.documents.length) throw new Error("Choose a .md or .markdown file, or a folder containing one.");
    const folder = files.find((file) => file.webkitRelativePath)?.webkitRelativePath.split("/")[0];
    this.activeDocumentID = this.initialDocument().id;
    this.label = folder ? `Folder: ${folder}` : `${this.documents.length} selected Markdown file${this.documents.length === 1 ? "" : "s"}`;
  }

  initialDocument(): ExternalDocument {
    // Prefer a root README to a nested dependency's README, independent of selection order.
    return [...this.documents].sort((a, b) => {
      const rank = (path: string) => /(^|\/)readme\.(md|markdown)$/i.test(path) ? path.split("/").length : Number.MAX_SAFE_INTEGER;
      return rank(a.path) - rank(b.path) || a.path.localeCompare(b.path);
    })[0];
  }

  document(id: string): ExternalDocument | undefined {
    return this.documents.find((doc) => doc.id === id);
  }

  linkedDocument(from: string, href: string): { document: ExternalDocument; fragment: string } | undefined {
    const target = resolveLocalTarget(from, href);
    if (!target) return;
    const document = this.documents.find((doc) => doc.path === target.path);
    return document ? { document, fragment: target.fragment } : undefined;
  }

  async read(document: ExternalDocument): Promise<string> {
    if (this.disposed || this.document(document.id) !== document) throw new Error("This source is closed. Select the files again.");
    if (document.file.size > MAX_MARKDOWN_BYTES) throw new Error("This Markdown file exceeds the 2 MiB limit.");
    let bytes: ArrayBuffer;
    try { bytes = await document.file.arrayBuffer(); } catch {
      throw new Error("This file is no longer readable. Select it again to refresh the source.");
    }
    if (this.disposed) throw new Error("This source is closed. Select the files again.");
    if (bytes.byteLength > MAX_MARKDOWN_BYTES) throw new Error("This Markdown file exceeds the 2 MiB limit.");
    try { return new TextDecoder("utf-8", { fatal: true }).decode(bytes); } catch {
      throw new Error("This file is not valid UTF-8 Markdown.");
    }
  }

  image(from: string, href: string): Promise<{ src: string; mimeType: string } | undefined> {
    if (this.disposed) return Promise.resolve(undefined);
    const target = resolveLocalTarget(from, href);
    if (!target || !imageExtension.test(target.path)) return Promise.resolve(undefined);
    const cached = this.images.get(target.path);
    if (cached) return cached;
    const file = this.files.get(target.path);
    if (!file || file.size > MAX_IMAGE_BYTES || this.images.size >= MAX_IMAGE_CACHE_COUNT || this.imageBytes + file.size > MAX_IMAGE_CACHE_BYTES) return Promise.resolve(undefined);
    this.imageBytes += file.size;
    const promise = this.readImage(file).catch(() => undefined);
    this.images.set(target.path, promise);
    return promise;
  }

  private async readImage(file: File): Promise<{ src: string; mimeType: string } | undefined> {
    const bytes = await file.arrayBuffer();
    if (this.disposed || bytes.byteLength > MAX_IMAGE_BYTES || bytes.byteLength !== file.size) return;
    const head = new Uint8Array(bytes, 0, Math.min(bytes.byteLength, 16));
    const ascii = (start: number, end: number) => String.fromCharCode(...head.slice(start, end));
    const mimeType = head[0] === 0x89 && ascii(1, 4) === "PNG" && head[4] === 13 && head[5] === 10 && head[6] === 26 && head[7] === 10 ? "image/png"
      : head[0] === 0xff && head[1] === 0xd8 && head[2] === 0xff ? "image/jpeg"
      : ascii(0, 6) === "GIF87a" || ascii(0, 6) === "GIF89a" ? "image/gif"
      : ascii(0, 4) === "RIFF" && ascii(8, 12) === "WEBP" ? "image/webp" : undefined;
    if (!mimeType) return;
    const src = URL.createObjectURL(new Blob([bytes], { type: mimeType }));
    this.urls.add(src);
    return { src, mimeType };
  }

  dispose(): void {
    this.disposed = true;
    this.urls.forEach((url) => URL.revokeObjectURL(url));
    this.urls.clear();
    this.images.clear();
    this.files.clear();
  }
}
