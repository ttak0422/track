//! Bounded, display-only lexical hints. Never parses Markdown or performs I/O.
use std::{ops::Range, rc::Rc};

pub const MAX_BODY_BYTES: usize = 64 * 1024;
pub const MAX_TOKENS: usize = 8192;
pub const MAX_LANGUAGE_BYTES: usize = 128;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Role {
    Plain,
    Comment,
    Keyword,
    String,
    Number,
    Function,
    Property,
}
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Token {
    pub range: Range<usize>,
    pub role: Role,
}

pub fn normalize(language: &str) -> String {
    let word = language.split_whitespace().next().unwrap_or("");
    word.strip_prefix("language-")
        .unwrap_or(word)
        .to_ascii_lowercase()
}

/// Immutable block-local cache: constructed only at ingestion/change, then shared
/// by reader projections. No source copy or scan on theme, Find or pointer changes.
/// Empty ranges mean lossless plain display, including unsupported/oversize input.
pub fn classify(body: &str, language: Option<&str>) -> Rc<[Token]> {
    if body.len() > MAX_BODY_BYTES || language.is_some_and(|value| value.len() > MAX_LANGUAGE_BYTES)
    {
        return Rc::from([]);
    }
    let language = normalize(language.unwrap_or(""));
    let js = matches!(
        language.as_str(),
        "js" | "javascript" | "jsx" | "ts" | "typescript" | "tsx"
    );
    let lua = language == "lua";
    let json = language == "json";
    let python = matches!(language.as_str(), "python" | "py" | "python3");
    if !(js || lua || json || python) {
        return Rc::from([]);
    }
    let bytes = body.as_bytes();
    let mut tokens: Vec<Token> = Vec::new();
    let mut i = 0;
    while i < bytes.len() {
        let start = i;
        let tail = &body[i..];
        let role;
        if (js && tail.starts_with("//"))
            || (lua && tail.starts_with("--"))
            || (python && tail.starts_with('#'))
        {
            i += if python {
                tail.find(['\r', '\n'])
            } else {
                tail.find('\n')
            }
            .unwrap_or(tail.len());
            role = Role::Comment;
        } else if js && tail.starts_with("/*") {
            i += tail[2..].find("*/").map_or(tail.len(), |n| n + 4);
            role = Role::Comment;
        } else if bytes[i] == b'"' || (!json && (bytes[i] == b'\'' || (js && bytes[i] == b'`'))) {
            let quote = bytes[i];
            let triple = python && bytes[i..].starts_with(&[quote, quote, quote]);
            i += if triple { 3 } else { 1 };
            while i < bytes.len() {
                if bytes[i] == b'\\' {
                    i += 1;
                    if python && bytes[i..].starts_with(b"\r\n") {
                        i += 2;
                    } else if i < bytes.len() {
                        i += body[i..].chars().next().unwrap().len_utf8();
                    }
                } else if triple && bytes[i..].starts_with(&[quote, quote, quote]) {
                    i += 3;
                    break;
                } else if bytes[i] == quote && !triple {
                    i += 1;
                    break;
                } else if python && !triple && matches!(bytes[i], b'\n' | b'\r') {
                    break;
                } else {
                    i += body[i..].chars().next().unwrap().len_utf8();
                }
            }
            role = if json && body[i..].trim_start().starts_with(':') {
                Role::Property
            } else {
                Role::String
            };
        } else if bytes[i].is_ascii_digit() {
            i += 1;
            while i < bytes.len()
                && (bytes[i].is_ascii_alphanumeric() || matches!(bytes[i], b'.' | b'_'))
            {
                i += 1;
            }
            role = Role::Number;
        } else if bytes[i].is_ascii_alphabetic() || matches!(bytes[i], b'_' | b'$') {
            i += 1;
            while i < bytes.len()
                && (bytes[i].is_ascii_alphanumeric() || matches!(bytes[i], b'_' | b'$'))
            {
                i += 1;
            }
            let word = &body[start..i];
            let keyword = if json {
                matches!(word, "true" | "false" | "null")
            } else if python {
                matches!(
                    word,
                    "False"
                        | "None"
                        | "True"
                        | "and"
                        | "as"
                        | "assert"
                        | "async"
                        | "await"
                        | "break"
                        | "class"
                        | "continue"
                        | "def"
                        | "del"
                        | "elif"
                        | "else"
                        | "except"
                        | "finally"
                        | "for"
                        | "from"
                        | "global"
                        | "if"
                        | "import"
                        | "in"
                        | "is"
                        | "lambda"
                        | "nonlocal"
                        | "not"
                        | "or"
                        | "pass"
                        | "raise"
                        | "return"
                        | "try"
                        | "while"
                        | "with"
                        | "yield"
                )
            } else if lua {
                matches!(
                    word,
                    "and"
                        | "break"
                        | "do"
                        | "else"
                        | "elseif"
                        | "end"
                        | "false"
                        | "for"
                        | "function"
                        | "goto"
                        | "if"
                        | "in"
                        | "local"
                        | "nil"
                        | "not"
                        | "or"
                        | "repeat"
                        | "return"
                        | "then"
                        | "true"
                        | "until"
                        | "while"
                )
            } else {
                matches!(
                    word,
                    "async"
                        | "await"
                        | "break"
                        | "case"
                        | "catch"
                        | "class"
                        | "const"
                        | "continue"
                        | "debugger"
                        | "default"
                        | "delete"
                        | "do"
                        | "else"
                        | "export"
                        | "extends"
                        | "false"
                        | "finally"
                        | "for"
                        | "from"
                        | "function"
                        | "if"
                        | "import"
                        | "in"
                        | "instanceof"
                        | "interface"
                        | "let"
                        | "new"
                        | "null"
                        | "of"
                        | "return"
                        | "static"
                        | "super"
                        | "switch"
                        | "this"
                        | "throw"
                        | "true"
                        | "try"
                        | "type"
                        | "typeof"
                        | "undefined"
                        | "var"
                        | "void"
                        | "while"
                        | "yield"
                )
            };
            role = if keyword {
                Role::Keyword
            } else if !json && body[i..].trim_start().starts_with('(') {
                Role::Function
            } else {
                Role::Plain
            };
        } else {
            i += tail.chars().next().unwrap().len_utf8();
            role = Role::Plain;
        }
        if let Some(last) = tokens.last_mut().filter(|t| t.role == role) {
            last.range.end = i;
        } else {
            if tokens.len() == MAX_TOKENS {
                return Rc::from([]);
            }
            tokens.push(Token {
                range: start..i,
                role,
            });
        }
    }
    tokens.into()
}

