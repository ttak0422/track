import { act, fireEvent, render } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ExternalLink } from "./ExternalLink";
import { previewOpenDelay } from "../preview/stack";

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: ReactNode }) => <a>{children}</a>,
}));

vi.mock("../../queries", () => ({
  useResolveQuery: () => ({ data: undefined, isPending: false }),
}));

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
