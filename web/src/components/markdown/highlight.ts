// A tiny, dependency-free syntax tokenizer for fenced code blocks. tokenizeCode turns source text into
// classified spans; the React rendering wrapper lives with the CodeBlock component. Pure, so each
// language path is unit-tested.

export interface HighlightToken {
  text: string;
  className?: string;
}

const keywordSets: Record<string, Set<string>> = {
  css: new Set(["important", "from", "to"]),
  go: new Set([
    "break",
    "case",
    "chan",
    "const",
    "continue",
    "default",
    "defer",
    "else",
    "fallthrough",
    "for",
    "func",
    "go",
    "goto",
    "if",
    "import",
    "interface",
    "map",
    "package",
    "range",
    "return",
    "select",
    "struct",
    "switch",
    "type",
    "var",
  ]),
  js: new Set([
    "await",
    "break",
    "case",
    "catch",
    "class",
    "const",
    "continue",
    "default",
    "else",
    "export",
    "extends",
    "finally",
    "for",
    "from",
    "function",
    "if",
    "import",
    "let",
    "new",
    "return",
    "switch",
    "throw",
    "try",
    "typeof",
    "var",
    "while",
  ]),
  lua: new Set([
    "and",
    "break",
    "do",
    "else",
    "elseif",
    "end",
    "false",
    "for",
    "function",
    "if",
    "in",
    "local",
    "nil",
    "not",
    "or",
    "repeat",
    "return",
    "then",
    "true",
    "until",
    "while",
  ]),
  sh: new Set(["case", "do", "done", "elif", "else", "esac", "fi", "for", "function", "if", "in", "then", "while"]),
};

keywordSets.ts = keywordSets.js;
keywordSets.tsx = keywordSets.js;
keywordSets.jsx = keywordSets.js;
keywordSets.javascript = keywordSets.js;
keywordSets.typescript = keywordSets.js;
keywordSets.bash = keywordSets.sh;
keywordSets.shell = keywordSets.sh;
keywordSets.zsh = keywordSets.sh;

export function tokenizeCode(text: string, lang: string): HighlightToken[] {
  const normalized = normalizeCodeLang(lang);
  if (normalized === "") return [{ text }];
  if (normalized === "json") return tokenizeGeneric(text, normalized, jsonKeyword);
  if (normalized === "yaml" || normalized === "yml") return tokenizeYaml(text);
  if (normalized === "html" || normalized === "xml") return tokenizeHtml(text);
  if (normalized === "md" || normalized === "markdown") return tokenizeMarkdownCode(text);
  return tokenizeGeneric(text, normalized, keywordSets[normalized]);
}

export function normalizeCodeLang(lang: string): string {
  const first = lang.trim().split(/\s+/)[0] ?? "";
  return first.replace(/^language-/, "").toLowerCase();
}

function tokenizeGeneric(text: string, lang: string, keywords?: Set<string>): HighlightToken[] {
  const out: HighlightToken[] = [];
  let i = 0;
  while (i < text.length) {
    const rest = text.slice(i);
    const comment = matchComment(rest, lang);
    if (comment) {
      out.push({ text: comment, className: "syntax-comment" });
      i += comment.length;
      continue;
    }
    const string = matchPrefix(rest, /^`(?:\\.|[^`\\])*`|^"(?:\\.|[^"\\])*"|^'(?:\\.|[^'\\])*'/);
    if (string) {
      out.push({ text: string, className: "syntax-string" });
      i += string.length;
      continue;
    }
    const number = matchPrefix(rest, /^\b\d+(?:\.\d+)?\b/);
    if (number) {
      out.push({ text: number, className: "syntax-number" });
      i += number.length;
      continue;
    }
    const word = matchPrefix(rest, /^[A-Za-z_][\w-]*/);
    if (word) {
      if (keywords?.has(word)) {
        out.push({ text: word, className: "syntax-keyword" });
      } else if (/^\s*\(/.test(text.slice(i + word.length))) {
        out.push({ text: word, className: "syntax-function" });
      } else {
        out.push({ text: word });
      }
      i += word.length;
      continue;
    }
    out.push({ text: text[i] });
    i += 1;
  }
  return out;
}

function matchComment(rest: string, lang: string): string {
  if (rest.startsWith("/*")) {
    const end = rest.indexOf("*/", 2);
    return end === -1 ? rest : rest.slice(0, end + 2);
  }
  if (lang === "lua" && rest.startsWith("--")) {
    return rest.slice(0, lineEnd(rest));
  }
  if ((lang === "sh" || lang === "bash" || lang === "shell" || lang === "zsh") && rest.startsWith("#")) {
    return rest.slice(0, lineEnd(rest));
  }
  if (rest.startsWith("//")) {
    return rest.slice(0, lineEnd(rest));
  }
  return "";
}

function lineEnd(text: string): number {
  const next = text.indexOf("\n");
  return next === -1 ? text.length : next;
}

function matchPrefix(text: string, pattern: RegExp): string {
  return pattern.exec(text)?.[0] ?? "";
}

const jsonKeyword = new Set(["false", "null", "true"]);

function tokenizeYaml(text: string): HighlightToken[] {
  return text.split(/(\n)/).flatMap((line) => {
    if (line === "\n") return [{ text: line }];
    const match = /^(\s*)([-\w.]+)(\s*:)/.exec(line);
    if (!match) return tokenizeGeneric(line, "yaml");
    const rest = line.slice(match[0].length);
    return [
      { text: match[1] },
      { text: match[2], className: "syntax-property" },
      { text: match[3] },
      ...tokenizeGeneric(rest, "yaml"),
    ];
  });
}

function tokenizeHtml(text: string): HighlightToken[] {
  const out: HighlightToken[] = [];
  const pattern = /(<!--[\s\S]*?-->|<\/?[A-Za-z][^>]*>)/g;
  let last = 0;
  for (const match of text.matchAll(pattern)) {
    if (match.index > last) out.push({ text: text.slice(last, match.index) });
    out.push({ text: match[0], className: match[0].startsWith("<!--") ? "syntax-comment" : "syntax-keyword" });
    last = match.index + match[0].length;
  }
  if (last < text.length) out.push({ text: text.slice(last) });
  return out;
}

function tokenizeMarkdownCode(text: string): HighlightToken[] {
  return text.split(/(\n)/).flatMap((line) => {
    if (/^\s*#{1,6}\s/.test(line)) return [{ text: line, className: "syntax-keyword" }];
    if (/^\s*[-*]\s/.test(line)) return [{ text: line, className: "syntax-property" }];
    return [{ text: line }];
  });
}
