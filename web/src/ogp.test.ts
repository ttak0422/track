import { afterEach, describe, expect, it, vi } from "vitest";
import { browserOgp, parseOgp } from "./ogp";

const url = "https://example.test/original";

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("OGP parsing", () => {
  it("keeps the first case-insensitive tag, including an empty value, and scans only head", () => {
    const html = `<html><head>
      <title>HTML fallback</title>
      <meta property="OG:TITLE" content="  First &amp; primary  ">
      <meta name="og:title" content="Later duplicate">
      <meta property="og:description" content=" ">
      <meta property="og:description" content="Ignored duplicate">
      <meta name="twitter:description" content=" Twitter fallback ">
      <meta property="" name="og:site_name" content="Ignored empty property">
    </head><body><meta property="og:image" content="/body.png"></body></html>`;
    expect(parseOgp(html, url, url)).toEqual({
      url, title: "First & primary", description: "Twitter fallback", site_name: "example.test",
    });
  });

  it("prefers Open Graph over Twitter and plain metadata", () => {
    const html = `<head><title>HTML</title>
      <meta name="twitter:title" content="Twitter"><meta property="og:title" content="OG">
      <meta name="description" content="Plain"><meta name="twitter:description" content="Twitter description">
      <meta property="og:description" content="OG description"><meta property="og:site_name" content=" Site ">
      <meta name="twitter:image" content="/twitter.png"><meta property="og:image:url" content="/alias.png">
      <meta property="og:image" content="/primary.png"></head>`;
    expect(parseOgp(html, url, url)).toEqual({
      url, title: "OG", description: "OG description", image: "https://example.test/primary.png", site_name: "Site",
    });
  });

  it("falls back to Twitter tags and then the HTML title/plain description", () => {
    expect(parseOgp(`<head><title>HTML</title><meta name="twitter:title" content="Twitter">
      <meta name="twitter:description" content="Description"></head>`, url, url)).toEqual({
      url, title: "Twitter", description: "Description", site_name: "example.test",
    });
    expect(parseOgp(`<head><title> HTML &amp; text </title><meta name="description" content=" Plain "></head>`, url, url)).toEqual({
      url, title: "HTML & text", description: "Plain", site_name: "example.test",
    });
  });

  it.each(["og:image", "og:image:url", "twitter:image", "twitter:image:src"])("accepts the %s image field", (key) => {
    expect(parseOgp(`<meta property="${key}" content="../cover.png">`, "https://example.test/posts/item", url).image)
      .toBe("https://example.test/cover.png");
  });

  it.each([
    ["/cover.png", "https://example.test/cover.png"],
    ["../cover.png", "https://example.test/cover.png"],
    ["//cdn.example.test/cover.png", "https://cdn.example.test/cover.png"],
    ["http://cdn.example.test/cover.png", "http://cdn.example.test/cover.png"],
  ])("resolves image %s against the supplied page URL", (ref, image) => {
    expect(parseOgp(`<meta property="og:image" content="${ref}">`, "https://example.test/posts/item", url).image).toBe(image);
  });

  it.each(["javascript:alert(1)", "data:image/png;base64,abc", "ftp://example.test/card.png"])("omits image scheme %s", (ref) => {
    expect(parseOgp(`<meta property="og:image" content="${ref}">`, url, url)).toEqual({ url, site_name: "example.test" });
  });

  it("does not try a lower-priority image after the selected reference is rejected", () => {
    expect(parseOgp(`<meta property="og:image" content="data:image/png;base64,abc">
      <meta name="twitter:image" content="https://example.test/fallback.png">`, url, url)).toEqual({ url, site_name: "example.test" });
  });

  it("omits empty fields and tolerates an invalid page URL", () => {
    expect(parseOgp(`<head><title> </title><meta property="og:image" content="/cover.png"></head>`, "not a URL", url)).toEqual({ url });
  });

  it("clips titles and descriptions by Unicode code points without splitting surrogate pairs", () => {
    const title = "a".repeat(199) + "😀tail";
    const description = "日".repeat(319) + "🚀tail";
    const result = parseOgp(`<meta property="og:title" content="${title}"><meta property="og:description" content="${description}">`, url, url);
    expect(result.title).toBe("a".repeat(199) + "😀");
    expect(result.description).toBe("日".repeat(319) + "🚀");
    expect([...result.title!]).toHaveLength(200);
    expect([...result.description!]).toHaveLength(320);
  });
});

describe("browser OGP fetching", () => {
  it("uses the final response URL for images and the host while retaining the requested URL", async () => {
    const response = new Response(`<title>Redirected</title><meta property="og:image" content="../cover.png">`, {
      headers: { "content-type": "text/html" },
    });
    Object.defineProperty(response, "url", { value: "https://redirected.test/posts/item" });
    const fetch = vi.fn().mockResolvedValue(response);
    vi.stubGlobal("fetch", fetch);
    expect(await browserOgp(url)).toEqual({ url, title: "Redirected", image: "https://redirected.test/cover.png", site_name: "redirected.test" });
    expect(fetch).toHaveBeenCalledExactlyOnceWith(url);
  });

  it.each([null, "", "Text/HTML; charset=UTF-8", "application/xhtml+xml"])("accepts content type %s and uses the request URL when response.url is empty", async (type) => {
    const response = new Response("<title>Accepted</title>");
    if (type === null) response.headers.delete("content-type");
    else response.headers.set("content-type", type);
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response));
    expect(await browserOgp(url)).toEqual({ url, title: "Accepted", site_name: "example.test" });
  });

  it.each(["application/json", "text/plain"])("returns the bare card without reading a %s body", async (type) => {
    const response = new Response("<title>Ignored</title>", { headers: { "content-type": type } });
    const text = vi.spyOn(response, "text");
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response));
    expect(await browserOgp(url)).toEqual({ url });
    expect(text).not.toHaveBeenCalled();
  });

  it.each([404, 500])("returns the bare card without reading a non-OK %s response", async (status) => {
    const response = new Response("<title>Ignored</title>", { status, headers: { "content-type": "text/html" } });
    const text = vi.spyOn(response, "text");
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response));
    expect(await browserOgp(url)).toEqual({ url });
    expect(text).not.toHaveBeenCalled();
  });

  it("returns the bare card on a CORS/network rejection", async () => {
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new TypeError("Failed to fetch")));
    expect(await browserOgp(url)).toEqual({ url });
  });

  it("returns the bare card when reading the response body fails", async () => {
    const response = new Response("", { headers: { "content-type": "text/html" } });
    vi.spyOn(response, "text").mockRejectedValue(new Error("Read failed"));
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response));
    expect(await browserOgp(url)).toEqual({ url });
  });
});
