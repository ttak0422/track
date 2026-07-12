import { useContext, useEffect, useRef, useState } from "react";
import { useAssetTextQuery, useOgpQuery } from "../../queries";
import { PdfDeck } from "../PdfDeck";
import { CodeBlock } from "./CodeBlock";
import { NoteKindContext } from "./context";
import { GraphvizDiagram } from "./GraphvizDiagram";
import { MediaFrame } from "./MediaFrame";
import { MermaidDiagram } from "./MermaidDiagram";
import { EChartsFence } from "./EChartsBlock";
import {
  assetHref,
  googleMapsEmbedUrl,
  hostOf,
  isEChartsHref,
  isHtmlHref,
  isImageHref,
  isMermaidHref,
  isPdfHref,
  isTextAssetHref,
  safeFrameUrl,
  textAssetLang,
  tweetIdFromUrl,
  webHref,
  youtubeEmbedUrl,
} from "./urls";

interface EmbedProps {
  src: string;
  alt: string;
  // A CSS length from the `:height` embed option (see remarkEmbedOptions); applied to the HTML-page
  // frame, which otherwise has no intrinsic height. Ignored by intrinsically-sized embeds.
  height?: string;
}

// Embed renders a standalone ![alt](src), routing by the kind of target: YouTube links become an
// inline player, PDFs a slide-deck viewer, image URLs an <img>, and any other http(s) page an Open Graph
// card. Embedding stays opt-in via the ![...]() syntax so ordinary [text](url) links are never turned
// into noisy previews. The URL is normalized through webHref so bare domains still resolve, and only
// http(s)/relative URLs feed an iframe so a note cannot smuggle a javascript: document into the frame.
export function Embed({ src, alt, height }: EmbedProps) {
  const kind = useContext(NoteKindContext);
  // A relative "assets/<file>" reference is served from the vault by the local server. Resolving it here
  // means it is never treated as a YouTube/tweet/OGP URL and never resolved against the /notes/<id>
  // route (which the SPA fallback would answer with index.html, rendering the app inside the embed).
  const asset = assetHref(src, kind);

  const youtube = asset ? null : youtubeEmbedUrl(src);
  if (youtube) {
    return (
      <div className="embed embed-video">
        <iframe
          src={youtube}
          title={alt || "YouTube video"}
          loading="lazy"
          allow="accelerometer; autoplay; clipboard-write; encrypted-media; gyroscope; picture-in-picture; web-share"
          allowFullScreen
        />
      </div>
    );
  }

  const map = asset ? null : googleMapsEmbedUrl(src);
  if (map) {
    return (
      <div className="embed embed-map">
        <iframe
          src={map}
          title={alt || "Map"}
          loading="lazy"
          allowFullScreen
          referrerPolicy="no-referrer-when-downgrade"
        />
      </div>
    );
  }

  const target = asset ?? webHref(src);
  if (isPdfHref(src)) {
    const safe = safeFrameUrl(target);
    if (safe) {
      return (
        <MediaFrame src={src} alt={alt}>
          <PdfDeck src={safe} alt={alt} />
        </MediaFrame>
      );
    }
  }

  // An HTML document (local asset or remote page) is mounted in a sandboxed iframe so its own JS/CSS run
  // but it cannot reach the app/vault: no allow-same-origin, so the frame gets a unique opaque origin with
  // no access to the parent, cookies, or storage. allow-scripts + allow-same-origin together would let the
  // frame remove its own sandbox, so that pair is deliberately never used.
  if (isHtmlHref(src)) {
    const safe = safeFrameUrl(target);
    if (safe) {
      return (
        <div className="embed embed-html">
          <iframe
            src={safe}
            sandbox="allow-scripts allow-popups"
            loading="lazy"
            title={alt || "Embedded page"}
            // `:height` overrides the CSS min-height floor (both, so a value below the default can shrink).
            style={height ? { height, minHeight: height } : undefined}
          />
        </div>
      );
    }
  }

  // A text-file attachment (a mermaid diagram source, or any plain-text file) is fetched and rendered
  // inline rather than handed to <img>, which would only show a broken image. This stays asset-only so a
  // remote text URL is still treated as an ordinary link/OGP card.
  if (asset && isTextAssetHref(src)) {
    // A resolved-chart asset renders the same interactive block a fenced ```echarts chart does, so the
    // media hover-preview/float chrome would only pop up a duplicate of it; render it bare like the fence.
    if (isEChartsHref(src)) {
      return <TextAssetEmbed href={asset} src={src} alt={alt} />;
    }
    return (
      <MediaFrame src={src} alt={alt}>
        <TextAssetEmbed href={asset} src={src} alt={alt} />
      </MediaFrame>
    );
  }

  const tweetId = asset ? null : tweetIdFromUrl(src);
  if (tweetId) {
    return <TweetEmbed tweetId={tweetId} url={target} alt={alt} />;
  }

  if (!asset && !isImageHref(src) && /^https?:\/\//i.test(target)) {
    return <OgpCard url={target} alt={alt} />;
  }

  return (
    <MediaFrame src={src} alt={alt}>
      <img className="embed embed-image" src={target} alt={alt} loading="lazy" />
    </MediaFrame>
  );
}

interface TextAssetEmbedProps {
  href: string;
  src: string;
  alt: string;
}