#[cfg(test)]
mod tests {
    use super::*;
    fn role(body: &str, lang: &str, word: &str) -> Role {
        let start = body.find(word).unwrap();
        classify(body, Some(lang))
            .iter()
            .find(|t| t.range.contains(&start))
            .unwrap()
            .role
    }
    #[test]
    fn examples_aliases_and_shielding() {
        for lang in [
            "js",
            "javascript",
            "jsx",
            "ts",
            "typescript",
            "tsx",
            " language-TS extra ",
        ] {
            let body = "const answer = compute(42); 'const 42' /* compute(42) */ // const";
            assert_eq!(role(body, lang, "const"), Role::Keyword);
            assert_eq!(role(body, lang, "compute"), Role::Function);
            assert_eq!(role(body, lang, "42"), Role::Number);
            assert_eq!(role(body, lang, "'const"), Role::String);
            assert_eq!(role(body, lang, "/*"), Role::Comment);
        }
        let lua = "local greeting = \"こんにちは\" -- note 42";
        assert_eq!(role(lua, "lua", "local"), Role::Keyword);
        assert_eq!(role(lua, "lua", "こんにちは"), Role::String);
        assert_eq!(role(lua, "lua", "42"), Role::Comment);
        let json = "{\"ready\":true,\"count\":2,\"label\":\"🦀\"}";
        assert_eq!(role(json, "json", "ready"), Role::Property);
        assert_eq!(role(json, "json", "true"), Role::Keyword);
        assert_eq!(role(json, "json", "2"), Role::Number);
        assert_eq!(role(json, "json", "🦀"), Role::String);
    }
    #[test]
    fn python_aliases_comments_strings_and_keywords() {
        for lang in ["python", "py", "python3", "language-Python linenums"] {
            let body = "async def greet(name):\n  return True # False 42\n";
            assert_eq!(role(body, lang, "async"), Role::Keyword);
            assert_eq!(role(body, lang, "def"), Role::Keyword);
            assert_eq!(role(body, lang, "greet"), Role::Function);
            assert_eq!(role(body, lang, "True"), Role::Keyword);
            assert_eq!(role(body, lang, "False"), Role::Comment);
            assert_eq!(role(body, lang, "42"), Role::Comment);
        }
        // Expressions inside strings remain opaque; soft keywords stay plain.
        let body = "f'hello {True}'\nmatch = None\ncase = 42\ntype = 1";
        assert_eq!(role(body, "py", "True"), Role::String);
        assert_eq!(role(body, "py", "match"), Role::Plain);
        assert_eq!(role(body, "py", "case"), Role::Plain);
        assert_eq!(role(body, "py", "type"), Role::Plain);
        assert_eq!(role(body, "py", "None"), Role::Keyword);
        assert_eq!(role(body, "py", "42"), Role::Number);
    }

