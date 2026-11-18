//! Ephemeral, bounded external snapshot data. No path here is a filesystem path.
use serde::{
    Deserialize, Deserializer,
    de::{self, SeqAccess, Visitor},
};
use std::{
    collections::{BTreeMap, HashSet},
    fmt,
    sync::Arc,
};
pub const WIRE_LIMIT: usize = 24 * 1024 * 1024;
const TOTAL_LIMIT: usize = 16 * 1024 * 1024;
const MARKDOWN_LIMIT: usize = 2 * 1024 * 1024;
const IMAGE_LIMIT: usize = 10 * 1024 * 1024;
const FILE_LIMIT: usize = 5000;
pub const PAGE_SIZE: usize = 100;

pub struct Capability {
    pub(crate) token: String,
    pub(crate) origin: String,
    pub(crate) launch: u64,
}
impl fmt::Debug for Capability {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("Capability([redacted])")
    }
}
impl Capability {
    pub fn parse(input: &str, origin: &str, launch: u64) -> Result<Self, &'static str> {
        // Do not URL-normalize: the accepted bytes must match the owned origin exactly.
        let port = origin
            .strip_prefix("http://127.0.0.1:")
            .ok_or("No owned loopback origin")?;
        if port
            .parse::<u16>()
            .ok()
            .filter(|p| *p != 0)
            .map(|p| p.to_string())
            .as_deref()
            != Some(port)
        {
            return Err("No owned loopback origin");
        }
        let route = if input.starts_with('/') {
            input
        } else {
            input
                .strip_prefix(origin)
                .ok_or("Handoff must use the owned server origin")?
        };
        let token = route
            .strip_prefix("/markdown#handoff=")
            .ok_or("Invalid handoff route")?;
        if token.len() != 64
            || !token
                .bytes()
                .all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b))
        {
            return Err("Invalid handoff capability");
        }
        Ok(Self {
            token: token.into(),
            origin: origin.into(),
            launch,
        })
    }
}
#[derive(Debug)]
pub struct MarkdownFile {
    pub path: String,
    pub text: String,
}
#[derive(Debug)]
pub struct Snapshot {
    pub active: String,
    pub files: Vec<MarkdownFile>,
    pub images: Arc<BTreeMap<String, Arc<[u8]>>>,
}
impl Snapshot {
    pub fn selected(&self, path: &str) -> Option<&MarkdownFile> {
        self.files.iter().find(|f| f.path == path)
    }
    pub fn markdown_paths(&self) -> Vec<&str> {
        self.files.iter().map(|f| f.path.as_str()).collect()
    }
    pub fn markdown(&self, path: &str) -> Option<&str> {
        self.files
            .iter()
            .find(|f| f.path == path)
            .map(|f| f.text.as_str())
    }
    pub fn page_count(&self) -> usize {
        self.files.len().div_ceil(PAGE_SIZE)
    }
    pub fn page(&self, page: usize) -> &[MarkdownFile] {
        let start = page.saturating_mul(PAGE_SIZE).min(self.files.len());
        &self.files[start..start.saturating_add(PAGE_SIZE).min(self.files.len())]
    }
    pub fn decode(bytes: &[u8]) -> Result<Self, &'static str> {
        if bytes.len() > WIRE_LIMIT {
            return Err("Snapshot response is too large");
        }
        let wire: Wire = serde_json::from_slice(bytes).map_err(|_| "Invalid snapshot JSON")?;
        if wire.files.is_empty() {
            return Err("Snapshot must contain files");
        }
        let mut seen = HashSet::new();
        let mut total = 0usize;
        let mut files = Vec::new();
        let mut images = BTreeMap::new();
        for file in wire.files {
            if !valid_path(&file.path) || !seen.insert(file.path.clone()) {
                return Err("Invalid or duplicate snapshot path");
            }
            let ext = file
                .path
                .rsplit_once('.')
                .map(|(_, e)| e.to_ascii_lowercase())
                .unwrap_or_default();
            let markdown = matches!(ext.as_str(), "md" | "markdown");
            let limit = if markdown {
                MARKDOWN_LIMIT
            } else if matches!(ext.as_str(), "png" | "jpg" | "jpeg" | "gif" | "webp") {
                IMAGE_LIMIT
            } else {
                return Err("Unsupported snapshot file type");
            };
            let raster = matches!(ext.as_str(), "png" | "jpg" | "jpeg");
            let data = decode_base64(&file.data, limit, TOTAL_LIMIT - total, markdown || raster)?;
            total += data.1;
            if markdown {
                files.push(MarkdownFile {
                    path: file.path,
                    text: String::from_utf8(data.0).map_err(|_| "Markdown must be UTF-8")?,
                });
            } else if raster {
                images.insert(file.path, Arc::from(data.0));
            }
        }
        if !files.iter().any(|f| f.path == wire.active) {
            return Err("Active snapshot file must be Markdown");
        }
        Ok(Self {
            active: wire.active,
            files,
            images: Arc::new(images),
        })
    }
}
fn valid_path(path: &str) -> bool {
    !path.is_empty()
        && path.len() <= 4096
        && !path.contains('\\')
        && !path.chars().any(char::is_control)
        && path
            .split('/')
            .all(|p| !p.is_empty() && p != "." && p != "..")
}
#[derive(Deserialize)]
struct Wire {
    active: String,
    #[serde(deserialize_with = "bounded_files")]
    files: Vec<WireFile>,
}
#[derive(Deserialize)]
struct WireFile {
    path: String,
    data: String,
}
fn bounded_files<'de, D: Deserializer<'de>>(d: D) -> Result<Vec<WireFile>, D::Error> {
    struct Files;
    impl<'de> Visitor<'de> for Files {
        type Value = Vec<WireFile>;
        fn expecting(&self, f: &mut fmt::Formatter) -> fmt::Result {
            f.write_str("bounded snapshot files")
        }
        fn visit_seq<A: SeqAccess<'de>>(self, mut seq: A) -> Result<Self::Value, A::Error> {
            if seq.size_hint().is_some_and(|n| n > FILE_LIMIT) {
                return Err(de::Error::custom("too many files"));
            }
            let mut out = Vec::new();
            while out.len() < FILE_LIMIT {
                let Some(file) = seq.next_element()? else {
                    return Ok(out);
                };
                out.push(file);
            }
            // Detect an excess entry without allocating its strings or a larger array.
            if seq.next_element::<de::IgnoredAny>()?.is_some() {
                return Err(de::Error::custom("too many files"));
            }
            Ok(out)
        }
    }
    d.deserialize_seq(Files)
}
// Canonical standard base64, with decoded size checked before allocation. Images
// retain PNG/JPEG encoded bytes, but no image objects are created here.
fn decode_base64(
    s: &str,
    limit: usize,
    remaining: usize,
    keep: bool,
) -> Result<(Vec<u8>, usize), &'static str> {
    let bytes = s.as_bytes();
    if bytes.len() % 4 != 0 || bytes.len() > limit.div_ceil(3) * 4 {
        return Err("Invalid or oversized base64 file");
    }
    let padding = if bytes.ends_with(b"==") {
        2
    } else if bytes.ends_with(b"=") {
        1
    } else {
        0
    };
    let size = bytes.len() / 4 * 3 - padding;
    if size > limit || size > remaining {
        return Err("Snapshot decoded size limit exceeded");
    }
    let mut out = if keep {
        Vec::with_capacity(size)
    } else {
        Vec::new()
    };
    for (i, chunk) in bytes.chunks_exact(4).enumerate() {
        let last = i + 1 == bytes.len() / 4;
        let mut v = [0u8; 4];
        for j in 0..4 {
            v[j] = match chunk[j] {
                b'A'..=b'Z' => chunk[j] - b'A',
                b'a'..=b'z' => chunk[j] - b'a' + 26,
                b'0'..=b'9' => chunk[j] - b'0' + 52,
                b'+' => 62,
                b'/' => 63,
                b'=' if last && j >= 4 - padding => 0,
                _ => return Err("Invalid base64 file"),
            };
        }
        if last && ((padding == 2 && v[1] & 15 != 0) || (padding == 1 && v[2] & 3 != 0)) {
            return Err("Invalid base64 padding");
        }
        if keep {
            out.push(v[0] << 2 | v[1] >> 4);
            if !last || padding < 2 {
                out.push(v[1] << 4 | v[2] >> 2);
            }
            if !last || padding == 0 {
                out.push(v[2] << 6 | v[3]);
            }
        }
    }
    Ok((out, size))
}

