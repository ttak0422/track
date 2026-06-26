import { describe, expect, it } from "vitest";
import { normalizeCodeLang, tokenizeCode } from "./highlight";

describe("normalizeCodeLang", () => {
  it("lowercases, trims, and drops a language- prefix", () => {
    expect(normalizeCodeLang("language-TS")).toBe("ts");
    expect(normalizeCodeLang("  Go  ")).toBe("go");
    expect(normalizeCodeLang("")).toBe("");
  });
});

// className lookup helper for asserting a span carries the expected class.
function classOf(tokens: { text: string; className?: string }[], text: string): string | undefined {
  return tokens.find((t) => t.text === text)?.className;
}

describe("tokenizeCode", () => {
  it("returns a single plain token when the language is unknown/empty", () => {
    expect(tokenizeCode("x = 1", "")).toEqual([{ text: "x = 1" }]);
  });

  it("classifies keywords, numbers, and call expressions", () => {
    const tokens = tokenizeCode("const x = foo(1)", "js");
    expect(classOf(tokens, "const")).toBe("syntax-keyword");
    expect(classOf(tokens, "1")).toBe("syntax-number");
    expect(classOf(tokens, "foo")).toBe("syntax-function");
  });

  it("classifies line comments", () => {
    const tokens = tokenizeCode("// hi", "js");
    expect(tokens[0]).toEqual({ text: "// hi", className: "syntax-comment" });
  });

  it("highlights YAML keys as properties", () => {
    const tokens = tokenizeCode("key: value", "yaml");
    expect(classOf(tokens, "key")).toBe("syntax-property");
  });

  it("highlights HTML tags as keywords", () => {
    const tokens = tokenizeCode("<div>x</div>", "html");
    expect(classOf(tokens, "<div>")).toBe("syntax-keyword");
  });

  it("highlights markdown headings", () => {
    expect(tokenizeCode("# Title", "md")).toEqual([{ text: "# Title", className: "syntax-keyword" }]);
  });
});
