import { render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { FeatureBoundary } from "./FeatureBoundary";

afterEach(() => vi.restoreAllMocks());
it("contains an optional renderer failure and permits later independent actions", () => {
  vi.spyOn(console,"error").mockImplementation(() => {});
  vi.spyOn(console,"warn").mockImplementation(() => {});
  function Broken(): never { throw new Error("synthetic optional renderer"); }
  render(<><p>Readable body</p><button onClick={(e) => {e.currentTarget.textContent="Navigated";}}>Navigate</button><FeatureBoundary name="Graph"><Broken /></FeatureBoundary></>);
  expect(screen.getByText("Readable body")).toBeTruthy();
  expect(screen.getByText(/Graph is unavailable/)).toBeTruthy();
  screen.getByText("Navigate").click();
  expect(screen.getByText("Navigated")).toBeTruthy();
});
