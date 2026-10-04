import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MarkdownView } from "../MarkdownView";
import { type ExternalMarkdownImage, type ExternalMarkdownOptions } from "./ExternalMarkdownView";

describe("external MarkdownView", () => {
  const fetch = vi.fn(() => Promise.reject(new Error("External rendering must not make API requests")));
  beforeEach(() => {
    fetch.mockClear();
    vi.stubGlobal("fetch", fetch);
  });
  afterEach(() => vi.unstubAllGlobals());

  it("keeps regular typography, GFM and heading IDs without a vault/query provider", () => {
    const { container } = render(<MarkdownView external title="Readme" markdown={[
      "# Readme", "", "## Installation", "", "## Installation", "",
      "Some **strong**, ~~deleted~~ and `inline` text.", "",
      "| Feature | Works |", "| --- | --- |", "| Table | yes |", "",
      "> [!NOTE]", "> A note.",
    ].join("\n")} />);
    expect(container.querySelector(".markdown-view")).not.toBeNull();
    expect(container.querySelector(".note-title")).toHaveTextContent("Readme");
    expect(container.querySelector("#h-installation")).toBeInTheDocument();
    expect(container.querySelector("#h-installation-2")).toBeInTheDocument();
    expect(container.querySelector("strong")).toHaveTextContent("strong");
    expect(container.querySelector("del")).toHaveTextContent("deleted");
    expect(container.querySelector(".inline-code")).toHaveTextContent("inline");
    expect(screen.getByRole("table")).toHaveTextContent("Feature");
    expect(fetch).not.toHaveBeenCalled();
  });

  it("never resolves vault links/includes, upgrades tasks, or runs active fences", () => {
    const languages = ["track-view", "query", "viewspec", "echarts", "taskboard", "map", "mermaid", "graphviz", "dot", "d2", "drawio", "mindmap", "html"];
    const markdown = [
      "[[Private vault note]]", "", "![[Secret document]]", "",
      "- [ ] Pending [#A] [due:2027-01-01]", "- [x] Done", "- [/] In progress", "",
      ...languages.map((language) => `\`\`\`${language}\nfetch('/api/notes'); <img src='https://tracker.test/pixel'>\n\`\`\``),
    ].join("\n");
    const { container } = render(<MarkdownView
      external
      markdown={markdown}
      noteId="private-note"
      vault="private-vault"
      copyPath="/private/vault.md"
      includes={[{ line: 2, note_id: 1, title: "Secret document", caption: "secret", lines: ["TOP SECRET"] }]}
    />);
    expect(container).toHaveTextContent("[[Private vault note]]");
    expect(container).toHaveTextContent("![[Secret document]]");
    expect(container).not.toHaveTextContent("TOP SECRET");
    expect(container.querySelectorAll(".code-block")).toHaveLength(languages.length);
    expect(container.querySelectorAll(".note-include, .task-table, .task-board, iframe, img, svg")).toHaveLength(0);
    expect(screen.queryByRole("link")).not.toBeInTheDocument();
    for (const checkbox of screen.getAllByRole("checkbox")) expect(checkbox).toBeDisabled();
    expect(container).toHaveTextContent("[#A] [due:2027-01-01]");
    expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
    expect(fetch).not.toHaveBeenCalled();
  });

  it("assigns consistent, unique IDs to Setext and ATX headings, with inline formatting removed", () => {
    const { container } = render(<MarkdownView external markdown={[
      "Getting **started**!", "====================", "",
      "Installation & setup", "--------------------", "",
      "## Installation & setup", "",
      "## Hello, [world](https://example.test)!", "",
      "## Installation setup-2", "",
      '<h2 id="owned-by-html">Raw HTML</h2>',
    ].join("\n")} />);
    expect([...container.querySelectorAll("h1, h2")].map((heading) => heading.id)).toEqual([
      "h-getting-started", "h-installation-setup", "h-installation-setup-2", "h-hello-world", "h-installation-setup-2-2",
    ]);
    expect(container.querySelector("#owned-by-html")).toBeNull();
  });

  it("leaves raw HTML inert and unsafe destinations unclickable", () => {
    const onOpenLink = vi.fn();
    const { container } = render(<MarkdownView external={{ onOpenLink }} markdown={[
      '<script>fetch("/api/notes")</script>', "",
      '<iframe src="https://tracker.test/frame"></iframe>', "",
      '<img src="https://tracker.test/html-pixel" onerror="alert(1)">', "",
      '<style>@import url(https://tracker.test/css);</style>', "",
      "[script](javascript:alert%281%29)", "[encoded](java%73cript%3Aalert%281%29)",
      "[data](data:text/html,hello)", "[file](file:///etc/passwd)", "[mail](mailto:x@example.test)",
      "[action](track:delete)", "[absolute](/api/notes)", "[protocol](//tracker.test/page)",
      "[encoded-absolute](%2Fapi/notes)",
    ].join("\n")} />);
    expect(container.querySelectorAll("script, iframe, style, img, a")).toHaveLength(0);
    expect(onOpenLink).not.toHaveBeenCalled();
    expect(fetch).not.toHaveBeenCalled();
  });

  it("opens ordinary web links only through explicit, referrer-free new-tab anchors", () => {
    const onOpenLink = vi.fn();
    render(<MarkdownView external={{ onOpenLink }} markdown="[Website](https://example.test/page)" />);
    const link = screen.getByRole("link", { name: "Website" });
    expect(link).toHaveAttribute("href", "https://example.test/page");
    expect(link).toHaveAttribute("target", "_blank");
    expect(link).toHaveAttribute("rel", "noreferrer noopener");
    fireEvent.mouseEnter(link);
    expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
    expect(onOpenLink).not.toHaveBeenCalled();
    expect(fetch).not.toHaveBeenCalled();
  });

  it("defaults local links to inert text and passes the original relative target only on click", () => {
    const onOpenLink = vi.fn();
    const markdown = "[Next](../guide%20one.md#installation)";
    const { rerender } = render(<MarkdownView external markdown={markdown} />);
    expect(screen.queryByRole("link")).not.toBeInTheDocument();
    rerender(<MarkdownView external={{ onOpenLink }} markdown={markdown} />);
    const link = screen.getByRole("link", { name: "Next" });
    expect(link).toHaveAttribute("href", "#");
    expect(onOpenLink).not.toHaveBeenCalled();
    fireEvent.click(link);
    expect(onOpenLink).toHaveBeenCalledExactlyOnceWith("../guide%20one.md#installation");
    expect(fetch).not.toHaveBeenCalled();
  });

  it("scrolls same-document headings and footnotes without changing the route", () => {
    const { container } = render(<MarkdownView external markdown={[
      "[Jump](#installation)", "", "## Installation", "", "A footnote[^one].", "", "[^one]: Footnote text.",
    ].join("\n")} />);
    const before = window.location.href;
    const target = container.querySelector("#h-installation")!;
    const scroll = vi.spyOn(target, "scrollIntoView");
    fireEvent.click(screen.getByRole("link", { name: "Jump" }));
    expect(scroll).toHaveBeenCalled();
    expect(window.location.href).toBe(before);
    const footnote = container.querySelector('[id="user-content-fn-one"]')!;
    const footnoteScroll = vi.spyOn(footnote, "scrollIntoView");
    fireEvent.click(screen.getByRole("link", { name: "1" }));
    expect(footnoteScroll).toHaveBeenCalled();
    expect(container.querySelector('[id="user-content-fnref-one"]')).not.toBeNull();
  });

  it("scrolls fragments immediately and delegates them to the parent for history, including repeated clicks", () => {
    const onOpenLink = vi.fn();
    const { container } = render(<MarkdownView external={{ onOpenLink }} markdown={[
      "[Jump](#installation)", "", "## Installation", "", "A footnote[^one].", "", "[^one]: Footnote text.",
    ].join("\n")} />);
    const scroll = vi.spyOn(container.querySelector("#h-installation")!, "scrollIntoView");
    fireEvent.click(screen.getByRole("link", { name: "Jump" }));
    expect(onOpenLink).toHaveBeenCalledExactlyOnceWith("#installation");
    expect(scroll).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByRole("link", { name: "Jump" }));
    expect(onOpenLink).toHaveBeenCalledTimes(2);
    expect(scroll).toHaveBeenCalledTimes(2);
    fireEvent.click(screen.getByRole("link", { name: "1" }));
    expect(onOpenLink).toHaveBeenLastCalledWith("#user-content-fn-one");
  });

  it("never resolves trackers, absolute images, raw blob/data images or SVGs", async () => {
    const resolveImage = vi.fn();
    const { container } = render(<MarkdownView external={{ resolveImage }} markdown={[
      "![tracker](https://tracker.test/pixel.png)", "![protocol](//tracker.test/pixel.png)",
      "![asset](/api/assets/private.png)", "![encoded](%2fapi/assets/private.png)",
      "![inline](data:image/png;base64,AAAA)", "![blob](blob:http://localhost/forged)",
      "![SVG](local.svg)", "![compressed](local.svgz)", "![encoded SVG](local%2Esvg#view)",
    ].join("\n\n")} />);
    await act(async () => {});
    expect(container.querySelector("img")).toBeNull();
    expect(resolveImage).not.toHaveBeenCalled();
    expect(fetch).not.toHaveBeenCalled();
  });

  it("loads only approved relative raster blobs, with safe unavailable-image defaults", async () => {
    const resolveImage = vi.fn(async () => ({ src: "blob:http://localhost/owned", mimeType: "image/png" }));
    const { container, rerender } = render(<MarkdownView external markdown="![Local diagram](images/diagram.png)" />);
    expect(container.querySelector("img")).toBeNull();
    expect(screen.getByRole("img", { name: "Local diagram" })).toHaveTextContent("Image unavailable");
    rerender(<MarkdownView external={{ resolveImage }} markdown="![Local diagram](images/diagram.png)" />);
    await waitFor(() => expect(container.querySelector("img")).toHaveAttribute("src", "blob:http://localhost/owned"));
    expect(resolveImage).toHaveBeenCalledExactlyOnceWith("images/diagram.png");
    expect(container.querySelector("img")).toHaveAttribute("referrerpolicy", "no-referrer");
    expect(fetch).not.toHaveBeenCalled();
  });

  it.each([
    { src: "https://tracker.test/pixel.png", mimeType: "image/png" },
    { src: "/api/assets/private.png", mimeType: "image/png" },
    { src: "blob:http://localhost/owned", mimeType: "image/svg+xml" },
  ])("rejects unsafe resolver output %j", async (image) => {
    const resolveImage = vi.fn(async () => image);
    const { container } = render(<MarkdownView external={{ resolveImage }} markdown="![Candidate](local.png)" />);
    await act(async () => {});
    expect(resolveImage).toHaveBeenCalledExactlyOnceWith("local.png");
    expect(container.querySelector("img")).toBeNull();
  });

  it("ignores delayed image results after navigating and after unmounting", async () => {
    let resolveOld!: (value: ExternalMarkdownImage) => void;
    const resolveImage: ExternalMarkdownOptions["resolveImage"] = vi.fn((source) => source === "old.png"
      ? new Promise<ExternalMarkdownImage>((resolve) => { resolveOld = resolve; })
      : { src: "blob:http://localhost/new", mimeType: "image/png" });
    const { container, rerender, unmount } = render(<MarkdownView external={{ resolveImage }} markdown="![Old](old.png)" />);
    await act(async () => {});
    rerender(<MarkdownView external={{ resolveImage }} markdown="![New](new.png)" />);
    await waitFor(() => expect(container.querySelector("img")).toHaveAttribute("src", "blob:http://localhost/new"));
    await act(async () => { resolveOld({ src: "blob:http://localhost/old", mimeType: "image/png" }); });
    expect(container.querySelector("img")).toHaveAttribute("src", "blob:http://localhost/new");
    rerender(<MarkdownView external={{ resolveImage }} markdown="![Old](old.png)" />);
    await act(async () => {});
    unmount();
    await act(async () => { resolveOld({ src: "blob:http://localhost/old", mimeType: "image/png" }); });
    expect(fetch).not.toHaveBeenCalled();
  });

  it("handles failed asset reads as unavailable images", async () => {
    const resolveImage = vi.fn(async () => { throw new Error("Unavailable"); });
    const { container } = render(<MarkdownView external={{ resolveImage }} markdown="![Missing](missing.png)" />);
    await act(async () => {});
    expect(container.querySelector("img")).toBeNull();
    expect(screen.getByRole("img", { name: "Missing" })).toHaveTextContent("Image unavailable");
  });

  it("typesets math without trusting KaTeX network/HTML commands", async () => {
    const { container } = render(<MarkdownView external markdown={String.raw`$x^2$ and $\includegraphics{https://tracker.test/pixel.png}$ and $\href{https://tracker.test/page}{click}$`} />);
    await waitFor(() => expect(container.querySelector(".katex")).not.toBeNull(), { timeout: 10000 });
    expect(container.querySelectorAll("img, a, iframe")).toHaveLength(0);
    expect(fetch).not.toHaveBeenCalled();
  });
});
