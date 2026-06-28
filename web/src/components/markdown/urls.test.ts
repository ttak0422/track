import { describe, expect, it } from "vitest";
import {
  assetHref,
  hostOf,
  isImageHref,
  isMermaidHref,
  isPdfHref,
  isTextAssetHref,
  noteCandidateFromHref,
  safeFrameUrl,
  textAssetLang,
  tweetIdFromUrl,
  webHref,
  youtubeEmbedUrl,
  youtubeStartSeconds,
} from "./urls";

describe("webHref", () => {
  it("upgrades bare domains to https", () => {
    expect(webHref("www.example.com")).toBe("https://www.example.com");
    expect(webHref("example.com/path")).toBe("https://example.com/path");
  });
  it("leaves URLs with a scheme and non-domain text untouched", () => {
    expect(webHref("https://x.com")).toBe("https://x.com");
    expect(webHref("mailto:a@b.com")).toBe("mailto:a@b.com");
    expect(webHref("/local/path")).toBe("/local/path");
  });
});

describe("noteCandidateFromHref", () => {
  it("returns the note title, stripping .md / query / hash", () => {
    expect(noteCandidateFromHref("Some Note")).toBe("Some Note");
    expect(noteCandidateFromHref("Some Note.md")).toBe("Some Note");
    expect(noteCandidateFromHref("note?x=1#h")).toBe("note");
  });
  it("decodes percent-encoding", () => {
    expect(noteCandidateFromHref("Caf%C3%A9")).toBe("Café");
  });
  it("returns empty for anchors and explicit schemes", () => {
    expect(noteCandidateFromHref("#anchor")).toBe("");
    expect(noteCandidateFromHref("http://x")).toBe("");
  });
});

describe("assetHref", () => {
  it("maps an assets/ reference to the per-kind asset endpoint", () => {
    expect(assetHref("assets/a.png", "note")).toBe("/api/asset?kind=note&name=a.png");
    expect(assetHref("./assets/a.png", "journal")).toBe("/api/asset?kind=journal&name=a.png");
  });
  it("returns null for non-asset references", () => {
    expect(assetHref("http://x/assets/a", "note")).toBeNull();
    expect(assetHref("other/a.png", "note")).toBeNull();
    expect(assetHref("assets/", "note")).toBeNull();
  });
});

describe("youtubeEmbedUrl", () => {
  it("builds privacy-enhanced embed URLs", () => {
    expect(youtubeEmbedUrl("https://youtu.be/abcdef")).toBe(
      "https://www.youtube-nocookie.com/embed/abcdef",
    );
    expect(youtubeEmbedUrl("https://www.youtube.com/watch?v=abcdefg")).toBe(
      "https://www.youtube-nocookie.com/embed/abcdefg",
    );
  });
  it("carries a start time", () => {
    expect(youtubeEmbedUrl("https://youtube.com/watch?v=abcdefg&t=90")).toBe(
      "https://www.youtube-nocookie.com/embed/abcdefg?start=90",
    );
  });
  it("returns null for non-YouTube and unparseable URLs", () => {
    expect(youtubeEmbedUrl("https://example.com")).toBeNull();
    expect(youtubeEmbedUrl("not a url ???")).toBeNull();
  });
});

describe("youtubeStartSeconds", () => {
  it("parses plain seconds and the 1h2m3s form", () => {
    expect(youtubeStartSeconds(null)).toBe(0);
    expect(youtubeStartSeconds("90")).toBe(90);
    expect(youtubeStartSeconds("1h2m3s")).toBe(3723);
    expect(youtubeStartSeconds("2m")).toBe(120);
    expect(youtubeStartSeconds("abc")).toBe(0);
  });
});

describe("tweetIdFromUrl", () => {
  it("extracts the status id from twitter.com / x.com", () => {
    expect(tweetIdFromUrl("https://twitter.com/u/status/123")).toBe("123");
    expect(tweetIdFromUrl("https://x.com/u/status/456")).toBe("456");
  });
  it("returns null for other hosts", () => {
    expect(tweetIdFromUrl("https://example.com")).toBeNull();
  });
});

describe("href type guards", () => {
  it("recognizes images and pdfs ignoring query/hash and case", () => {
    expect(isImageHref("a.png")).toBe(true);
    expect(isImageHref("a.JPG")).toBe(true);
    expect(isImageHref("a.png?x=1")).toBe(true);
    expect(isImageHref("a.txt")).toBe(false);
    expect(isPdfHref("a.pdf")).toBe(true);
    expect(isPdfHref("a.PDF?x")).toBe(true);
    expect(isPdfHref("a.txt")).toBe(false);
  });
});

describe("text-file asset embeds", () => {
  it("recognizes mermaid sources by extension", () => {
    expect(isMermaidHref("assets/chart.mmd")).toBe(true);
    expect(isMermaidHref("assets/CHART.MERMAID?x=1")).toBe(true);
    expect(isMermaidHref("assets/chart.txt")).toBe(false);
  });
  it("maps text extensions to a render language, mermaid included", () => {
    expect(textAssetLang("assets/chart.mmd")).toBe("mermaid");
    expect(textAssetLang("assets/notes.txt")).toBe("");
    expect(textAssetLang("assets/data.json")).toBe("json");
    expect(textAssetLang("assets/run.sh")).toBe("bash");
    expect(textAssetLang("assets/photo.png")).toBeNull();
  });
  it("treats only mapped text extensions as inline text assets", () => {
    expect(isTextAssetHref("assets/chart.mmd")).toBe(true);
    expect(isTextAssetHref("assets/notes.txt")).toBe(true);
    expect(isTextAssetHref("assets/photo.png")).toBe(false);
    expect(isTextAssetHref("assets/doc.pdf")).toBe(false);
  });
});

describe("safeFrameUrl", () => {
  it("allows http(s) and relative paths, rejects dangerous schemes", () => {
    expect(safeFrameUrl("https://x")).toBe("https://x");
    expect(safeFrameUrl("/rel")).toBe("/rel");
    expect(safeFrameUrl("./rel")).toBe("./rel");
    expect(safeFrameUrl("javascript:alert(1)")).toBeNull();
    expect(safeFrameUrl("data:text/html,x")).toBeNull();
  });
});

describe("hostOf", () => {
  it("returns the hostname, or the input when unparseable", () => {
    expect(hostOf("https://a.b.com/x")).toBe("a.b.com");
    expect(hostOf("garbage")).toBe("garbage");
  });
});
