//! Inactive bounded request encoding, not launch authority or a command builder.
//! The caller supplies the final owned-child context. Encoding does not inspect
//! the environment, verify a parent, resolve a vault or start a process.
use super::launch_context::ResolutionContextRequest;
use super::workspace_provenance::RequestedSelector;
use std::fmt;

const LIMIT: usize = 256 * 1024;
const INVALID: &str = "Invalid bound launch protocol request";

pub(super) struct BoundLaunchRequest {
    context: ResolutionContextRequest,
    selector: RequestedSelector,
    private_root: String,
    nonce: String,
}
impl fmt::Debug for BoundLaunchRequest {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("BoundLaunchRequest { values: [redacted] }")
    }
}
fn hex(value: &str) -> bool {
    value.len().is_multiple_of(2)
        && value
            .bytes()
            .all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b))
}
fn bounded(value: &str, max: usize) -> bool {
    !value.is_empty() && value.len() <= max && !value.chars().any(char::is_control)
}
fn private_path(value: &str) -> bool {
    bounded(value, 4096)
        && value.starts_with('/')
        && (value == "/"
            || !value[1..]
                .split('/')
                .any(|p| p.is_empty() || p == "." || p == ".."))
}
impl BoundLaunchRequest {
    pub(super) fn new(
        context: ResolutionContextRequest,
        selector: RequestedSelector,
        private_root: String,
        nonce: String,
    ) -> Result<Self, &'static str> {
        let selected = match &selector {
            RequestedSelector::Registered(name) => {
                bounded(name, 256)
                    && name
                        .bytes()
                        .all(|b| b.is_ascii_lowercase() || b.is_ascii_digit() || b == b'-')
            }
            RequestedSelector::Path(path) => bounded(path, 4096),
        };
        let value = |prefix: &str| {
            context
                .environment()
                .iter()
                .find_map(|e| e.strip_prefix(prefix))
        };
        let parent = value("TRACK_WEB_DESKTOP_PARENT_PID=").ok_or(INVALID)?;
        let token = value("TRACK_WEB_DESKTOP_READY_TOKEN=").ok_or(INVALID)?;
        if !selected
            || !private_path(&private_root)
            || !(32..=128).contains(&nonce.len())
            || !hex(&nonce)
            || !parent
                .parse::<u32>()
                .is_ok_and(|pid| pid > 1 && pid.to_string() == parent)
            || token.len() != 64
            || !hex(token)
        {
            return Err(INVALID);
        }
        // Shape alone is not proof that this PID is the actual parent or that
        // the caller owns the token. The Go boundary and lifecycle must check it.
        Ok(Self {
            context,
            selector,
            private_root,
            nonce,
        })
    }
    pub(super) fn preflight(&self) -> Result<Vec<u8>, &'static str> {
        self.encode("preflight", "")
    }
    pub(super) fn serve(&self, revision: &str) -> Result<Vec<u8>, &'static str> {
        if revision.len() != 64 || !hex(revision) {
            return Err(INVALID);
        }
        self.encode("serve", revision)
    }
    fn encode(&self, mode: &str, revision: &str) -> Result<Vec<u8>, &'static str> {
        let (name, path) = match &self.selector {
            RequestedSelector::Registered(name) => (name.as_str(), ""),
            RequestedSelector::Path(path) => ("", path.as_str()),
        };
        let value = serde_json::json!({
            "version": 1,
            "mode": mode,
            "selector": {"name": name, "path": path},
            "private_root": self.private_root,
            "context": {
                "cwd": self.context.cwd().to_str().ok_or(INVALID)?,
                "pwd": self.context.pwd(),
                "environment": self.context.environment(),
            },
            "revision": revision,
            "nonce": self.nonce,
        });
        let bytes = serde_json::to_vec(&value).map_err(|_| INVALID)?;
        if bytes.len() > LIMIT {
            return Err(INVALID);
        }
        Ok(bytes)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::ffi::OsString;
    use std::path::Path;
    fn context(pwd: Option<&str>) -> ResolutionContextRequest {
        let mut entries: Vec<(OsString, OsString)> = vec![
            ("HOME".into(), "/supplied/home".into()),
            ("TRACK_CACHE_DIR".into(), "~/cache".into()),
            ("TRACK_WEB_DESKTOP_PARENT_PID".into(), "123".into()),
            (
                "TRACK_WEB_DESKTOP_READY_TOKEN".into(),
                "a".repeat(64).into(),
            ),
            ("XDG_CONFIG_HOME".into(), "".into()),
        ];
        if let Some(pwd) = pwd {
            entries.push(("PWD".into(), pwd.into()));
        }
        ResolutionContextRequest::capture(Path::new("/supplied/cwd"), &entries).unwrap()
    }
    fn request(pwd: Option<&str>) -> BoundLaunchRequest {
        BoundLaunchRequest::new(
            context(pwd),
            RequestedSelector::Path("~/notes".into()),
            "/supplied/private".into(),
            "b".repeat(32),
        )
        .unwrap()
    }
    #[test]
    fn phases_share_exact_final_context_and_have_no_revision_fallback() {
        let request = request(None);
        let pre: serde_json::Value = serde_json::from_slice(&request.preflight().unwrap()).unwrap();
        let serve: serde_json::Value =
            serde_json::from_slice(&request.serve(&"c".repeat(64)).unwrap()).unwrap();
        assert_eq!(pre["context"], serve["context"]);
        assert_eq!(pre["selector"], serve["selector"]);
        assert_eq!(pre["nonce"], serve["nonce"]);
        assert_eq!(pre["selector"]["path"], "~/notes");
        assert_eq!(pre["mode"], "preflight");
        assert_eq!(pre["revision"], "");
        assert_eq!(serve["mode"], "serve");
        assert_eq!(serve["revision"], "c".repeat(64));
        assert!(pre["context"]["pwd"].is_null());
        assert!(
            pre["context"]["environment"]
                .as_array()
                .unwrap()
                .contains(&serde_json::json!("TRACK_CACHE_DIR=~/cache"))
        );
        for bad in ["".to_owned(), "g".repeat(64), "a".repeat(63)] {
            assert!(request.serve(&bad).is_err());
        }
        assert_eq!(
            format!("{request:?}"),
            "BoundLaunchRequest { values: [redacted] }"
        );
    }
    #[test]
    fn pwd_empty_and_lexical_alias_are_not_invented_or_normalized() {
        for pwd in ["", "/supplied/../cwd"] {
            let bytes = request(Some(pwd)).preflight().unwrap();
            let value: serde_json::Value = serde_json::from_slice(&bytes).unwrap();
            assert_eq!(value["context"]["pwd"], pwd);
        }
    }
    #[test]
    fn malformed_owner_selector_root_and_nonce_never_encode() {
        let missing =
            ResolutionContextRequest::capture(Path::new("/cwd"), &[("HOME".into(), "x".into())])
                .unwrap();
        assert!(
            BoundLaunchRequest::new(
                missing,
                RequestedSelector::Registered("work".into()),
                "/private".into(),
                "a".repeat(32)
            )
            .is_err()
        );
        for nonce in [
            "".to_owned(),
            "g".repeat(32),
            "a".repeat(33),
            "a".repeat(130),
        ] {
            assert!(
                BoundLaunchRequest::new(
                    context(None),
                    RequestedSelector::Registered("work".into()),
                    "/private".into(),
                    nonce
                )
                .is_err()
            );
        }
        for root in ["relative", "/a/../b", "/a//b"] {
            assert!(
                BoundLaunchRequest::new(
                    context(None),
                    RequestedSelector::Registered("work".into()),
                    root.into(),
                    "a".repeat(32)
                )
                .is_err()
            );
        }
        for selector in [
            RequestedSelector::Registered("Invalid".into()),
            RequestedSelector::Path("".into()),
        ] {
            assert!(
                BoundLaunchRequest::new(context(None), selector, "/private".into(), "a".repeat(32))
                    .is_err()
            );
        }
    }
}
