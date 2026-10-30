import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, within } from "@testing-library/react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";
import { MarkdownView } from "./MarkdownView";
import fixture from "./markdown/fixtures/image-captions.md?raw";
import { FloatingLayer } from "./preview/FloatingLayer";
import { FloatingProvider } from "./preview/floatingStore";

vi.mock("@tanstack/react-router", () => ({
  useRouterState: () => "/",
  useNavigate: () => vi.fn(),
}));

// Exercise Embed's PDF routing without loading pdf.js or requiring a layout observer in jsdom.
vi.mock("./PdfDeck", () => ({ PdfDeck: () => <span data-testid="pdf-deck" /> }));

const caption = "A & B <script>alert('caption')</script> — 日本語";

function reader(markdown = fixture) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={client}>
    <FloatingProvider>
      <MarkdownView markdown={markdown} />
      <FloatingLayer />
    </FloatingProvider>
  </QueryClientProvider>;
}

describe("Markdown image captions", () => {
  it("renders nonempty titles as text while preserving alt, source, and lazy decoding", () => {
    const { container, getByAltText } = render(reader());
    const image = getByAltText("A red square");
    expect(image).toHaveAttribute("src", "/api/asset?kind=note&name=red.png");
    expect(image).toHaveAttribute("loading", "lazy");
    expect(image).toHaveAttribute("decoding", "async");
    expect(image.nextElementSibling).toHaveTextContent(caption);
    expect(container.querySelector("script")).toBeNull();
    expect([...container.querySelectorAll(".image-caption")].map((el) => el.textContent))
      .toEqual([caption, "Between", "Nested", "Table caption"]);
    for (const alt of ["Alt only", "Empty title", "Whitespace title"]) {
      expect(getByAltText(alt).closest(".image-with-caption")).toBeNull();
    }
  });

  it("keeps surrounding prose and table/emphasis ancestry without block elements inside paragraphs", () => {
    const { container, getByAltText } = render(reader());
    expect(getByAltText("Nested image").closest("em")).toHaveTextContent("Nested");
    expect(getByAltText("Table image").closest("td")).toHaveTextContent("Table caption");
    expect(container.querySelector("p div, p figure, em div, em figure, p dialog")).toBeNull();
    expect(container.textContent).toContain("Before");
    expect(container.textContent).toContain("after.");
  });

  it("preserves captions in a preview and the enlarge dialog without adding nested preview controls", () => {
    const { container, getByLabelText } = render(reader('![Accessible alt](assets/red.png "Caption")'));
    fireEvent.click(getByLabelText("Preview"));
    const preview = container.querySelector(".wiki-preview")!;
    expect(preview.querySelector(".image-caption")).toHaveTextContent("Caption");
    expect(preview.querySelector("img")).toHaveAttribute("alt", "Accessible alt");
    expect(preview.querySelector(".media-controls")).toBeNull();
    fireEvent.click(getByLabelText("Enlarge"));
    expect(container.querySelector(".wiki-preview")).toBeNull();
    const dialog = document.querySelector("dialog.media-lightbox")!;
    expect(dialog.querySelector(".image-caption")).toHaveTextContent("Caption");
    expect(within(dialog as HTMLElement).getByAltText("Accessible alt")).toBeInTheDocument();
    fireEvent.click(dialog);
    expect(document.querySelector("dialog.media-lightbox")).toBeNull();
  });

  it("retains untitled images and their enlarge action", () => {
    const { getByLabelText, container } = render(reader("![Alt only](assets/plain.png)"));
    expect(container.querySelector(".image-caption")).toBeNull();
    fireEvent.click(getByLabelText("Enlarge"));
    const dialog = document.querySelector("dialog.media-lightbox")!;
    expect(within(dialog as HTMLElement).getByAltText("Alt only")).toBeInTheDocument();
    fireEvent.click(dialog);
  });

  it("does not borrow a kept preview's caption from another occurrence of the same image", () => {
    const { container, getAllByLabelText } = render(reader([
      '![First](assets/same.png)',
      '![Second](assets/same.png "Second caption")',
      '![Third](assets/same.png "Third caption")',
    ].join("\n\n")));
    const buttons = getAllByLabelText("Preview");
    buttons.forEach((button) => fireEvent.click(button));
    expect(container.querySelectorAll(".wiki-preview")).toHaveLength(3);
    expect([...container.querySelectorAll(".wiki-preview .image-caption")].map((el) => el.textContent))
      .toEqual(["Second caption", "Third caption"]);
    // Reopening the same titled occurrence still raises its existing window.
    fireEvent.click(buttons[1]);
    expect(container.querySelectorAll(".wiki-preview")).toHaveLength(3);
  });

  it("does not caption other embed types", () => {
    const markdown = [
      '![Video](https://www.youtube.com/watch?v=abcdefghijk "Video caption")',
      '![PDF](assets/book.pdf "PDF caption")',
      '![Page](https://example.com/page "OGP caption")',
      '![HTML](assets/demo.html "HTML caption")',
      '![Diagram](assets/diagram.mmd "Diagram caption")',
    ].join("\n\n");
    const { container } = render(reader(markdown));
    expect(container.querySelector(".image-caption")).toBeNull();
    expect(container.querySelector(".embed-video iframe")).toHaveAttribute("title", "Video");
    expect(container.querySelector(".embed-html iframe")).toHaveAttribute("title", "HTML");
    expect(container.querySelector('[data-testid="pdf-deck"]')).not.toBeNull();
  });

  it("survives static HTML parsing with captions in their original cells and text safely escaped", () => {
    const html = renderToStaticMarkup(reader());
    expect(html).toContain("&lt;script&gt;");
    const parsed = new DOMParser().parseFromString(html, "text/html");
    expect(parsed.querySelector("script")).toBeNull();
    expect(parsed.querySelector("td .image-caption")?.textContent).toBe("Table caption");
    expect(parsed.querySelector("em .image-caption")?.textContent).toBe("Nested");
    expect(parsed.querySelector(".image-caption")?.textContent).toBe(caption);
    expect(parsed.querySelectorAll(".image-caption")).toHaveLength(4);
  });
});
