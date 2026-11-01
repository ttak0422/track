import { fireEvent, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { RenderFailure } from "./RenderFailure";

it("keeps failed-render source escaped and retries only on request", () => {
 const retry=vi.fn();
 render(<RenderFailure source={'<script>unsafe()</script>\n- [ ] task'} retry={retry} retrying={false} />);
 expect(document.querySelector("script")).toBeNull();
 expect(screen.queryByRole("checkbox")).toBeNull();
 expect(screen.getByText(/unsafe/)).toBeTruthy();
 expect(retry).not.toHaveBeenCalled();
 fireEvent.click(screen.getByText("Retry preview"));
 expect(retry).toHaveBeenCalledTimes(1);
});
