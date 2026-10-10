import { describe, expect, it } from "vitest";
import { safeDiagramSvg } from "./safeDiagramSvg";

describe("safeDiagramSvg", () => {
  it("preserves generated geometry, labels and viewBox", () => {
    const svg = safeDiagramSvg('<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 80"><g transform="translate(4 76)"><path fill="#eee" stroke="#333" d="M 0 0 L 40 50"/><text x="5" y="8">注文 API</text></g></svg>');
    expect(svg).toContain('viewBox="0 0 100 80"');
    expect(svg).toContain('transform="translate(4 76)"');
    expect(svg).toContain("注文 API");
  });

  it("removes active content, external references and CSS from untrusted SVG", () => {
    const svg = safeDiagramSvg(`<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" onload="alert(1)">
      <script>alert(1)</script><style>@import 'https://attacker.invalid/a'; body{display:none}</style>
      <foreignObject><div xmlns="http://www.w3.org/1999/xhtml">html</div></foreignObject>
      <image href="https://attacker.invalid/image"/><use xlink:href="https://attacker.invalid/file#shape"/>
      <a href="javascript:alert(1)"><text>label</text></a>
      <path id="outside" style="fill:url(https://attacker.invalid/fill)" fill="url(https://attacker.invalid/fill)"/>
      <path fill="u/**/rl(https://attacker.invalid/fill)"/>
      <animate attributeName="href" to="javascript:alert(1)"/>
    </svg>`);
    expect(svg).toContain("label");
    expect(svg).not.toMatch(/script|style|foreignObject|image|<use|<a[ >]|onload|attacker|javascript|animate|id=/i);
  });

  it("rejects non-SVG results instead of inserting arbitrary markup", () => {
    expect(() => safeDiagramSvg("<div>not SVG</div>")).toThrow("valid SVG");
  });
});