#[cfg(test)]
mod tests {
    use super::*;
    const ORIGIN: &str = "http://127.0.0.1:1234";
    fn url() -> String {
        format!("{ORIGIN}/markdown#handoff={}", "a".repeat(64))
    }
    fn wire(active: &str, files: serde_json::Value) -> Vec<u8> {
        serde_json::to_vec(&serde_json::json!({"active":active,"files":files})).unwrap()
    }
    #[test]
    fn capability_exact_owned_bytes_and_redaction() {
        let valid = url();
        assert!(Capability::parse(&valid, ORIGIN, 1).is_ok());
        assert!(Capability::parse(valid.strip_prefix(ORIGIN).unwrap(), ORIGIN, 1).is_ok());
        for bad in [
            valid.replace(":1234", ":12345"),
            valid.replace("127.0.0.1", "localhost"),
            valid.replace("http:", "https:"),
            valid.replace("http://", "http://user@"),
            valid.replace("/markdown#", "/markdown?x#"),
            format!("{valid}#x"),
            format!("{valid}&x=y"),
            valid.replace("handoff=", "handoff=%61"),
            valid.replace('a', "A"),
            format!(" {valid}"),
            valid.replace("/markdown", "/x/../markdown"),
        ] {
            let error = Capability::parse(&bad, ORIGIN, 1).unwrap_err();
            assert!(!error.contains(&"a".repeat(64)));
        }
        assert_eq!(
            format!("{:?}", Capability::parse(&valid, ORIGIN, 1).unwrap()),
            "Capability([redacted])"
        );
    }
    #[test]
    fn unicode_paths_selection_and_inert_asset_validation() {
        let bytes = wire(
            "日本語.md",
            serde_json::json!([{"path":"日本語.md","data":"IyBFeGFtcGxlCg=="},{"path":"a.PNG","data":"AA=="}]),
        );
        let snapshot = Snapshot::decode(&bytes).unwrap();
        assert_eq!(snapshot.markdown("日本語.md"), Some("# Example\n"));
        assert!(snapshot.markdown("a.PNG").is_none());
        assert_eq!(snapshot.images["a.PNG"].as_ref(), &[0]);
        assert!(snapshot.markdown("/tmp/a.md").is_none());
        assert_eq!(snapshot.page_count(), 1);
    }
    #[test]
    fn rejects_paths_types_base64_utf8_and_missing_active() {
        for path in [
            "", "/a.md", "a//b.md", "a/./b.md", "../b.md", "a\\b.md", "a\nb.md", "a.exe",
        ] {
            assert!(
                Snapshot::decode(&wire(
                    path,
                    serde_json::json!([{"path":path,"data":"YQ=="}])
                ))
                .is_err(),
                "{path:?}"
            );
        }
        for data in ["_w==", "/w==", "YQ=", "YR==", "YQ===", "====", "YQ==YQ=="] {
            assert!(
                Snapshot::decode(&wire(
                    "a.md",
                    serde_json::json!([{"path":"a.md","data":data}])
                ))
                .is_err(),
                "{data}"
            );
        }
        for files in [
            serde_json::json!([]),
            serde_json::json!([{"path":"a.md","data":""},{"path":"a.md","data":""}]),
            serde_json::json!([{"path":"b.md","data":""}]),
            serde_json::json!([{"path":"a.md","data":null}]),
        ] {
            assert!(Snapshot::decode(&wire("a.md", files)).is_err());
        }
        assert!(Snapshot::decode(br#"{"active":null,"files":[]}"#).is_err());
        assert!(Snapshot::decode(br#"{"active":"a.md","files":null}"#).is_err());
    }
    #[test]
    fn paging_reaches_every_file_without_lookups() {
        for count in [1, 100, 101, 5000, 5001] {
            let files: Vec<_> = (0..count)
                .map(|n| serde_json::json!({"path":format!("{n}.md"),"data":""}))
                .collect();
            let parsed = Snapshot::decode(&wire("0.md", serde_json::json!(files)));
            if count > FILE_LIMIT {
                assert!(parsed.is_err());
                continue;
            }
            let s = parsed.unwrap();
            assert_eq!(
                (0..s.page_count()).map(|p| s.page(p).len()).sum::<usize>(),
                count
            );
            assert!(s.page(usize::MAX).is_empty());
        }
    }
    #[test]
    fn bounds_before_decoding_and_total_bytes() {
        assert!(Snapshot::decode(&vec![b' '; WIRE_LIMIT + 1]).is_err());
        assert!(
            decode_base64(
                &"A".repeat((MARKDOWN_LIMIT.div_ceil(3) + 1) * 4),
                MARKDOWN_LIMIT,
                TOTAL_LIMIT,
                true
            )
            .is_err()
        );
        assert!(decode_base64("AAAA", IMAGE_LIMIT, 2, false).is_err());
        assert!(
            decode_base64("AA==", IMAGE_LIMIT, 1, false)
                .unwrap()
                .0
                .is_empty()
        );
        let path = format!("{}.md", "x".repeat(4096));
        assert!(!valid_path(&path));
        let chunk = "A".repeat((2 * 1024 * 1024 / 3) * 4);
        let files: Vec<_> = (0..9)
            .map(|n| serde_json::json!({"path":format!("{n}.md"),"data":chunk}))
            .collect();
        assert!(Snapshot::decode(&wire("0.md", serde_json::json!(files))).is_err());
    }
}
