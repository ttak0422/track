import { describe, expect, it } from "vitest";
import { graphPointAnchor } from "./GraphFullView";

describe("graphPointAnchor", () => {
  it("uses the clicked graph point as the floating preview anchor", () => {
    expect(graphPointAnchor({ x: 320, y: 180 })).toEqual({
      linkLeft: 320,
      linkRight: 320,
      linkTop: 180,
      linkBottom: 180,
    });
  });
});