    #[test]
    fn python_triple_quotes_and_unterminated_lines() {
        for quote in ["\"\"\"", "'''"] {
            let body = format!("{quote}日本語\n# return 'single' \"double\"{quote}\nreturn 7");
            assert_eq!(role(&body, "python", "#"), Role::String);
            assert_eq!(role(&body, "python", "日本語"), Role::String);
            let end = body.rfind("return").unwrap();
            assert_eq!(
                classify(&body, Some("python"))
                    .iter()
                    .find(|t| t.range.contains(&end))
                    .unwrap()
                    .role,
                Role::Keyword
            );
        }
        assert_eq!(role("'unfinished\nreturn 2", "py", "return"), Role::Keyword);
        assert_eq!(
            role("\"\"\"unfinished\nreturn 2", "py", "return"),
            Role::String
        );
        assert_eq!(role("'escaped\\\nreturn 2'", "py", "return"), Role::String);
        assert_eq!(
            role("'escaped\\\r\nreturn 2'", "py", "return"),
            Role::String
        );
        assert_eq!(role("# comment\rreturn 2", "py", "return"), Role::Keyword);
    }

    #[test]
    fn ranges_lossless_and_bounded() {
        for body in [
            "",
            "\r\n\tconst 日本語🦀e\u{301} = 'escaped\\\"42';\n",
            "'unterminated🦀\\",
            "/* unterminated\n42",
            "-- 42\nlocal x=1\n",
        ] {
            for lang in ["js", "lua", "json", "python", "py", "python3"] {
                let tokens = classify(body, Some(lang));
                let mut end = 0;
                for token in tokens.iter() {
                    assert_eq!(token.range.start, end);
                    assert!(body.get(token.range.clone()).is_some());
                    end = token.range.end;
                }
                assert_eq!(end, body.len());
                assert_eq!(
                    tokens
                        .iter()
                        .map(|t| &body[t.range.clone()])
                        .collect::<String>(),
                    body
                );
            }
        }
        for lang in [None, Some(""), Some("go"), Some("mermaid")] {
            assert!(classify("const x=1", lang).is_empty());
        }
        assert!(classify("const x=1", Some(&"j".repeat(MAX_LANGUAGE_BYTES + 1))).is_empty());
        for lang in ["js", "python", "py", "python3"] {
            assert!(classify(&"x".repeat(MAX_BODY_BYTES + 1), Some(lang)).is_empty());
            assert!(classify(&"1;".repeat(MAX_TOKENS), Some(lang)).is_empty());
        }
    }
}
