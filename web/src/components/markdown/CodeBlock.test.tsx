import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { CodeBlock } from "./CodeBlock";

describe("CodeBlock", () => {
  beforeEach(() => {
    Object.assign(navigator, {
      clipboard: { writeText: vi.fn().mockResolvedValue(undefined) },
    });
  });

  it("renders the code and tags keywords with a highlight class", () => {
    const { container } = render(<CodeBlock lang="js" text="const x = 1" />);
    expect(container.querySelector(".code-block")).toHaveAttribute("data-language", "js");
    // The keyword and the number land in classified spans.
    expect(screen.getByText("const")).toHaveClass("syntax-keyword");
    expect(screen.getByText("1")).toHaveClass("syntax-number");
  });

  it("copies to the clipboard and acknowledges via the button label", async () => {
    render(<CodeBlock lang="" text="plain text" />);
    const button = screen.getByRole("button", { name: "Copy code" });
    button.click();
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith("plain text");
    await waitFor(() => expect(screen.getByRole("button", { name: "Copied" })).toBeInTheDocument());
  });
});
