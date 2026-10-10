import { describe, expect, it } from "vitest";
import { sanitizeD2Svg } from "./d2Svg";

describe("D2 SVG boundary", () => {
  it("keeps local markers, generated theme CSS and embedded fonts", () => {
    const svg = sanitizeD2Svg(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 120 80"><style>@font-face{font-family:d2;src:url("data:application/font-woff;base64,YWJj")} .label{fill:red}</style><defs><marker id="arrow"/></defs><path marker-end="url(#arrow)"/><text class="label">API</text></svg>`);
    expect(svg).toContain("data:application/font-woff;base64,YWJj");
    expect(svg).toContain("url(#arrow)");
    expect(svg).toContain("API");
    expect(svg).toContain('viewBox="0 0 120 80"');
  });

  it("removes active content, links and remote icons before DOM insertion", () => {
    const svg = sanitizeD2Svg(`<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"><script>alert(1)</script><a href="javascript:alert(1)"><text>API</text></a><image href="https://tracker.test/pixel"/><foreignObject><iframe src="https://tracker.test"/></foreignObject><set attributeName="href" to="https://tracker.test"/><use href="https://tracker.test/x.svg#x"/></svg>`);
    expect(svg).toContain("API");
    expect(svg).not.toMatch(/script|onload|javascript:|https:|foreignObject|<image|<a\s|<set/);
  });

  it.each([
    'text{fill:url(https://tracker.test/pixel)}',
    '@import "https://tracker.test/style";',
    'text{fill:u\\72l(https://tracker.test/pixel)}',
    'text{fill:u/**/rl(https://tracker.test/pixel)}',
  ])("drops a stylesheet with nonlocal or obfuscated resources: %s", (css) => {
    const svg = sanitizeD2Svg(`<svg xmlns="http://www.w3.org/2000/svg"><style>${css}</style><text>API</text></svg>`);
    expect(svg).not.toContain("<style");
    expect(svg).toContain("API");
  });
});
