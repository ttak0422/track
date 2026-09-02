import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createEvent, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { ReactElement } from "react";
import { describe, expect, it, vi } from "vitest";
import { MarkdownView } from "./MarkdownView";
import { TaskBoardContext } from "./markdown/context";
import { FloatingProvider } from "./preview/floatingStore";

// FloatingProvider (needed by the include embed's WikiLink header) reads the current route, so stub
// the router the same way WikiLink.test.tsx does.
vi.mock("@tanstack/react-router", () => ({
  useRouterState: () => "/",
  useNavigate: () => vi.fn(),
}));

const copyText = vi.hoisted(() => vi.fn());
const copyRich = vi.hoisted(() => vi.fn());
vi.mock("./markdown/clipboard", () => ({ copyText, copyRich }));

// The selection popover's Confluence action renders its portable markdown through portableToHtml;
// stub the renderer so the wiring test asserts exact clipboard flavors without loading react-dom/server.
const portableToHtml = vi.hoisted(() =>
  vi.fn(async (portable: string) => `<section data-stub>${portable}</section>`),
);
vi.mock("./markdown/portable", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./markdown/portable")>()),
  portableToHtml,
}));

// EChartsBlock lazy-imports echarts; stub it so a chart fence doesn't pull the real (heavy) library
// into this suite and starve the KaTeX lazy-load test of its waitFor budget.
vi.mock("echarts", () => ({
  init: vi.fn(() => ({ setOption: vi.fn(), resize: vi.fn(), dispose: vi.fn() })),
  getInstanceByDom: vi.fn(() => undefined),
}));

// GraphvizDiagram lazy-imports the Graphviz WASM engine; stub it for the same reason.
vi.mock("@hpcc-js/wasm-graphviz", () => ({
  Graphviz: { load: async () => ({ dot: () => '<svg viewBox="0 0 10 10"><text>G</text></svg>' }) },
}));

// Partial mock: only the task write and the OGP fetch are stubbed, so every other api call the view
// makes (asset text, wiki-link resolution) keeps its real implementation. The OGP fetch is stubbed so a
// card renders deterministically instead of degrading to its offline fallback.
const setTaskState = vi.hoisted(() => vi.fn(async () => ({ tasks: { items: [] } })));
const setTaskDate = vi.hoisted(() => vi.fn(async () => ({ tasks: { items: [] } })));
const getOgp = vi.hoisted(() =>
  vi.fn(async (url: string) => ({
    url,
    site_name: "Example",
    title: "Example page",
    description: "A sample page.",
    image: "https://example.com/og.png",
  })),
);
// An embedded excerpt fetches the note it came from, to address its tasks by their own lines.
// eslint-disable-next-line @typescript-eslint/no-explicit-any
const getNote = vi.hoisted(() => vi.fn(async (): Promise<any> => ({ note: { tasks: { items: [] }, etag: "loaded" } })));
vi.mock("../api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../api")>()),
  setTaskState,
  setTaskDate,
  getOgp,
  getNote,
}));

vi.mock("@terrastruct/d2", () => ({
  D2: class {
    compile = async () => ({ diagram: {}, renderOptions: {} });
    render = async () => '<svg viewBox="0 0 10 10"><text>D</text></svg>';
  },
}));

// The draw.io viewer is a vendored script injected at runtime, not an importable module; stub the
// loader with a viewer that draws a marker SVG into the host.
vi.mock("./markdown/drawioViewer", () => ({
  loadDrawioViewer: () =>
    Promise.resolve({
      createViewerForElement: (element: Element) => {
        element.innerHTML = '<svg viewBox="0 0 10 10"><text>X</text></svg>';
      },
    }),
}));

// A QueryClient is only needed for markdown that produces links (ExternalLink/WikiLink) or viewspec
// charts (ViewSpecChart), which call useQuery. Pure block content (tables, task lists, code) renders
// without it.
function renderWithQuery(ui: ReactElement) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>);
}