// TextAssetEmbed fetches a text-file attachment and renders it: a mermaid source becomes a diagram (the
// same renderer fenced ```mermaid blocks use), a resolved-chart option (.echarts.json) an interactive
// chart, any other text file a syntax-highlighted code block. While loading it shows a placeholder, and
// on a failed fetch it degrades to a plain link so the embed is never a dead end.
function TextAssetEmbed({ href, src, alt }: TextAssetEmbedProps) {
  const text = useAssetTextQuery(href);

  if (text.isLoading) {
    return <div className="embed text-asset text-asset-loading">Loading…</div>;
  }
  if (text.isError || text.data === undefined) {
    return (
      <a className="embed md-link text-asset-fallback" href={href} target="_blank" rel="noreferrer noopener">
        {alt || src}
      </a>
    );
  }
  if (isMermaidHref(src)) {
    return <MermaidDiagram text={text.data} />;
  }
  // A Graphviz source attachment (.dot/.gv) renders with the same engine fenced ```dot blocks use.
  if (textAssetLang(src) === "dot") {
    return <GraphvizDiagram text={text.data} />;
  }
  if (isEChartsHref(src)) {
    return <EChartsFence text={text.data} />;
  }
  return <CodeBlock lang={textAssetLang(src) ?? ""} text={text.data} />;
}

interface TweetEmbedProps {
  tweetId: string;
  url: string;
  alt: string;
}

type TweetStatus = "loading" | "ready" | "error";

// TweetEmbed renders the actual Twitter/X post (not just a card) via Twitter's official widgets.js,
// matching how Obsidian embeds tweets. While the widget loads it shows a plain link, and if the tweet
// cannot be rendered (deleted, blocked, offline) it falls back to the generic OGP card.
function TweetEmbed({ tweetId, url, alt }: TweetEmbedProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const [status, setStatus] = useState<TweetStatus>("loading");

  useEffect(() => {
    let cancelled = false;
    setStatus("loading");
    loadTwitterWidgets()
      .then((twttr) => {
        if (cancelled) return;
        const container = containerRef.current;
        if (!twttr || !container) {
          setStatus("error");
          return;
        }
        container.replaceChildren();
        return twttr.widgets
          .createTweet(tweetId, container, { dnt: true, theme: currentTheme(), conversation: "none" })
          .then((el) => {
            if (cancelled) return;
            setStatus(el ? "ready" : "error");
          });
      })
      .catch(() => {
        if (!cancelled) setStatus("error");
      });
    return () => {
      cancelled = true;
    };
  }, [tweetId]);

  if (status === "error") {
    return <OgpCard url={url} alt={alt} />;
  }
  return (
    <div className="embed embed-tweet">
      <div ref={containerRef} />
      {status === "loading" ? (
        <a className="md-link embed-fallback" href={url} target="_blank" rel="noreferrer noopener">
          {alt || url}
        </a>
      ) : null}
    </div>
  );
}

interface TwitterWidgets {
  createTweet: (
    id: string,
    container: HTMLElement,
    options?: Record<string, unknown>,
  ) => Promise<HTMLElement | undefined>;
}

interface Twttr {
  widgets: TwitterWidgets;
}

let twitterWidgetsPromise: Promise<Twttr | null> | null = null;

// loadTwitterWidgets injects Twitter's widgets.js once and resolves the global twttr API. Subsequent
// calls reuse the same promise so the script is never loaded twice.
function loadTwitterWidgets(): Promise<Twttr | null> {
  if (typeof window === "undefined" || typeof document === "undefined") {
    return Promise.resolve(null);
  }
  const existing = (window as unknown as { twttr?: Twttr }).twttr;
  if (existing?.widgets) {
    return Promise.resolve(existing);
  }
  if (twitterWidgetsPromise) {
    return twitterWidgetsPromise;
  }
  twitterWidgetsPromise = new Promise((resolve, reject) => {
    const script = document.createElement("script");
    script.src = "https://platform.twitter.com/widgets.js";
    script.async = true;
    script.onload = () => resolve((window as unknown as { twttr?: Twttr }).twttr ?? null);
    script.onerror = () => reject(new Error("failed to load twitter widgets"));
    document.head.appendChild(script);
  });
  return twitterWidgetsPromise;
}

// currentTheme resolves the embed theme from the app's data-theme attribute, falling back to the OS
// preference when it is unset or set to "system".
function currentTheme(): "light" | "dark" {
  const attr = document.documentElement.getAttribute("data-theme");
  if (attr === "dark" || attr === "light") {
    return attr;
  }
  return window.matchMedia?.("(prefers-color-scheme: dark)").matches ? "dark" : "light";
}

interface OgpCardProps {
  url: string;
  alt: string;
}

// OgpCard fetches the link's Open Graph metadata through the local server and renders it as a card. It
// degrades gracefully: while loading it shows the host and label, and on a failed/blocked fetch it
// falls back to a plain link so the embed is never a dead end.
function OgpCard({ url, alt }: OgpCardProps) {
  const ogp = useOgpQuery(url);
  const host = hostOf(url);

  if (ogp.isError) {
    return (
      <a className="embed md-link ogp-fallback" href={url} target="_blank" rel="noreferrer noopener">
        {alt || url}
      </a>
    );
  }

  const data = ogp.data;
  const title = data?.title || alt || url;
  return (
    <a className="embed ogp-card" href={url} target="_blank" rel="noreferrer noopener">
      {data?.image ? (
        <img className="ogp-card-image" src={data.image} alt="" loading="lazy" />
      ) : null}
      <span className="ogp-card-body">
        <span className="ogp-card-site">{data?.site_name || host}</span>
        <span className="ogp-card-title">{title}</span>
        {data?.description ? <span className="ogp-card-desc">{data.description}</span> : null}
      </span>
    </a>
  );
}
