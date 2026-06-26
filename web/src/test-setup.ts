// Extends vitest's expect with @testing-library/jest-dom matchers (toBeInTheDocument, toBeChecked, …)
// and cleans up the DOM between tests.
import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterEach } from "vitest";

afterEach(() => {
  cleanup();
});
