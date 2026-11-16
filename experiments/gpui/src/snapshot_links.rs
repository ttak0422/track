//! Pure memory-only paths and history. No filesystem/URL resolver is involved.
pub(crate) fn resolve(current: &str, target: &str) -> Option<String> {
    if target.is_empty() || target.len() > 4096 || target.trim() != target {
        return None;
    }
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
    let target = String::from_utf8(bytes).ok()?;
    if target.is_empty()
        || target.starts_with('/')
        || target.contains(['\\', ':', '?', '#'])
        || target.chars().any(char::is_control)
    {
        return None;
    }
    let ext = target.rsplit_once('.')?.1;
    if !ext.eq_ignore_ascii_case("md") && !ext.eq_ignore_ascii_case("markdown") {
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
#[derive(Default)]
pub(crate) struct History {
    paths: Vec<String>,
    cursor: usize,
}
impl History {
    pub fn reset(&mut self, path: String) {
        self.paths = vec![path];
        self.cursor = 0;
    }
    pub fn target(&self, forward: bool) -> Option<(usize, &str)> {
        let i = if forward {
            self.cursor.checked_add(1)?
        } else {
            self.cursor.checked_sub(1)?
        };
        Some((i, self.paths.get(i)?.as_str()))
    }
    pub fn accept(&mut self, path: String, target: Option<usize>) {
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
        assert_eq!(h.target(false), Some((1, "b.md")));
        h.accept("b.md".into(), Some(1));
        assert_eq!(h.target(true), Some((2, "c.md")));
        h.accept("d.md".into(), None);
        assert!(h.target(true).is_none());
        h.accept("wrong.md".into(), Some(0));
        assert_eq!(h.paths[h.cursor], "d.md");
        for i in 0..200 {
            h.accept(format!("{i}.md"), None);
        }
        assert_eq!(h.paths.len(), 100);
        assert_eq!(h.cursor, 99);
    }
}
