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
    if matches!(language.as_str(), "bash" | "sh" | "shell") {
        return shell_tokens(body);
    }
    if matches!(language.as_str(), "rust" | "rs" | "nix") {
        return rust_nix_tokens(body, language != "nix");
    }
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

/// Deliberately lexical, not a compiler. Ambiguous Nix indented strings,
/// interpolation and paths leave the remaining source plain. No recursive parser.
fn rust_nix_tokens(body: &str, rust: bool) -> Rc<[Token]> {
    let bytes = body.as_bytes();
    let mut tokens: Vec<Token> = Vec::new();
    let mut i = 0;
    while i < bytes.len() {
        let start = i;
        let tail = &body[i..];
        let mut role = Role::Plain;
        let raw_end = if rust { raw_string_end(tail) } else { None };
        if (rust && tail.starts_with("//")) || (!rust && tail.starts_with('#')) {
            i += tail.find(['\r', '\n']).unwrap_or(tail.len());
            role = Role::Comment;
        } else if tail.starts_with("/*") {
            i += 2;
            let mut depth = 1usize;
            while i < bytes.len() {
                if rust && bytes[i..].starts_with(b"/*") {
                    depth += 1;
                    i += 2;
                } else if bytes[i..].starts_with(b"*/") {
                    depth -= 1;
                    i += 2;
                    if depth == 0 {
                        break;
                    }
                } else {
                    i += body[i..].chars().next().unwrap().len_utf8();
                }
            }
            role = Role::Comment;
        } else if !rust && tail.starts_with("''") {
            let (end, complete) = nix_indented_end(tail);
            i += end;
            if complete {
                role = Role::String;
            }
        } else if !rust
            && (tail.starts_with("${")
                || tail.starts_with("./")
                || tail.starts_with("../")
                || matches!(bytes[i], b'/' | b'~' | b'<'))
        {
            i = bytes.len();
        } else if raw_end.is_some() {
            i += raw_end.unwrap();
            role = Role::String;
        } else if bytes[i] == b'"' {
            i += 1;
            role = Role::String;
            while i < bytes.len() {
                if !rust && bytes[i..].starts_with(b"${") {
                    // Inner expression quotes cannot safely delimit the outer string.
                    role = Role::Plain;
                    i = bytes.len();
                    break;
                } else if bytes[i] == b'\\' {
                    i += 1;
                    if i < bytes.len() {
                        i += body[i..].chars().next().unwrap().len_utf8();
                    }
                } else if bytes[i] == b'"' {
                    i += 1;
                    break;
                } else {
                    i += body[i..].chars().next().unwrap().len_utf8();
                }
            }
        } else if rust && bytes[i] == b'\'' {
            if let Some(end) = char_end(tail) {
                i += end;
                role = Role::String;
            } else {
                i += 1;
                // Lifetime/label identifiers stay plain, including non-ASCII names.
                while i < bytes.len() {
                    let c = body[i..].chars().next().unwrap();
                    if !(c.is_alphanumeric() || c == '_') {
                        break;
                    }
                    i += c.len_utf8();
                }
            }
        } else if bytes[i].is_ascii_digit() {
            i += 1;
            while i < bytes.len() && (bytes[i].is_ascii_alphanumeric() || bytes[i] == b'_') {
                i += 1;
            }
            role = Role::Number;
        } else if tail.chars().next().unwrap().is_alphabetic() || bytes[i] == b'_' {
            i += tail.chars().next().unwrap().len_utf8();
            while i < bytes.len() {
                let c = body[i..].chars().next().unwrap();
                if !(c.is_alphanumeric() || c == '_' || (!rust && matches!(c, '-' | '\''))) {
                    break;
                }
                i += c.len_utf8();
            }
            // Raw Rust identifiers are identifiers even when their word is a keyword.
            if rust && &body[start..i] == "r" && bytes.get(i) == Some(&b'#') {
                i += 1;
                while i < bytes.len() {
                    let c = body[i..].chars().next().unwrap();
                    if !(c.is_alphanumeric() || c == '_') {
                        break;
                    }
                    i += c.len_utf8();
                }
            } else if !rust
                && bytes.get(i) == Some(&b':')
                && bytes.get(i + 1).is_some_and(|b| !b.is_ascii_whitespace())
            {
                // URI-like input is not a sequence of Nix keywords/comments.
                i = bytes.len();
            } else if if rust {
                matches!(
                    &body[start..i],
                    "as" | "async"
                        | "await"
                        | "break"
                        | "const"
                        | "continue"
                        | "crate"
                        | "dyn"
                        | "else"
                        | "enum"
                        | "extern"
                        | "false"
                        | "fn"
                        | "for"
                        | "if"
                        | "impl"
                        | "in"
                        | "let"
                        | "loop"
                        | "match"
                        | "mod"
                        | "move"
                        | "mut"
                        | "pub"
                        | "ref"
                        | "return"
                        | "self"
                        | "Self"
                        | "static"
                        | "struct"
                        | "super"
                        | "trait"
                        | "true"
                        | "type"
                        | "unsafe"
                        | "use"
                        | "where"
                        | "while"
                )
            } else {
                matches!(
                    &body[start..i],
                    "assert"
                        | "else"
                        | "if"
                        | "in"
                        | "inherit"
                        | "let"
                        | "or"
                        | "rec"
                        | "then"
                        | "with"
                        | "true"
                        | "false"
                        | "null"
                )
            } {
                role = Role::Keyword;
            }
        } else {
            i += tail.chars().next().unwrap().len_utf8();
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

/// No interpolation evaluation: an interpolation or unfinished escape makes
/// this string and the remainder plain. Known indented escapes remain opaque.
fn nix_indented_end(tail: &str) -> (usize, bool) {
    let bytes = tail.as_bytes();
    let mut i = 2;
    while i < bytes.len() {
        if bytes[i..].starts_with(b"${") {
            return (bytes.len(), false);
        }
        if bytes[i..].starts_with(b"''") {
            match bytes.get(i + 2) {
                Some(b'\'' | b'$') => {
                    i += 3;
                    continue;
                }
                Some(b'\\') => {
                    i += 3;
                    if i == bytes.len() {
                        return (bytes.len(), false);
                    }
                    i += tail[i..].chars().next().unwrap().len_utf8();
                    continue;
                }
                _ => return (i + 2, true),
            }
        }
        i += tail[i..].chars().next().unwrap().len_utf8();
    }
    (bytes.len(), false)
}

/// Return the complete raw token, or its unfinished remainder. Hash count is
/// bounded by the Rust grammar; unsupported >255 hashes are left plain by caller.
fn raw_string_end(tail: &str) -> Option<usize> {
    let prefix = if tail.starts_with("br") || tail.starts_with("cr") {
        2
    } else if tail.starts_with('r') {
        1
    } else {
        return None;
    };
    let hashes = tail.as_bytes()[prefix..]
        .iter()
        .take_while(|&&b| b == b'#')
        .count();
    let quote = prefix + hashes;
    if hashes > 255 || tail.as_bytes().get(quote) != Some(&b'"') {
        return None;
    }
    let bytes = tail.as_bytes();
    let mut i = quote + 1;
    while i < bytes.len() {
        if bytes[i] == b'"'
            && bytes
                .get(i + 1..i + 1 + hashes)
                .is_some_and(|s| s.iter().all(|&b| b == b'#'))
        {
            return Some(i + 1 + hashes);
        }
        i += 1;
    }
    Some(tail.len())
}

/// Only a closed single scalar or known escape is a character hint. Lifetimes
/// are never scanned up to an unrelated apostrophe later in the document.
fn char_end(tail: &str) -> Option<usize> {
    let bytes = tail.as_bytes();
    let mut i = 1;
    if bytes.get(i) == Some(&b'\\') {
        i += 1;
        match *bytes.get(i)? {
            b'n' | b'r' | b't' | b'0' | b'\\' | b'\'' | b'"' => i += 1,
            b'x' => {
                if !bytes.get(i + 1..i + 3)?.iter().all(u8::is_ascii_hexdigit) {
                    return None;
                }
                i += 3;
            }
            b'u' if bytes.get(i + 1) == Some(&b'{') => {
                i += 2;
                let start = i;
                while bytes
                    .get(i)
                    .is_some_and(|b| b.is_ascii_hexdigit() || *b == b'_')
                {
                    i += 1;
                }
                if i == start || i - start > 12 || bytes.get(i) != Some(&b'}') {
                    return None;
                }
                i += 1;
            }
            _ => return None,
        }
    } else {
        let c = tail.get(i..)?.chars().next()?;
        if matches!(c, '\'' | '\n' | '\r' | '\t') {
            return None;
        }
        i += c.len_utf8();
    }
    (bytes.get(i) == Some(&b'\'')).then_some(i + 1)
}

/// Shell lexical hints, not an interpreter: keyword positions and expansions are
/// not parsed. Unquoted <<, ${, $( and backticks leave the remaining source plain. Zsh is deliberately
/// not an alias: interactive comments and its additional syntax differ.
fn shell_tokens(body: &str) -> Rc<[Token]> {
    let bytes = body.as_bytes();
    let mut tokens: Vec<Token> = Vec::new();
    let mut i = 0;
    let mut boundary = true;
    while i < bytes.len() {
        let start = i;
        let mut role = Role::Plain;
        if bytes[i..].starts_with(b"<<")
            || bytes[i..].starts_with(b"${")
            || bytes[i..].starts_with(b"$(")
            || bytes[i] == b'`'
        {
            // Do not mistake here-document or expansion contents for shell source.
            i = bytes.len();
        } else if boundary && bytes[i] == b'#' {
            i += body[i..].find(['\r', '\n']).unwrap_or(bytes.len() - i);
            role = Role::Comment;
        } else if matches!(bytes[i], b'\'' | b'"') {
            let quote = bytes[i];
            i += 1;
            while i < bytes.len() {
                if bytes[i] == quote {
                    i += 1;
                    break;
                }
                // In single quotes even a backslash is literal. Double quotes
                // only escape dollar, backtick, quote, backslash and newline.
                if quote == b'"'
                    && bytes[i] == b'\\'
                    && i + 1 < bytes.len()
                    && matches!(bytes[i + 1], b'$' | b'`' | b'"' | b'\\' | b'\n' | b'\r')
                {
                    i += 2;
                    if bytes[i - 1] == b'\r' && bytes.get(i) == Some(&b'\n') {
                        i += 1;
                    }
                } else {
                    i += body[i..].chars().next().unwrap().len_utf8();
                }
            }
            boundary = false;
            role = Role::String;
        } else if bytes[i] == b'\\' {
            i += 1;
            let continuation = matches!(bytes.get(i), Some(b'\n' | b'\r'));
            if i < bytes.len() {
                i += body[i..].chars().next().unwrap().len_utf8();
                if bytes[i - 1] == b'\r' && bytes.get(i) == Some(&b'\n') {
                    i += 1;
                }
            }
            if !continuation {
                boundary = false;
            }
        } else if shell_separator(bytes[i]) {
            i += 1;
            boundary = true;
        } else {
            i += body[i..].chars().next().unwrap().len_utf8();
            while i < bytes.len()
                && !shell_separator(bytes[i])
                && !matches!(bytes[i], b'\'' | b'"' | b'\\' | b'`')
                && !bytes[i..].starts_with(b"${")
                && !bytes[i..].starts_with(b"$(")
            {
                i += body[i..].chars().next().unwrap().len_utf8();
            }
            // Only complete unquoted words receive keyword hints; concatenated
            // quoted/escaped pieces remain ordinary words.
            if boundary
                && (i == bytes.len() || shell_separator(bytes[i]))
                && matches!(
                    &body[start..i],
                    "if" | "then"
                        | "else"
                        | "elif"
                        | "fi"
                        | "for"
                        | "while"
                        | "until"
                        | "do"
                        | "done"
                        | "case"
                        | "esac"
                        | "in"
                        | "function"
                        | "select"
                        | "time"
                        | "coproc"
                        | "!"
                        | "{"
                        | "}"
                )
            {
                role = Role::Keyword;
            }
            boundary = false;
        }
        if let Some(last) = tokens.last_mut().filter(|token| token.role == role) {
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

fn shell_separator(byte: u8) -> bool {
    matches!(
        byte,
        b' ' | b'\t' | b'\r' | b'\n' | b';' | b'|' | b'&' | b'(' | b')' | b'<' | b'>'
    )
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
    fn rust_literals_lifetimes_and_nested_comments() {
        for lang in ["rust", "rs", "language-Rust linenums"] {
            let body =
                "fn f<'a>(x: &'a str) { let c = '🦀'; /* outer /* inner */ fn hidden */ return; }";
            assert_eq!(role(body, lang, "fn"), Role::Keyword);
            assert_eq!(role(body, lang, "'a"), Role::Plain);
            assert_eq!(role(body, lang, "🦀"), Role::String);
            assert_eq!(role(body, lang, "hidden"), Role::Comment);
            assert_eq!(role(body, lang, "return"), Role::Keyword);
            for literal in [
                r##"r#"fn \"raw\""#"##,
                r###"br##"/* fn */"##"###,
                r##"cr"fn""##,
                r"'\n'",
                r"'\u{1f980}'",
                r"'\x41'",
            ] {
                assert!(
                    classify(literal, Some(lang))
                        .iter()
                        .all(|t| t.role == Role::String),
                    "{literal}"
                );
            }
            assert_eq!(role("r#type r#fn let", lang, "type"), Role::Plain);
            assert_eq!(role("r#type r#fn let", lang, "let"), Role::Keyword);
            assert_eq!(role("r##\"unfinished fn", lang, "fn"), Role::String);
            assert_eq!(role("/* /* unfinished */ fn", lang, "fn"), Role::Comment);
            assert_eq!(role("élet", lang, "let"), Role::Plain);
        }
    }

    #[test]
    fn nix_strings_escapes_and_conservative_remainder() {
        let body = "let x = ''日本語 # opaque ''' ''${literal} ''\\n text''; in true";
        assert_eq!(role(body, "nix", "let"), Role::Keyword);
        assert_eq!(role(body, "nix", "opaque"), Role::String);
        assert_eq!(role(body, "nix", "literal"), Role::String);
        assert_eq!(role(body, "nix", "in true"), Role::Keyword);
        assert_eq!(role(r#""escaped \${if}" then"#, "nix", "if"), Role::String);
        assert_eq!(
            role(r#""escaped \${if}" then"#, "nix", "then"),
            Role::Keyword
        );
        for body in [
            "''x ${if true then \"nested\" else \"x\"}'' in",
            "''unfinished in",
            "\"x ${let x = \"inner\";}\" in",
            "./foo/if # tail",
            "https://host/if # tail",
        ] {
            let t = classify(body, Some("nix"));
            assert!(t.iter().all(|t| t.role == Role::Plain), "{body}: {t:?}");
        }
        assert_eq!(
            role(
                "let-name = 1; inherit x; # comment\r\nin x",
                "nix",
                "let-name"
            ),
            Role::Plain
        );
        assert_eq!(role("/* first */ let", "nix", "let"), Role::Keyword);
    }

    #[test]
    fn rust_nix_lossless_limits_and_all_unicode_prefixes() {
        for lang in ["rust", "rs", "nix"] {
            for body in [
                "fn 日本語<'a>() { '🦀'; r##\"a\"#b\"##; }",
                "let x = ''a ''' b ''${c}''; in x",
                "/* nested /* x */ y */\r\n",
                "\"é\\🦀\" 'x' 42",
            ] {
                for end in body
                    .char_indices()
                    .map(|(i, _)| i)
                    .chain(std::iter::once(body.len()))
                {
                    let source = &body[..end];
                    let tokens = classify(source, Some(lang));
                    let mut offset = 0;
                    for t in tokens.iter() {
                        assert_eq!(t.range.start, offset);
                        assert!(source.get(t.range.clone()).is_some());
                        offset = t.range.end;
                    }
                    assert_eq!(offset, source.len());
                    assert_eq!(
                        tokens
                            .iter()
                            .map(|t| &source[t.range.clone()])
                            .collect::<String>(),
                        source
                    );
                }
            }
            assert!(classify(&"x".repeat(MAX_BODY_BYTES + 1), Some(lang)).is_empty());
            assert!(classify(&"let;".repeat(MAX_TOKENS), Some(lang)).is_empty());
        }
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
    fn shell_words_comments_quotes_and_escapes() {
        for lang in ["bash", "sh", "shell", "language-BASH linenums"] {
            let body = "if true; then echo foo#bar # 日本語\r\nfi";
            assert_eq!(role(body, lang, "if"), Role::Keyword);
            assert_eq!(role(body, lang, "then"), Role::Keyword);
            assert_eq!(role(body, lang, "foo#bar"), Role::Plain);
            assert_eq!(role(body, lang, "日本語"), Role::Comment);
            assert_eq!(role(body, lang, "fi"), Role::Keyword);
            assert_eq!(role("echo \\#literal", lang, "#literal"), Role::Plain);
            assert_eq!(role("echo 'a'123#literal", lang, "#literal"), Role::Plain);
            assert_eq!(role("echo foo\\\n#literal", lang, "#literal"), Role::Plain);
            assert_eq!(role("echo \\\r\n#comment", lang, "#comment"), Role::Comment);
            assert_eq!(
                role("'literal\\' #comment", lang, "#comment"),
                Role::Comment
            );
            assert_eq!(
                role(r#""escaped\" #string" #comment"#, lang, "#string"),
                Role::String
            );
            assert_eq!(
                role(r#""escaped\" #string" #comment"#, lang, "#comment"),
                Role::Comment
            );
            assert_eq!(role("if'x' then_suffix", lang, "if"), Role::Plain);
        }
    }

    #[test]
    fn shell_unsupported_constructs_are_plain_not_interpreted() {
        for body in [
            "cat <<EOF\nif # data\nEOF",
            "cat <<<word # tail",
            "echo $((1 << 2)) # tail",
            "echo ${x#prefix} # tail",
            "echo before$(echo x) # tail",
            "echo `echo x` # tail",
        ] {
            assert_eq!(role(body, "bash", "#"), Role::Plain);
        }
        assert_eq!(
            role(r#"echo "${value} $(command) # opaque""#, "bash", "#"),
            Role::String
        );
        assert!(classify("if true; then x; fi", Some("zsh")).is_empty());
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
            for lang in [
                "js", "lua", "json", "python", "py", "python3", "bash", "sh", "shell",
            ] {
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
        for lang in ["bash", "sh", "shell"] {
            assert!(classify(&"x".repeat(MAX_BODY_BYTES + 1), Some(lang)).is_empty());
            assert!(classify(&"if;".repeat(MAX_TOKENS), Some(lang)).is_empty());
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
