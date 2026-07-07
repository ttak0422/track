import { useEffect, useRef, useState } from "react";
import type { PDFDocumentLoadingTask, PDFDocumentProxy, RenderTask } from "pdfjs-dist";
import workerUrl from "pdfjs-dist/build/pdf.worker.min.mjs?url";
import { STATIC_MODE } from "../runtime";

// pdf.js needs two asset directories at render time: cmaps (CID-keyed fonts — most CJK PDFs show no
// text without them) and standard_fonts (the standard 14 fonts a PDF may reference without
// embedding). The live build bundles both under pdfjs/ (vite.config's bundlePdfjsAssets; ADR 0029 —
// the workspace works offline); the static export keeps its footprint small and loads them from
// jsDelivr, pinned to the bundled pdfjs-dist version; the Vite dev server reads them straight out of
// node_modules.
function pdfjsAssetBase(version: string): string {
  if (STATIC_MODE) return `https://cdn.jsdelivr.net/npm/pdfjs-dist@${version}/`;
  if (import.meta.env.DEV) return "/node_modules/pdfjs-dist/";
  return `${import.meta.env.BASE_URL}pdfjs/`;
}

// pdf.js is large, so it is code-split out of the main bundle and only fetched when a note actually
// embeds a PDF. The first call wires up the worker (Vite emits it under assets/ with a content hash,
// served by the local server) so rendering never blocks the main thread; later calls reuse the module.
let pdfjsPromise: Promise<typeof import("pdfjs-dist")> | null = null;
function loadPdfjs(): Promise<typeof import("pdfjs-dist")> {
  if (!pdfjsPromise) {
    // pdf.js 6.x calls the stage-3 Map.getOrInsert/getOrInsertComputed proposals; on an engine
    // without them every render throws — silently, since cancellations are the expected rejection
    // below — and each page stays a blank canvas. Backfill them before the module loads.
    /* eslint-disable @typescript-eslint/no-explicit-any -- proposal APIs, absent from lib.dom */
    const proto = Map.prototype as any;
    if (!proto.getOrInsertComputed) {
      proto.getOrInsertComputed = function (key: unknown, cb: (key: unknown) => unknown) {
        if (!this.has(key)) this.set(key, cb(key));
        return this.get(key);
      };
    }
    if (!proto.getOrInsert) {
      proto.getOrInsert = function (key: unknown, value: unknown) {
        if (!this.has(key)) this.set(key, value);
        return this.get(key);
      };
    }
    /* eslint-enable @typescript-eslint/no-explicit-any */
    pdfjsPromise = import("pdfjs-dist").then((pdfjs) => {
      pdfjs.GlobalWorkerOptions.workerSrc = workerUrl;
      return pdfjs;
    });
  }
  return pdfjsPromise;
}

interface PdfDeckProps {
  src: string;
  alt: string;
}

type Status = "loading" | "ready" | "error";

