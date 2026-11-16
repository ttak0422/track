//! Pure memory-only paths and history. No filesystem/URL resolver is involved.
pub(crate) fn resolve(current: &str, target: &str) -> Option<String> {
    resolve_extension(current, target, &["md", "markdown"])
}
pub(crate) fn resolve_extension(
    current: &str,
    target: &str,
    extensions: &[&str],
) -> Option<String> {
    if target.is_empty() || target.len() > 4096 || target.trim() != target {
        return None;
    }
    let target = decode(target)?;
    if target.is_empty()
        || target.starts_with('/')
        || target.contains(['\\', ':', '?', '#'])
        || target.chars().any(char::is_control)
    {
        return None;
    }
    let ext = target.rsplit_once('.')?.1;
    if !extensions
        .iter()
        .any(|allowed| ext.eq_ignore_ascii_case(allowed))
    {
        return None;
    }
    let mut parts: Vec<&str> = current.split('/').collect();
    parts.pop()?;
    if parts
        .iter()
        .any(|p| p.is_empty() || *p == "." || *p == "..")
    {
        return None;
    }
    for part in target.split('/') {
        match part {
            "" => return None,
            "." => {}
            ".." => {
                parts.pop()?;
            }
            _ => parts.push(part),
        }
    }
    let result = parts.join("/");
    (!result.is_empty() && result.len() <= 4096).then_some(result)
}
fn decode(target: &str) -> Option<String> {
    let mut bytes = Vec::with_capacity(target.len());
    let raw = target.as_bytes();
    let mut i = 0;
    while i < raw.len() {
        if raw[i] == b'%' {
            let hex = raw.get(i + 1..i + 3)?;
            let a = (hex[0] as char).to_digit(16)?;
            let b = (hex[1] as char).to_digit(16)?;
            bytes.push((a * 16 + b) as u8);
            i += 3;
        } else {
            bytes.push(raw[i]);
            i += 1;
        }
    }
    String::from_utf8(bytes).ok()
}
#[derive(Clone, Debug, PartialEq, Eq)]
pub(crate) struct Location {
    pub file: String,
    pub heading: Option<String>,
}
impl From<String> for Location {
    fn from(file: String) -> Self {
        Self {
            file,
            heading: None,
        }
    }
}
impl From<&str> for Location {
    fn from(file: &str) -> Self {
        file.to_owned().into()
    }
}
pub(crate) fn location(current: &str, target: &str) -> Option<Location> {
    if target.is_empty() || target.len() > 4096 || target.trim() != target {
        return None;
    }
    let (path, fragment) = target
        .split_once('#')
        .map_or((target, None), |(path, fragment)| (path, Some(fragment)));
    let heading = match fragment {
        None => None,
        Some(fragment) => {
            let decoded = decode(fragment)?;
            if decoded.is_empty()
                || decoded.len() > 512
                || decoded
                    .chars()
                    .any(|c| c.is_control() || matches!(c, '#' | '?' | '\\' | ':'))
            {
                return None;
            }
            Some(decoded)
        }
    };
    let file = if path.is_empty() && heading.is_some() {
        current.to_owned()
    } else {
        resolve(current, path)?
    };
    Some(Location { file, heading })
}
#[derive(Default)]
pub(crate) struct History {
    paths: Vec<Location>,
    cursor: usize,
}
impl History {
    pub fn reset(&mut self, path: String) {
        self.paths = vec![path.into()];
        self.cursor = 0;
    }
    pub fn target(&self, forward: bool) -> Option<(usize, &Location)> {
        let i = if forward {
            self.cursor.checked_add(1)?
        } else {
            self.cursor.checked_sub(1)?
        };
        Some((i, self.paths.get(i)?))
    }
    pub fn accept(&mut self, path: Location, target: Option<usize>) {
        if let Some(i) = target {
            if self.paths.get(i) == Some(&path) {
                self.cursor = i;
            }
            return;
        }
        if self.paths.get(self.cursor) == Some(&path) {
            return;
        }
        self.paths.truncate(self.cursor + 1);
        self.paths.push(path);
        if self.paths.len() > 100 {
            self.paths.remove(0);
        }
        self.cursor = self.paths.len() - 1;
    }
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn relative_and_encoded_paths_never_leave_root() {
        assert_eq!(
            resolve("docs/a.md", "../Guide%20日本語.MD").as_deref(),
            Some("Guide 日本語.MD")
        );
        assert_eq!(
            resolve("a.md", "./docs/b.markdown").as_deref(),
            Some("docs/b.markdown")
        );
        for target in [
            "../x.md",
            "%2e%2e/x.md",
            "/x.md",
            "https://x/a.md",
            "file:a.md",
            "C:\\x.md",
            "a.md#x",
            "a.md?x",
            "a%23b.md",
            "a%3fb.md",
            "%ff.md",
            "bad%xx.md",
            "x.png",
            "a//b.md",
            "x%00.md",
        ] {
            assert!(resolve("a.md", target).is_none(), "{target}");
        }
    }
    #[test]
    fn history_bounded_branching_and_stale_target() {
        let mut h = History::default();
        h.reset("a.md".into());
        h.accept("b.md".into(), None);
        h.accept("c.md".into(), None);
        assert_eq!(h.target(false), Some((1, &Location::from("b.md"))));
        h.accept("b.md".into(), Some(1));
        assert_eq!(h.target(true), Some((2, &Location::from("c.md"))));
        h.accept("d.md".into(), None);
        assert!(h.target(true).is_none());
        h.accept("wrong.md".into(), Some(0));
        assert_eq!(h.paths[h.cursor].file, "d.md");
        for i in 0..200 {
            h.accept(format!("{i}.md").into(), None);
        }
        assert_eq!(h.paths.len(), 100);
        assert_eq!(h.cursor, 99);
    }
}

#[cfg(test)]
mod heading_tests {
    use super::*;
    #[test]
    fn fragments_decode_once_and_history_keeps_sections() {
        let first = location("docs/a.md", "#%E6%97%A5%E6%9C%AC%E8%AA%9E").unwrap();
        assert_eq!(
            first,
            Location {
                file: "docs/a.md".into(),
                heading: Some("日本語".into())
            }
        );
        assert_eq!(
            location("docs/a.md", "../b.md#repeat-2").unwrap().file,
            "b.md"
        );
        assert_eq!(
            location("a.md", "#%2520").unwrap().heading.as_deref(),
            Some("%20")
        );
        for bad in [
            "#",
            "#%ff",
            "#%GG",
            "#x#y",
            "#a%00",
            "../b.md#x",
            "https://x/a.md#x",
            "b.md?x#y",
        ] {
            assert!(location("a.md", bad).is_none(), "{bad}");
        }
        let mut h = History::default();
        h.reset("docs/a.md".into());
        h.accept(first.clone(), None);
        let second = location("docs/a.md", "#other").unwrap();
        h.accept(second.clone(), None);
        assert_eq!(h.target(false), Some((1, &first)));
        h.accept(first.clone(), Some(1));
        assert_eq!(h.target(true), Some((2, &second)));
        h.accept(first, None);
        assert_eq!(h.paths.len(), 3); // repeat jump, no duplicate entry
        h.accept(Location::from("docs/a.md"), None);
        assert!(h.target(true).is_none());
    }
}
