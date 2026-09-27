import { act, fireEvent, render } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ExternalLink } from "./ExternalLink";
import { previewOpenDelay } from "../preview/stack";
import { NoteVaultContext } from "./context";

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: ReactNode }) => <a>{children}</a>,
}));

vi.mock("../../queries", () => ({
  useResolveQuery: () => ({ data: undefined, isPending: false }),
  useSiteQuery: () => ({ data: undefined }),
}));

describe("ExternalLink static app launches", () => {
  it("opens a vault-relative app as a top-level launch link and keeps app params", () => {
    const { getByRole } = render(
      <NoteVaultContext.Provider value="work">
        <ExternalLink href="apps/dashboard/?tab=weekly&vault=app#tasks">dashboard</ExternalLink>
      </NoteVaultContext.Provider>,
    );
    const link = getByRole("link", { name: "dashboard" });
    expect(link).toHaveAttribute(
      "href",
      "/apps/dashboard/?tab=weekly&vault=app&__track_vault=work#tasks",
    );
    expect(link).not.toHaveAttribute("target");
  });
});

describe("ExternalLink URL popup", () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it("reveals the final URL after hover intent", async () => {
    const { container } = render(<ExternalLink href="yahoo.co.jp">yahoo</ExternalLink>);
    const wrap = container.querySelector(".md-link-url-wrap")!;

    fireEvent.mouseEnter(wrap);
    await act(async () => vi.advanceTimersByTime(previewOpenDelay - 1));
    expect(container.querySelector(".md-link-url-popup")).toBeNull();

    await act(async () => vi.advanceTimersByTime(1));
    expect(container.querySelector(".md-link-url-popup")).toHaveTextContent("https://yahoo.co.jp");
  });

  it("does not open on a pointer that cannot hover", async () => {
    vi.stubGlobal("matchMedia", vi.fn(() => ({ matches: true })));
    const { container } = render(<ExternalLink href="https://example.com">example</ExternalLink>);
    fireEvent.mouseEnter(container.querySelector(".md-link-url-wrap")!);
    await act(async () => vi.advanceTimersByTime(previewOpenDelay + 1));
    expect(container.querySelector(".md-link-url-popup")).toBeNull();
  });
});