// PdfDeck shows a PDF as a SpeakerDeck-style slide viewer: one page at a time, fit to the available
// width, with prev/next navigation and a page counter. Pages are rasterized onto a canvas with pdf.js
// rather than handed to the browser's native <iframe> viewer, so none of the print/download/scroll
// toolbar chrome leaks in — the note just gets a clean page-by-page reader.
export function PdfDeck({ src, alt }: PdfDeckProps) {
  const stageRef = useRef<HTMLDivElement>(null);
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const docRef = useRef<PDFDocumentProxy | null>(null);
  const loadingTaskRef = useRef<PDFDocumentLoadingTask | null>(null);
  const renderTaskRef = useRef<RenderTask | null>(null);
  const [status, setStatus] = useState<Status>("loading");
  const [numPages, setNumPages] = useState(0);
  const [page, setPage] = useState(1);
  const [width, setWidth] = useState(0);

  // Load the document once per src. Destroying the previous doc on unmount/src change releases the
  // worker-side resources pdf.js holds for it.
  useEffect(() => {
    let cancelled = false;
    setStatus("loading");
    setNumPages(0);
    setPage(1);
    loadPdfjs().then(
      (pdfjs) => {
        if (cancelled) return;
        const assetBase = pdfjsAssetBase(pdfjs.version);
        const task = pdfjs.getDocument({
          url: src,
          cMapUrl: `${assetBase}cmaps/`,
          cMapPacked: true,
          standardFontDataUrl: `${assetBase}standard_fonts/`,
        });
        loadingTaskRef.current = task;
        task.promise.then(
          (doc) => {
            if (cancelled) return;
            docRef.current = doc;
            setNumPages(doc.numPages);
            setStatus("ready");
          },
          () => {
            if (!cancelled) setStatus("error");
          },
        );
      },
      () => {
        if (!cancelled) setStatus("error");
      },
    );
    return () => {
      cancelled = true;
      renderTaskRef.current?.cancel();
      void loadingTaskRef.current?.destroy();
      loadingTaskRef.current = null;
      docRef.current = null;
    };
  }, [src]);

  // Track the stage width so a slide can fit-to-width and re-rasterize crisply when the layout changes.
  useEffect(() => {
    const el = stageRef.current;
    if (!el) return;
    const update = () => setWidth(el.clientWidth);
    update();
    const ro = new ResizeObserver(update);
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  // Rasterize the current page whenever the page or available width changes. The canvas backing store is
  // sized in device pixels (capped at 2x) for sharpness, while its CSS box is bounded by both the width
  // and ~75vh so a tall page never overflows the note.
  useEffect(() => {
    const doc = docRef.current;
    const canvas = canvasRef.current;
    if (status !== "ready" || !doc || !canvas || width <= 0) return;
    let cancelled = false;
    renderTaskRef.current?.cancel();
    void doc.getPage(page).then((pdfPage) => {
      if (cancelled) return;
      const base = pdfPage.getViewport({ scale: 1 });
      const dpr = Math.min(window.devicePixelRatio || 1, 2);
      const maxHeight = window.innerHeight * 0.75;
      const cssScale = Math.min(width / base.width, maxHeight / base.height);
      const viewport = pdfPage.getViewport({ scale: cssScale * dpr });
      // Rasterize offscreen and blit once complete: pdf.js paints its operator list progressively,
      // so rendering straight into the visible canvas shows the page drawing itself in (and page
      // flips flash blank while the next page paints). Offscreen, the page appears in one frame.
      const buffer = document.createElement("canvas");
      buffer.width = Math.round(viewport.width);
      buffer.height = Math.round(viewport.height);
      const task = pdfPage.render({ canvas: buffer, viewport });
      renderTaskRef.current = task;
      task.promise.then(
        () => {
          if (cancelled) return;
          canvas.width = buffer.width;
          canvas.height = buffer.height;
          canvas.style.width = `${Math.round(viewport.width / dpr)}px`;
          canvas.style.height = `${Math.round(viewport.height / dpr)}px`;
          canvas.getContext("2d")?.drawImage(buffer, 0, 0);
        },
        (err: unknown) => {
          // A render is cancelled whenever the page/width changes mid-flight; that rejection is
          // expected. Anything else must not vanish — a swallowed render error reads as a blank page.
          if ((err as { name?: string })?.name !== "RenderingCancelledException") {
            console.error("PdfDeck render failed:", err);
          }
        },
      );
    });
    return () => {
      cancelled = true;
    };
  }, [status, page, width]);

  const go = (delta: number) =>
    setPage((p) => Math.min(numPages || 1, Math.max(1, p + delta)));

  if (status === "error") {
    return (
      <a className="embed md-link embed-fallback" href={src} target="_blank" rel="noreferrer noopener">
        {alt || "Open PDF"}
      </a>
    );
  }

  return (
    <div
      className="embed embed-pdf"
      tabIndex={0}
      onKeyDown={(e) => {
        if (e.key === "ArrowRight" || e.key === "ArrowDown") {
          e.preventDefault();
          go(1);
        } else if (e.key === "ArrowLeft" || e.key === "ArrowUp") {
          e.preventDefault();
          go(-1);
        }
      }}
    >
      <div
        className="pdf-deck-stage"
        ref={stageRef}
        onClick={() => go(1)}
        role="img"
        aria-label={alt || "PDF document"}
      >
        <canvas ref={canvasRef} className="pdf-deck-canvas" />
        {status === "loading" ? <span className="pdf-deck-loading">Loading…</span> : null}
      </div>
      <div className="pdf-deck-bar">
        <button
          type="button"
          className="pdf-deck-nav"
          onClick={() => go(-1)}
          disabled={page <= 1}
          aria-label="Previous page"
        >
          ‹
        </button>
        <span className="pdf-deck-count">{numPages ? `${page} / ${numPages}` : "…"}</span>
        <button
          type="button"
          className="pdf-deck-nav"
          onClick={() => go(1)}
          disabled={page >= numPages}
          aria-label="Next page"
        >
          ›
        </button>
        <a className="md-link pdf-deck-open" href={src} target="_blank" rel="noreferrer noopener">
          {alt || "Open PDF"}
        </a>
      </div>
    </div>
  );
}