describe("MarkdownView", () => {
  it("shows a placeholder for empty input", () => {
    render(<MarkdownView markdown="   " />);
    expect(screen.getByText("Empty note.")).toBeInTheDocument();
  });

  it("renders the note title as chrome and keeps an identical leading body h1", () => {
    const markdown = "\n# **Project**\n\n## Status\n\nbody";
    const { container } = render(<MarkdownView title="Project" markdown={markdown} />);
    expect([...container.querySelectorAll("h1")].map((h) => h.textContent)).toEqual(["Project", "Project"]);
    expect(container.querySelector("h1.note-title")).not.toBeNull();
    // The body h1 is an ordinary heading again: it renders with the id the aside's Contents
    // outline links to, and the h2 below it keeps its own.
    expect(container.querySelector("h1:not(.note-title)")?.id).toBe("h-project");
    expect(container.querySelector("h2")?.id).toBe("h-status");
  });

  it("keeps a distinct leading body h1 below the canonical note title", () => {
    const { container } = render(<MarkdownView title="Project" markdown="# Executive summary" />);
    expect([...container.querySelectorAll("h1")].map((h) => h.textContent)).toEqual([
      "Project",
      "Executive summary",
    ]);
  });

  it("renders the body h1 when the popup chrome owns the title row", () => {
    const { container } = render(
      <MarkdownView title="Project" showTitle={false} markdown={"# Project\n\n## Status"} />,
    );
    expect(container.querySelector("h1")?.textContent).toBe("Project");
    expect(container.querySelector("h1")?.id).toBe("h-project");
    expect(container.querySelector("h2")?.textContent).toBe("Status");
  });

  it("renders the note title and empty-state copy for an empty body", () => {
    const { container } = render(<MarkdownView title="Project" markdown="" />);
    expect(container.querySelector("h1")?.textContent).toBe("Project");
    expect(screen.getByText("Empty note.")).toBeInTheDocument();
  });

  it("copies the note title from the title copy button", async () => {
    copyText.mockReset();
    copyText.mockResolvedValue(true);

    render(<MarkdownView title="Project" noteId="note-1" markdown="" />);

    expect(screen.queryByRole("link", { name: "Project" })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Copy title" }));
    await waitFor(() => expect(copyText).toHaveBeenCalledWith("Project"));
    expect(await screen.findByRole("button", { name: "Title copied" })).toBeInTheDocument();
  });

  it("offers a copy range for a cross-block selection and keeps it for a backwards drag", async () => {
    copyText.mockReset();
    copyText.mockResolvedValue(true);
    const { container } = render(
      <MarkdownView copyPath="notes/project.md" markdown={"first block\n\nsecond block"} />,
    );
    const paragraphs = container.querySelectorAll("p");
    expect(paragraphs[0]).toHaveAttribute("data-copy-line-start", "1");
    expect(paragraphs[0]).toHaveAttribute("data-copy-line-end", "1");
    expect(paragraphs[1]).toHaveAttribute("data-copy-line-start", "3");

    const selection = window.getSelection()!;
    const first = paragraphs[0].firstChild!;
    const second = paragraphs[1].firstChild!;
    selection.setBaseAndExtent(first, 1, second, 6);
    fireEvent(document, new Event("selectionchange"));
    expect(screen.getByRole("button", { name: "Copy range" })).toBeInTheDocument();

    selection.setBaseAndExtent(second, 6, first, 1);
    fireEvent(document, new Event("selectionchange"));
    fireEvent.click(screen.getByRole("button", { name: "Copy range" }));
    await waitFor(() => expect(copyText).toHaveBeenCalledWith("notes/project.md:1-3"));
  });

  it("uses a one-line reference for a selection within one source line", async () => {
    copyText.mockReset();
    copyText.mockResolvedValue(true);
    const { container } = render(
      <MarkdownView copyPath="notes/project.md" markdown="single line" />,
    );
    const text = container.querySelector("p")!.firstChild!;
    const selection = window.getSelection()!;
    selection.setBaseAndExtent(text, 0, text, 6);
    fireEvent(document, new Event("selectionchange"));
    fireEvent.click(screen.getByRole("button", { name: "Copy range" }));
    await waitFor(() => expect(copyText).toHaveBeenCalledWith("notes/project.md:1"));
  });

  it("copies the selected lines' markdown source from the copy markdown action", async () => {
    copyText.mockReset();
    copyText.mockResolvedValue(true);
    const { container } = render(
      <MarkdownView copyPath="notes/project.md" markdown={"first block\n\nsecond block"} />,
    );
    const first = container.querySelectorAll("p")[0].firstChild!;
    const second = container.querySelectorAll("p")[1].firstChild!;
    // Lines 1..3 of the source, including the blank separator line between the paragraphs.
    const selection = window.getSelection()!;
    selection.setBaseAndExtent(first, 1, second, 6);
    fireEvent(document, new Event("selectionchange"));
    fireEvent.click(screen.getByRole("button", { name: "Copy markdown" }));
    await waitFor(() => expect(copyText).toHaveBeenCalledWith("first block\n\nsecond block"));
  });

  it("copies just the one selected line's markdown for a single-line selection", async () => {
    copyText.mockReset();
    copyText.mockResolvedValue(true);
    const { container } = render(
      <MarkdownView copyPath="notes/project.md" markdown={"alpha\n\nbeta"} />,
    );
    const second = container.querySelectorAll("p")[1].firstChild!;
    const selection = window.getSelection()!;
    selection.setBaseAndExtent(second, 0, second, 4);
    fireEvent(document, new Event("selectionchange"));
    fireEvent.click(screen.getByRole("button", { name: "Copy markdown" }));
    await waitFor(() => expect(copyText).toHaveBeenCalledWith("beta"));
  });

  it("copies the selected lines re-rendered as rich HTML for Confluence", async () => {
    copyText.mockReset();
    copyRich.mockReset();
    copyRich.mockResolvedValue(true);
    // FloatingProvider wraps the render because the selection spans a wiki link.
    const { container } = renderWithQuery(
      <FloatingProvider>
        <MarkdownView copyPath="notes/project.md" markdown={"see [[Design|API]] first\n\nsecond block"} />
      </FloatingProvider>,
    );
    const first = container.querySelectorAll("p")[0].firstChild!;
    const second = container.querySelectorAll("p")[1].firstChild!;
    const selection = window.getSelection()!;
    selection.setBaseAndExtent(first, 1, second, 6);
    fireEvent(document, new Event("selectionchange"));
    fireEvent.click(screen.getByRole("button", { name: "Copy for Confluence" }));
    // The same selected source Copy markdown takes — wiki links flattened to their alias — paired
    // with the HTML flavor rendered from that portable text.
    await waitFor(() =>
      expect(copyRich).toHaveBeenCalledWith(
        "<section data-stub>see API first\n\nsecond block</section>",
        "see API first\n\nsecond block",
      ),
    );
    expect(await screen.findByRole("button", { name: "Copied" })).toBeInTheDocument();
  });

  it("dismisses the popover once the Confluence copy has confirmed", async () => {
    copyText.mockReset();
    copyRich.mockReset();
    copyRich.mockResolvedValue(true);
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      const { container } = render(
        <MarkdownView copyPath="notes/project.md" markdown="single line" />,
      );
      const text = container.querySelector("p")!.firstChild!;
      const selection = window.getSelection()!;
      selection.setBaseAndExtent(text, 0, text, 6);
      fireEvent(document, new Event("selectionchange"));
      fireEvent.click(screen.getByRole("button", { name: "Copy for Confluence" }));
      await waitFor(() => expect(copyRich).toHaveBeenCalled());
      vi.advanceTimersByTime(1500);
      await waitFor(() =>
        expect(screen.queryByRole("button", { name: /Copy|Copied/ })).not.toBeInTheDocument(),
      );
    } finally {
      vi.useRealTimers();
    }
  });

  it("keeps the whole action row on screen when the selection sits at a window edge", async () => {
    // jsdom measures nothing and its Range has no geometry at all, so stand in for both: the row's
    // real width (three actions, ~336px), a phone-width window, and a selection near an edge. The
    // panel is centred with translateX(-50%), so a centre nearer an edge than half the row would
    // push an action off-screen.
    const width = vi.spyOn(HTMLElement.prototype, "offsetWidth", "get").mockReturnValue(336);
    const innerWidth = window.innerWidth;
    Object.defineProperty(window, "innerWidth", { value: 390, configurable: true });
    const rect = vi.fn(() => ({ left: 4, width: 0, top: 0, bottom: 0 }) as DOMRect);
    Object.defineProperty(Range.prototype, "getBoundingClientRect", { value: rect, configurable: true });
    try {
      const { container } = render(<MarkdownView copyPath="notes/project.md" markdown="single line" />);
      const text = container.querySelector("p")!.firstChild!;
      const selection = window.getSelection()!;
      selection.setBaseAndExtent(text, 0, text, 6);
      fireEvent(document, new Event("selectionchange"));
      // Half the row plus the 8px inset, rather than the selection's own centre at 4px.
      expect(container.querySelector<HTMLElement>(".selection-copy")!.style.left).toBe("176px");

      rect.mockReturnValue({ left: 386, width: 0, top: 0, bottom: 0 } as DOMRect);
      fireEvent(document, new Event("selectionchange"));
      expect(container.querySelector<HTMLElement>(".selection-copy")!.style.left).toBe("214px");
    } finally {
      width.mockRestore();
      delete (Range.prototype as Partial<Range>).getBoundingClientRect;
      Object.defineProperty(window, "innerWidth", { value: innerWidth, configurable: true });
    }
  });

  it("dismisses the copy range popup once it has copied", async () => {
    copyText.mockReset();
    copyText.mockResolvedValue(true);
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      const { container } = render(
        <MarkdownView copyPath="notes/project.md" markdown="single line" />,
      );
      const text = container.querySelector("p")!.firstChild!;
      const selection = window.getSelection()!;
      selection.setBaseAndExtent(text, 0, text, 6);
      fireEvent(document, new Event("selectionchange"));
      fireEvent.click(screen.getByRole("button", { name: "Copy range" }));
      await waitFor(() => expect(copyText).toHaveBeenCalled());
      vi.advanceTimersByTime(1500);
      await waitFor(() =>
        expect(screen.queryByRole("button", { name: /Copy range|Copied/ })).not.toBeInTheDocument(),
      );
    } finally {
      vi.useRealTimers();
    }
  });

  it("resolves a selection inside a list to the items it touches", async () => {
    copyText.mockReset();
    copyText.mockResolvedValue(true);
    const { container } = render(
      <MarkdownView copyPath="notes/project.md" markdown={"intro\n\n- one\n- two\n- three"} />,
    );
    const items = container.querySelectorAll("li");
    expect(items[1]).toHaveAttribute("data-copy-line-start", "4");
    const selection = window.getSelection()!;
    selection.setBaseAndExtent(items[1].firstChild!, 0, items[2].firstChild!, 3);
    fireEvent(document, new Event("selectionchange"));
    fireEvent.click(screen.getByRole("button", { name: "Copy range" }));
    await waitFor(() => expect(copyText).toHaveBeenCalledWith("notes/project.md:4-5"));
  });

  // A reader and every open floating window render a body of their own, and all of them listen on the
  // one document-level selectionchange. Each resolves the range against its own root, so a selection
  // belongs to exactly one of them and is copied with that body's path, not a neighbour's.
  it("answers a selection from the one body that contains it", async () => {
    copyText.mockReset();
    copyText.mockResolvedValue(true);
    const { container } = render(
      <>
        <MarkdownView copyPath="notes/reader.md" markdown="reader line" />
        <MarkdownView copyPath="notes/window.md" markdown="window line" />
      </>,
    );
    const inWindow = container.querySelectorAll("p")[1].firstChild!;
    const selection = window.getSelection()!;
    selection.setBaseAndExtent(inWindow, 0, inWindow, 6);
    fireEvent(document, new Event("selectionchange"));
    expect(container.querySelectorAll(".selection-copy")).toHaveLength(1);
    fireEvent.click(screen.getByRole("button", { name: "Copy range" }));
    await waitFor(() => expect(copyText).toHaveBeenCalledWith("notes/window.md:1"));
  });

  it("offers nothing when the selection has no marked note line", () => {
    const { container } = render(
      <MarkdownView copyPath="notes/project.md" title="Project" markdown="body" />,
    );
    const title = container.querySelector("h1.note-title")!.firstChild!;
    const selection = window.getSelection()!;
    selection.setBaseAndExtent(title, 0, title, title.textContent!.length);
    fireEvent(document, new Event("selectionchange"));
    expect(screen.queryByRole("button", { name: "Copy range" })).not.toBeInTheDocument();
  });

  it("offers nothing for text inside a diagram SVG", () => {
    const { container } = render(<MarkdownView copyPath="notes/project.md" markdown="body" />);
    const paragraph = container.querySelector("p")!;
    const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    const label = document.createElementNS("http://www.w3.org/2000/svg", "text");
    label.textContent = "chart label";
    svg.append(label);
    paragraph.append(svg);
    const text = label.firstChild!;
    const selection = window.getSelection()!;
    selection.setBaseAndExtent(text, 0, text, text.textContent!.length);
    fireEvent(document, new Event("selectionchange"));
    expect(screen.queryByRole("button", { name: "Copy range" })).not.toBeInTheDocument();
  });

  it("gives headings the ids their outline links to, counting repeats", () => {
    const { container } = render(<MarkdownView markdown={"# Intro\n## Intro\n### 設計"} />);
    expect(container.querySelector("h1")?.id).toBe("h-intro");
    expect(container.querySelector("h2")?.id).toBe("h-intro-2");
    expect(container.querySelector("h3")?.id).toBe("h-設計");
  });

  it("keeps heading ids right below an include", async () => {
    // The include splice writes the directive line as a one-line ATX heading token; if id assignment
    // ran before includes resolve, the token would eat the id meant for the heading below it.
    const { container } = renderWithQuery(
      <FloatingProvider>
        <MarkdownView
          markdown={"# First\n\n![[Other]]\n\n# Second"}
          includes={[{ line: 2, title: "Other", caption: "Other", lines: ["excerpt"] }]}
        />
      </FloatingProvider>,
    );
    await waitFor(() =>
      expect([...container.querySelectorAll("h1")].map((h) => h.id)).toEqual(["h-first", "h-second"]),
    );
  });

  it("survives an empty body becoming a real one, as a hover preview does", () => {
    // A preview mounts before its render arrives: every hook must run on both passes, or React
    // throws and the router shows a bare "Something went wrong!".
    const view = renderWithQuery(<MarkdownView markdown="" />);
    expect(() => view.rerender(<MarkdownView markdown={"# Later\n\ntext"} />)).not.toThrow();
  });

  it("does not count a setext heading, which track's parsers do not see", () => {
    // remark parses "Title\n=====" as a heading; the engine's ATX-only scanners do not, so counting
    // it here would shift every later id onto the wrong heading.
    const { container } = render(<MarkdownView markdown={"Setext\n======\n\n# Real"} />);
    expect(container.querySelector("h1#h-real")).not.toBeNull();
  });

  it("renders a GFM table", () => {
    render(<MarkdownView markdown={"| a | b |\n| --- | --- |\n| 1 | 2 |"} />);
    const table = screen.getByRole("table");
    expect(within(table).getByText("a")).toBeInTheDocument();
    expect(within(table).getByText("2")).toBeInTheDocument();
  });

  it("renders a br in a GFM table cell without enabling arbitrary HTML", () => {
    const { container } = render(
      <MarkdownView markdown={"| a | b |\n| --- | --- |\n| first<br/>second | 2 |"} />,
    );
    const cell = container.querySelector("tbody td");
    expect(cell?.textContent).toMatch(/first\s+second/);
    expect(cell?.querySelector("br")).toBeInTheDocument();
    expect(container.querySelector("script")).toBeNull();
  });

  it("keeps a plain GFM checklist as native checkboxes", () => {
    const { container } = render(<MarkdownView markdown={"- [ ] todo\n- [x] done"} />);
    expect(container.querySelectorAll("li.task-row")).toHaveLength(0);
    const boxes = container.querySelectorAll<HTMLInputElement>("input[type='checkbox']");
    expect(boxes).toHaveLength(2);
    expect(boxes[0]).not.toBeChecked();
    expect(boxes[1]).toBeChecked();
  });

  it("upgrades a nested checklist as one table, indenting by depth", () => {
    const { container } = renderWithQuery(
      <MarkdownView markdown={"- [ ] parent\n  - [ ] child [due:2026-01-01]\n  - [ ] sibling\n- [ ] parent2"} />,
    );
    // A sub-list is its own mdast list, but the reader sees one checklist: notation on a child must
    // not leave its parent behind as a bare checkbox.
    expect(container.querySelectorAll("table.task-table")).toHaveLength(1);
    const rows = container.querySelectorAll("tr.task-row");
    expect(rows).toHaveLength(4);
    expect(container.querySelectorAll("input[type='checkbox']")).toHaveLength(0);
    // Document order, with the source's nesting carried as an indent.
    const texts = [...rows].map((row) => row.querySelector("td.task-row-text")?.textContent?.trim());
    expect(texts).toEqual(["parent", "child", "sibling", "parent2"]);
    const indent = (i: number) =>
      (rows[i].querySelector("td.task-row-text") as HTMLElement).style.paddingLeft;
    expect(indent(0)).toBe("");
    expect(indent(1)).not.toBe("");
    expect(indent(3)).toBe("");
  });

  it("keeps a task line with no text in the table", () => {
    const { container } = renderWithQuery(
      <MarkdownView markdown={"- [/]\n- [ ] with text [#A]"} />,
    );
    expect(container.querySelectorAll("tr.task-row")).toHaveLength(2);
  });

  it("upgrades a checklist with task notation to one task table", () => {
    const { container } = renderWithQuery(
      <MarkdownView
        markdown={"- [/] Draft the post [#A] [due:2026-07-24] [1/2]\n- [x] plain done line\n- [?] Wait for sync [sched:2026-07-18]"}
      />,
    );
    // The [x] line has no notation of its own, but its block does — the whole list becomes a table.
    expect(container.querySelectorAll("table.task-table")).toHaveLength(1);
    const rows = container.querySelectorAll("tr.task-row");
    expect(rows).toHaveLength(3);
    expect(container.querySelectorAll("input[type='checkbox']")).toHaveLength(0);
    const badges = container.querySelectorAll(".task-row-state");
    expect(badges[0].textContent).toBe("DOING");
    expect(badges[1].textContent).toBe("DONE");
    expect(badges[2].textContent).toBe("WAITING");
    expect(rows[1]).toHaveClass("task-row-done");
    // The raw notation is gone from the text; priority and cookies become chips, dates move to
    // their own columns.
    expect(rows[0].textContent).not.toContain("[/]");
    expect(rows[0].textContent).not.toContain("[#A]");
    expect(within(rows[0] as HTMLElement).getByText("#A")).toHaveClass("task-chip-priority");
    expect(within(rows[0] as HTMLElement).getByText("1/2")).toHaveClass("task-chip");
    expect(within(rows[0] as HTMLElement).getByText("! 2026-07-24")).toHaveClass("task-row-due");
    expect(within(rows[2] as HTMLElement).getByText("▷ 2026-07-18")).toHaveClass("task-row-date");
  });

  it("sorts the task table by a date column, empties last, and restores source order", () => {
    const { container } = renderWithQuery(
      <MarkdownView
        markdown={"- [ ] late [due:2026-09-01]\n- [ ] none\n- [/] early [due:2026-08-01]"}
      />,
    );
    const firstTask = () => container.querySelector("tr.task-row td.task-row-text")?.textContent;
    expect(firstTask()).toBe("late ");
    const due = screen.getByRole("button", { name: "DUE" });
    fireEvent.click(due); // ascending
    expect(firstTask()).toBe("early ");
    fireEvent.click(screen.getByRole("button", { name: /DUE/ })); // descending
    expect(firstTask()).toBe("late ");
    // The date-less row stays last in both directions.
    const rows = container.querySelectorAll("tr.task-row td.task-row-text");
    expect(rows[rows.length - 1].textContent).toBe("none");
    fireEvent.click(screen.getByRole("button", { name: /DUE/ })); // back to source order
    expect(firstTask()).toBe("late ");
  });

  it("keeps a list that mixes tasks and plain bullets untouched", () => {
    const { container } = render(<MarkdownView markdown={"- [/] a task\n- just a note"} />);
    expect(container.querySelectorAll("table.task-table")).toHaveLength(0);
    expect(screen.getByText("[/] a task")).toBeInTheDocument();
  });

  it("leaves list items whose marker is outside the state set untouched", () => {
    const { container } = render(<MarkdownView markdown={"- [z] not a task\n- plain item"} />);
    expect(container.querySelectorAll("li.task-row")).toHaveLength(0);
    expect(screen.getByText("[z] not a task")).toBeInTheDocument();
  });

  it("makes a plain checklist tickable when the note is editable", async () => {
    const tasks = {
      items: [
        { line: 1, state: "TODO", done: false, text: "todo" },
        { line: 2, state: "DONE", done: true, text: "done" },
      ],
    };
    const { container } = renderWithQuery(
      <TaskBoardContext.Provider value={{ noteID: "100", tasksRef: { current: { tasks, etag: "loaded" } } }}>
        <MarkdownView markdown={"- [ ] todo\n- [x] done"} />
      </TaskBoardContext.Provider>,
    );
    // Still a plain checklist, not the notation table — only the boxes come alive.
    expect(container.querySelectorAll("table.task-table")).toHaveLength(0);
    const boxes = container.querySelectorAll<HTMLInputElement>("input[type='checkbox']");
    expect(boxes).toHaveLength(2);
    expect(boxes[0]).toBeEnabled();
    fireEvent.click(boxes[0]);
    await waitFor(() =>
      expect(setTaskState).toHaveBeenCalledWith("100", 1, "DONE", "TODO", "loaded"),
    );
  });

  it("leaves a plain checklist inert with no note behind it", () => {
    const { container } = render(<MarkdownView markdown={"- [ ] todo"} />);
    expect(container.querySelector<HTMLInputElement>("input[type='checkbox']")).toBeDisabled();
  });

  it("keeps a checklist plain once the engine stamps a completion date", () => {
    const { container } = render(<MarkdownView markdown={"- [ ] todo\n- [x] done [done:2026-07-30]"} />);
    // The stamp is written by the engine, not authored, so it must not promote the list to the
    // four-column table under the user's cursor.
    expect(container.querySelectorAll("table.task-table")).toHaveLength(0);
    expect(container.querySelectorAll("input[type='checkbox']")).toHaveLength(2);
  });

  it("makes the date cells editable where the note can be written", async () => {
    const tasks = { items: [{ line: 1, state: "TODO", done: false, text: "a task", due: "2026-07-24" }] };
    const { container } = renderWithQuery(
      <TaskBoardContext.Provider value={{ noteID: "100", tasksRef: { current: { tasks, etag: "loaded" } } }}>
        <MarkdownView markdown={"- [ ] a task [due:2026-07-24]"} />
      </TaskBoardContext.Provider>,
    );
    const due = container.querySelector<HTMLButtonElement>("td.task-row-due button.task-row-date-input");
    expect(due).not.toBeNull();
    expect(due!.textContent).toContain("2026-07-24");
    // The picker opens on the cell's month, a day click is the working choice, and SAVE writes it.
    fireEvent.click(due!);
    fireEvent.click(screen.getByRole("button", { name: "30" }));
    fireEvent.click(screen.getByRole("button", { name: "SAVE" }));
    // The cell asserts the state it drew, as the state controls do: a date picked against a task
    // that has since moved is refused rather than written onto whatever the line became.
    await waitFor(() =>
      expect(setTaskDate).toHaveBeenCalledWith("100", 1, "due", "2026-07-30", "TODO", "loaded"),
    );
  });

  it("opens the workspace's own calendar on a click, not the browser's", async () => {
    const tasks = { items: [{ line: 1, state: "TODO", done: false, text: "a task", due: "2026-07-24" }] };
    const { container } = renderWithQuery(
      <TaskBoardContext.Provider value={{ noteID: "100", tasksRef: { current: { tasks, etag: "loaded" } } }}>
        <MarkdownView markdown={"- [ ] a task [due:2026-07-24]"} />
      </TaskBoardContext.Provider>,
    );
    const due = container.querySelector<HTMLButtonElement>("td.task-row-due button.task-row-date-input")!;
    fireEvent.click(due);
    const dialog = screen.getByRole("dialog", { name: "Pick a date" });
    expect(dialog).toBeInTheDocument();
    // The month is the cell's own (July 2026 has 31 days), drawn in the workspace's tokens rather
    // than the browser's era-laden native scheme.
    expect(dialog.querySelector(".task-date-month")?.textContent).toBe("2026 / 07");
    expect(dialog.querySelectorAll(".task-date-day")).toHaveLength(31);
    // DELETE clears the token in the same write path SAVE uses.
    fireEvent.click(screen.getByRole("button", { name: "DELETE" }));
    await waitFor(() =>
      expect(setTaskDate).toHaveBeenCalledWith("100", 1, "due", "", "TODO", "loaded"),
    );
  });

  it("keeps the date cells as plain text with no note behind them", () => {
    const { container } = render(<MarkdownView markdown={"- [ ] a task [due:2026-07-24]"} />);
    expect(container.querySelector("button.task-row-date-input")).toBeNull();
    expect(container.querySelector("td.task-row-due")?.textContent).toBe("! 2026-07-24");
  });

  it("wires the badge select by source line, so inline markup does not break it", () => {
    const tasks = { items: [{ line: 1, state: "DOING", done: false, text: "a bold task" }] };
    const { container } = renderWithQuery(
      <TaskBoardContext.Provider value={{ noteID: "100", tasksRef: { current: { tasks, etag: "loaded" } } }}>
        <MarkdownView markdown={"- [/] a **bold** task [#A]"} />
      </TaskBoardContext.Provider>,
    );
    const select = container.querySelector<HTMLSelectElement>("select.task-row-state");
    expect(select).not.toBeNull();
    expect(select!.value).toBe("DOING");
  });

  it("renders a fenced code block through CodeBlock", () => {
    const { container } = render(<MarkdownView markdown={"```js\nconst x = 1\n```"} />);
    expect(container.querySelector(".code-block")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Copy code" })).toBeInTheDocument();
  });

  it("hides the copy button on a fence without an info string", () => {
    const { container } = render(<MarkdownView markdown={"```\nplain text\n```"} />);
    expect(container.querySelector(".code-block")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Copy code" })).not.toBeInTheDocument();
  });

  it("renders Mermaid fences through the diagram component", () => {
    const { container } = render(<MarkdownView markdown={"```mermaid\ngraph TD\nA-->B\n```"} />);
    expect(container.querySelector(".mermaid-diagram")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Copy code" })).not.toBeInTheDocument();
  });

  it("renders dot fences through the Graphviz diagram component", async () => {
    const { container } = render(<MarkdownView markdown={"```dot\ndigraph { a -> b }\n```"} />);
    expect(container.querySelector(".graphviz-diagram")).toBeInTheDocument();
    await waitFor(() => expect(container.querySelector("svg")).toBeInTheDocument());
    expect(screen.getByRole("img", { name: "Graphviz diagram" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Copy code" })).not.toBeInTheDocument();
  });

  it("renders d2 fences through the D2 diagram component", async () => {
    const { container } = render(<MarkdownView markdown={"```d2\na -> b\n```"} />);
    await waitFor(() => expect(container.querySelector("svg")).toBeInTheDocument());
    expect(screen.getByRole("img", { name: "D2 diagram" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Copy code" })).not.toBeInTheDocument();
  });

  it("renders drawio fences through the draw.io viewer component", async () => {
    const { container } = render(
      <MarkdownView markdown={"```drawio\n<mxGraphModel><root><mxCell id='0'/></root></mxGraphModel>\n```"} />,
    );
    await waitFor(() => expect(container.querySelector("svg")).toBeInTheDocument());
    expect(screen.getByRole("img", { name: "draw.io diagram" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Copy code" })).not.toBeInTheDocument();
  });

  it("renders a heading-based mindmap fence as an SVG tree", () => {
    const { container } = render(<MarkdownView markdown={"```mindmap\n# Root\n## Leaf\n```"} />);
    const svg = container.querySelector(".mindmap-diagram svg");
    expect(svg).toBeInTheDocument();
    expect(svg?.textContent).toContain("Root");
    expect(svg?.textContent).toContain("Leaf");
  });

  it("maps the note's heading tree for an empty mindmap fence", () => {
    const markdown = "# Note\n\n## Alpha\n\n## Beta\n\n```mindmap\n```";
    const { container } = render(<MarkdownView markdown={markdown} />);
    const svg = container.querySelector(".mindmap-diagram svg");
    expect(svg?.textContent).toContain("Note");
    expect(svg?.textContent).toContain("Alpha");
    expect(svg?.textContent).toContain("Beta");
  });

  it("renders GFM footnotes with linked reference and back-link", () => {
    const { container } = render(
      <MarkdownView markdown={"A claim.[^1]\n\n[^1]: The source of the claim."} />,
    );
    const ref = container.querySelector("sup a.footnote-ref") as HTMLAnchorElement;
    expect(ref).toBeInTheDocument();
    expect(ref.getAttribute("href")).toBe("#user-content-fn-1");
    expect(ref.id).toBe("user-content-fnref-1");

    const section = container.querySelector("section.footnotes");
    expect(section).toBeInTheDocument();
    expect(section?.textContent).toContain("The source of the claim.");
    const backref = section?.querySelector("a.footnote-backref") as HTMLAnchorElement;
    expect(backref.getAttribute("href")).toBe("#user-content-fnref-1");
    expect(backref).toHaveAttribute("title", "Back to reference 1");
    expect(container.querySelector("#user-content-fn-1")).toBeInTheDocument();
  });

  it("renders one back-link for each reference to the same footnote", () => {
    const { container } = render(
      <MarkdownView markdown={"First claim.[^source] Second claim.[^source]\n\n[^source]: Shared source."} />,
    );

    const refs = container.querySelectorAll("a.footnote-ref");
    const backrefs = container.querySelectorAll("a.footnote-backref");
    expect(refs).toHaveLength(2);
    expect(backrefs).toHaveLength(2);
    for (const backref of backrefs) {
      const target = backref.getAttribute("href")?.slice(1);
      expect(target).toBeTruthy();
      expect(container.querySelector(`#${target}`)).toBeInTheDocument();
    }
  });

  it("renders viewspec fences through the chart component", () => {
    const { container } = renderWithQuery(<MarkdownView markdown={'```viewspec\n{"version":2}\n```'} />);
    expect(container.querySelector(".viewspec-chart")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Copy code" })).not.toBeInTheDocument();
  });

  it("renders echarts fences as a chart container, and bad JSON as a code block", () => {
    const { container } = render(<MarkdownView markdown={'```echarts\n{"series":[]}\n```'} />);
    expect(container.querySelector(".viewspec-chart")).toBeInTheDocument();

    const { container: bad } = render(<MarkdownView markdown={"```echarts\nnot json\n```"} />);
    expect(bad.querySelector(".viewspec-chart")).not.toBeInTheDocument();
    expect(bad.querySelector(".code-block")).toBeInTheDocument();
  });

  it("renders an external link that opens in a new tab", () => {
    renderWithQuery(<MarkdownView markdown="[example](https://example.com)" />);
    const link = screen.getByRole("link", { name: "example" });
    expect(link).toHaveAttribute("href", "https://example.com");
    expect(link).toHaveAttribute("target", "_blank");
  });

  it("renders inline and block math with KaTeX (loaded lazily)", async () => {
    const { container } = render(<MarkdownView markdown={"inline $a^2+b^2$\n\n$$\n\\int_0^1 x\\,dx\n$$"} />);
    // KaTeX is imported on demand, so the .katex spans appear once the chunk resolves. A block
    // ($$…$$) is wrapped in .katex-display. Resolving the chunk competes with every other test file
    // under a parallel run, so give it well past waitFor's default 1s — this was a recurring flake.
    await waitFor(() => expect(container.querySelectorAll(".katex").length).toBeGreaterThanOrEqual(2), {
      timeout: 10_000,
    });
    expect(container.querySelector(".katex-display")).toBeInTheDocument();
  });

  it("anchors a ^id-marked paragraph and list item, hiding the marker", () => {
    const { container } = render(
      <MarkdownView markdown={"A marked paragraph. ^para\n\n- item one ^item\n- item two"} />,
    );
    const para = container.querySelector("#block-para");
    expect(para?.tagName).toBe("P");
    expect(para?.textContent).toBe("A marked paragraph.");
    const item = container.querySelector("#block-item");
    expect(item?.tagName).toBe("LI");
    expect(item?.textContent).toBe("item one");
    expect(container.textContent).not.toContain("^para");
    expect(container.textContent).not.toContain("^item");
  });

  it("leaves a lone ^id paragraph as prose (a marker needs content on its line)", () => {
    const { container } = render(<MarkdownView markdown={"^orphan"} />);
    expect(container.querySelector("#block-orphan")).toBeNull();
    expect(container.textContent).toContain("^orphan");
  });

  it("renders a [!NOTE] blockquote as a titled callout and leaves plain quotes alone", () => {
    const { container } = render(<MarkdownView markdown={"> [!NOTE]\n> body text"} />);
    const alert = container.querySelector(".md-alert.md-alert-note");
    expect(alert).not.toBeNull();
    expect(within(alert as HTMLElement).getByText("Note")).toBeInTheDocument();
    expect(alert?.textContent).toContain("body text");
    // The marker itself is stripped from the body.
    expect(container.textContent).not.toContain("[!NOTE]");

    const { container: quote } = render(<MarkdownView markdown={"> just a quote"} />);
    expect(quote.querySelector(".md-alert")).toBeNull();
    expect(quote.querySelector("blockquote")).not.toBeNull();
  });

  it("applies a :height embed option to an HTML embed and strips the option tail", () => {
    const { container } = render(<MarkdownView markdown={"![Demo](assets/x.html) :height 240"} />);
    const frame = container.querySelector(".embed-html iframe") as HTMLIFrameElement | null;
    expect(frame).not.toBeNull();
    expect(frame?.style.height).toBe("240px");
    // The option tail is consumed, not rendered as text.
    expect(container.textContent).not.toContain(":height");

    // A percentage is treated as viewport height (vh), since a normal-flow iframe has no % basis.
    const { container: pct } = render(<MarkdownView markdown={"![Demo](assets/x.html) :height 90%"} />);
    expect((pct.querySelector(".embed-html iframe") as HTMLIFrameElement).style.height).toBe("90vh");
  });

  it("removes the HTML frame with :frame none without changing the sandbox", () => {
    const { container: defaultEmbed } = render(<MarkdownView markdown={"![Demo](assets/x.html)"} />);
    const defaultFrame = defaultEmbed.querySelector(".embed-html") as HTMLElement;
    expect(defaultFrame).not.toHaveClass("embed-html-frame-none");
    expect(defaultFrame.querySelector("iframe")).toHaveAttribute(
      "sandbox",
      "allow-scripts allow-popups allow-popups-to-escape-sandbox allow-downloads allow-modals",
    );

    const { container } = render(<MarkdownView markdown={"![Demo](assets/x.html) :frame none"} />);
    const frame = container.querySelector(".embed-html") as HTMLElement;
    expect(frame).toHaveClass("embed-html-frame-none");
    expect(frame.querySelector("iframe")).toHaveAttribute(
      "sandbox",
      "allow-scripts allow-popups allow-popups-to-escape-sandbox allow-downloads allow-modals",
    );
    expect(container.textContent).not.toContain(":frame");
  });

  it("mounts only a vault-local HTML asset in the sandboxed frame with clipboard write allowed", () => {
    const { container } = render(<MarkdownView markdown={"![Demo](assets/demo.html)"} />);
    const iframe = container.querySelector(".embed-html iframe") as HTMLIFrameElement | null;
    expect(iframe).not.toBeNull();
    expect(iframe).toHaveAttribute(
      "sandbox",
      "allow-scripts allow-popups allow-popups-to-escape-sandbox allow-downloads allow-modals",
    );
    // Popups from the frame escape into ordinary tabs, and the document may write to the clipboard.
    expect(iframe).toHaveAttribute("allow", "clipboard-write");
  });

  it("renders a remote .html URL as an Open Graph card instead of an HTML frame", () => {
    const { container } = renderWithQuery(
      <MarkdownView markdown={"![Page](https://example.com/page.html)"} />,
    );
    // Only vault-local assets mount an HTML iframe; a remote .html page falls through to the card.
    expect(container.querySelector(".embed-html")).toBeNull();
    expect(container.querySelector(".ogp-card")).not.toBeNull();
  });

  it("applies multiple embed options from the same tail", () => {
    const { container } = render(
      <MarkdownView markdown={"![Demo](assets/x.html) :height 400 :frame none"} />,
    );
    const frame = container.querySelector(".embed-html") as HTMLElement;
    expect(frame).toHaveClass("embed-html-frame-none");
    expect((frame.querySelector("iframe") as HTMLIFrameElement).style.height).toBe("400px");
  });

  it("renders an embed sharing a paragraph with text as a sibling of that text", () => {
    // A block embed left inside a <p> loses its margins to anonymous blocks (it ends up flush against
    // the line below it), is capped at the prose measure instead of the column, and makes the
    // prerendered static HTML invalid — so it is hoisted out whether or not blank lines surround it.
    const { container } = render(
      <MarkdownView markdown={"foo\n![y](https://www.youtube.com/watch?v=abcdefghijk)\nbar"} />,
    );
    const view = container.querySelector(".markdown-view");
    expect(view?.querySelector("p .embed")).toBeNull();
    expect([...(view?.children ?? [])].map((el) => el.className)).toEqual([
      "",
      "embed embed-video",
      "",
    ]);
  });

  it("renders a resolved include as an embed card in place of its directive line", () => {
    // The embed header's WikiLink needs the floating-window store (same as WikiLink.test.tsx).
    const { container } = renderWithQuery(
      <FloatingProvider>
        <MarkdownView
          markdown={"before\n\n![[Design##API]] :only-contents\n\nafter"}
          includes={[{ line: 2, title: "Design", caption: "Design##API", lines: ["embedded line"] }]}
        />
      </FloatingProvider>,
    );
    const card = container.querySelector(".note-include");
    expect(card).not.toBeNull();
    expect(within(card as HTMLElement).getByText("embedded line")).toBeInTheDocument();
    // The caption header links back to the source note; the raw directive text is gone.
    expect(within(card as HTMLElement).getByText("Design##API")).toBeInTheDocument();
    expect(container.textContent).not.toContain(":only-contents");
  });

  it("writes an embedded task back to the note that owns it, at its own line", async () => {
    // The excerpt starts at the source note's line 10 (0-based source_line 9), so its first line
    // is that file's line 10 — not line 1 of the host note.
    const sourceTasks = { items: [{ line: 10, state: "TODO", done: false, text: "owned task" }] };
    getNote.mockResolvedValue({ note: { tasks: sourceTasks, etag: "source-etag" } });
    const { container } = renderWithQuery(
      <FloatingProvider>
        <MarkdownView
          markdown={"host\n\n![[Design##Plan]]"}
          includes={[
            {
              line: 2,
              note_id: 42,
              title: "Design",
              caption: "Design##Plan",
              lines: ["- [ ] owned task"],
              source_line: 9,
              etag: "source-etag",
            },
          ]}
        />
      </FloatingProvider>,
    );
    const card = container.querySelector(".note-include") as HTMLElement;
    const box = await waitFor(() => {
      const found = within(card).getByRole("checkbox");
      expect(found).toBeEnabled();
      return found;
    });
    fireEvent.click(box);
    await waitFor(() =>
      expect(setTaskState).toHaveBeenCalledWith("42", 10, "DONE", "TODO", "source-etag"),
    );
  });

  it("leaves an embedded task inert when the excerpt is not one run of the source", async () => {
    getNote.mockResolvedValue({ note: { tasks: { items: [{ line: 1, state: "TODO", done: false, text: "t" }] } } });
    const { container } = renderWithQuery(
      <FloatingProvider>
        <MarkdownView
          markdown={"![[Design]] :lines 1,3"}
          includes={[
            { line: 0, note_id: 42, title: "Design", caption: "Design", lines: ["- [ ] t"], source_line: -1 },
          ]}
        />
      </FloatingProvider>,
    );
    const card = container.querySelector(".note-include") as HTMLElement;
    expect(within(card).getByRole("checkbox")).toBeDisabled();
  });

  it("leaves an embedded task inert when its source has changed since the excerpt was rendered", async () => {
    getNote.mockResolvedValue({
      note: { tasks: { items: [{ line: 10, state: "TODO", done: false, text: "new task" }] }, etag: "new" },
    });
    const { container } = renderWithQuery(
      <FloatingProvider>
        <MarkdownView
          markdown={"![[Design]]"}
          includes={[
            {
              line: 0,
              note_id: 42,
              title: "Design",
              caption: "Design",
              lines: ["- [ ] old task"],
              source_line: 9,
              etag: "old",
            },
          ]}
        />
      </FloatingProvider>,
    );
    const box = await waitFor(() => within(container.querySelector(".note-include")!).getByRole("checkbox"));
    expect(box).toBeDisabled();
  });

  it("renders an include error as a warning card instead of dropping the line", () => {
    const { container } = renderWithQuery(
      <MarkdownView
        markdown={"![[Nope]]"}
        includes={[{ line: 0, caption: "Nope", lines: [], error: 'unresolved note "Nope"' }]}
      />,
    );
    expect(container.querySelector(".note-include-error")?.textContent).toContain(
      'unresolved note "Nope"',
    );
  });
});
