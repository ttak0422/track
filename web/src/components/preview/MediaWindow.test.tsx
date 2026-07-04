import { describe, expect, it } from "vitest";
import { mediaTitle } from "./MediaWindow";

describe("mediaTitle", () => {
  it("uses the alt text when the embed provides one", () => {
    expect(mediaTitle("pod.yaml", "assets/A32styehPAB.yaml", true)).toBe("pod.yaml");
  });

  it("keeps the real file name in the live workspace", () => {
    expect(mediaTitle("", "assets/pod.yaml", false)).toBe("pod.yaml");
  });

  it("falls back to file.<ext> for a content-hashed asset in the static export", () => {
    expect(mediaTitle("", "assets/A32styehPAB.yaml", true)).toBe("file.yaml");
  });

  it("returns Media when a hashed asset has no extension", () => {
    expect(mediaTitle("", "assets/A32styehPAB", true)).toBe("Media");
  });

  it("ignores a query string when deriving the extension", () => {
    expect(mediaTitle("", "assets/A32styehPAB.pdf?v=2", true)).toBe("file.pdf");
  });
});
