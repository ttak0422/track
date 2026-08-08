// The published data bundle is locked (ADR 0069): each file under data/ is gzipped and AES-256-GCM
// encrypted by `track export-site`, and served as raw bytes (<name>.bin). Reading it takes a specific
// conversion with the site's key, which the export bakes into the page (window.__trackLock). This module
// is that conversion — the only place the frontend opens published data, and the mirror of
// internal/track/site/lock.go.
//
// Layout: nonce (12 bytes) || AES-256-GCM(gzip(plaintext)).

import { reportStalePage } from "./runtime";

const NONCE_BYTES = 12;

let cached: Promise<CryptoKey> | undefined;

// siteKey imports the page's key. Read lazily (not at module scope) because the prerender sets
// window.__trackLock after loading this module's bundle.
function siteKey(): Promise<CryptoKey> {
  cached ??= (async () => {
    const raw = typeof window === "undefined" ? "" : window.__trackLock;
    if (!raw || raw.startsWith("__TRACK_")) {
      throw new Error("no site key: this page carries no locked data bundle");
    }
    return crypto.subtle.importKey("raw", base64Bytes(raw), "AES-GCM", false, ["encrypt", "decrypt"]);
  })();
  return cached;
}

// unlock turns one fetched data file back into its JSON text.
export async function unlock(blob: ArrayBuffer): Promise<string> {
  const bytes = new Uint8Array(blob);
  const plain = await crypto.subtle
    .decrypt({ name: "AES-GCM", iv: bytes.subarray(0, NONCE_BYTES) }, await siteKey(), bytes.subarray(NONCE_BYTES))
    .catch((err: unknown) => {
      // A file this page's key cannot open belongs to a different deploy than the page does — which,
      // since the data bundle is fetched per generation, only leaves the carried-forward assets.
      reportStalePage();
      throw err;
    });
  return await new Response(byteStream(new Uint8Array(plain)).pipeThrough(new DecompressionStream("gzip"))).text();
}

// lock is the same conversion in reverse, used by the prerender (web/scripts/prerender.mjs) to lock the
// dehydrated cache it inlines into each page — otherwise the page would hand out, in plain JSON, the very
// data the bundle keeps locked.
export async function lock(text: string): Promise<string> {
  const packed = await new Response(
    byteStream(new TextEncoder().encode(text)).pipeThrough(new CompressionStream("gzip")),
  ).arrayBuffer();
  const nonce = crypto.getRandomValues(new Uint8Array(NONCE_BYTES));
  const sealed = await crypto.subtle.encrypt({ name: "AES-GCM", iv: nonce }, await siteKey(), packed);
  const bytes = new Uint8Array(NONCE_BYTES + sealed.byteLength);
  bytes.set(nonce);
  bytes.set(new Uint8Array(sealed), NONCE_BYTES);
  return bytesBase64(bytes);
}

// unlockText opens a base64 blob (the inlined page state); unlock() takes the fetched binary form.
export function unlockText(blob: string): Promise<string> {
  return unlock(base64Bytes(blob).buffer as ArrayBuffer);
}

// A one-chunk stream to feed the compression streams. Blob.stream() would do the same in a browser, but
// jsdom (the test environment) has no Blob.stream.
function byteStream(bytes: BufferSource): ReadableStream<BufferSource> {
  return new ReadableStream({
    start(controller) {
      controller.enqueue(bytes);
      controller.close();
    },
  });
}

function base64Bytes(b64: string): Uint8Array<ArrayBuffer> {
  const binary = atob(b64);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
  return bytes;
}

function bytesBase64(bytes: Uint8Array): string {
  let binary = "";
  for (const b of bytes) binary += String.fromCharCode(b);
  return btoa(binary);
}
