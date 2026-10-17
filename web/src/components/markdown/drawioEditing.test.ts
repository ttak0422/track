import { describe, expect, it } from "vitest";
import { MAX_DRAWIO_XML, replaceDrawioFence, validDrawioXML } from "./drawioEditing";

const xml = '<mxfile><diagram id="one"><mxGraphModel><root /></mxGraphModel></diagram><diagram id="two">compressed-payload</diagram></mxfile>';
const changed = xml.replace('id="one"', 'id="edited"');
const source = `Intro 😀\n\n\`\`\`\`drawio :name architecture\n${xml}\n\`\`\`\`\n\n\`\`\`drawio\n${xml}\n\`\`\`\nEnd\n`;
const fence = { id: "test", start_line: 6, end_line: 8, xml };

describe("draw.io draft replacement", () => {
  it("replaces only the selected duplicate and retains the other XML pages and all surrounding bytes", () => {
    expect(replaceDrawioFence(source, source, fence, changed)).toBe(source.slice(0, source.lastIndexOf(xml)) + changed + source.slice(source.lastIndexOf(xml) + xml.length));
  });
  it("preserves longer fences, header arguments, CRLF and non-ASCII text", () => {
    const crlf = source.replace(/\n/g, "\r\n");
    const first = { id: "first", start_line: 2, end_line: 4, xml: xml + "\r" };
    expect(replaceDrawioFence(crlf, crlf, first, changed)).toBe(crlf.replace(xml, changed));
  });
  it("rejects any intervening draft change, even if the selected XML is unchanged", () => {
    expect(replaceDrawioFence(source + "new", source, fence, changed)).toBeNull();
    expect(replaceDrawioFence(source, source, { ...fence, xml: "wrong" }, changed)).toBeNull();
    expect(replaceDrawioFence(source, source, { ...fence, end_line: 80 }, changed)).toBeNull();
  });
  it("rejects XML that could break out of a Markdown fence", () => {
    expect(replaceDrawioFence(source, source, fence, '<mxfile>\n```\n</mxfile>')).toBeNull();
  });
  it("does not sanitize or regenerate source XML", () => {
    const editable = '<mxfile><diagram id="compressed">payload+==</diagram></mxfile>';
    expect(replaceDrawioFence(source, source, fence, editable)).toContain(editable);
  });
});

describe("draw.io XML validation", () => {
  it.each([xml, "<mxGraphModel><root /></mxGraphModel>"])("accepts saved draw.io documents", (input) => expect(validDrawioXML(input)).toBe(true));
  it.each(["", "<svg />", "<mxfile>", '<!DOCTYPE mxfile [<!ENTITY x "x">]><mxfile>&x;</mxfile>', "x".repeat(MAX_DRAWIO_XML + 1), null])("rejects invalid documents", (input) => expect(validDrawioXML(input)).toBe(false));
});
