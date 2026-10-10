import { afterEach, describe, expect, it, vi } from "vitest";
vi.mock("../runtime", () => ({ STATIC_MODE: true }));
import { nativeReadingEnabled } from "./useReadingKeybindings";

afterEach(() => { delete window.__trackNativeReading; });
describe("published static build", () => {
  it("never activates native reading bindings even if hosted in a marked WebView", () => {
    window.__trackNativeReading = true;
    expect(nativeReadingEnabled()).toBe(false);
  });
});
