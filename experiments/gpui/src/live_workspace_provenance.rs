//! Inactive, independent request/observation comparison. Neither type grants
//! admission, verifies filesystem provenance, or replaces Go manifest checking.
use serde::Deserialize;
use std::collections::BTreeMap;
use std::fmt;

const LIMIT: usize = 1 << 20;
const PATH_LIMIT: usize = 4096;
const INVALID: &str = "Invalid workspace provenance observation";

fn text(value: &str, limit: usize) -> bool {
    value.len() <= limit && !value.chars().any(char::is_control)
}
fn path(value: &str) -> bool {
    value.starts_with('/')
        && text(value, PATH_LIMIT)
        && (value == "/"
            || !value[1..]
                .split('/')
                .any(|p| p.is_empty() || p == "." || p == ".."))
}
fn name(value: &str) -> bool {
    !value.is_empty()
        && value.len() <= 256
        && value
            .bytes()
            .all(|b| b.is_ascii_lowercase() || b.is_ascii_digit() || b == b'-')
}
fn digest(value: &str) -> bool {
    value.len() == 64
        && value
            .bytes()
            .all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b))
}
fn identity(value: &str) -> bool {
    !value.is_empty() && text(value, PATH_LIMIT)
}

#[derive(Clone, PartialEq, Eq)]
// Path is an opaque raw selector: only Go expands tilde or resolves relative paths.
pub(super) enum RequestedSelector {
    Registered(String),
    Path(String),
}
#[derive(Clone, PartialEq, Eq)]
pub(super) enum PeerScopeRequest {
    Unresolved,
    ExplicitRequestedSet(Vec<(String, String)>),
}
#[derive(Clone, PartialEq, Eq)]
pub(super) struct WorkspaceScopeRequest {
    selector: RequestedSelector,
    peers: Option<BTreeMap<String, String>>,
}
impl fmt::Debug for WorkspaceScopeRequest {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("WorkspaceScopeRequest { values: [redacted] }")
    }
}
impl WorkspaceScopeRequest {
    pub(super) fn new(
        selector: RequestedSelector,
        scope: PeerScopeRequest,
    ) -> Result<Self, &'static str> {
        if !match &selector {
            RequestedSelector::Registered(n) => name(n),
            RequestedSelector::Path(p) => !p.is_empty() && text(p, PATH_LIMIT),
        } {
            return Err("Invalid requested workspace selector");
        }
        let peers = match scope {
            PeerScopeRequest::Unresolved => None,
            PeerScopeRequest::ExplicitRequestedSet(entries) => {
                if entries.is_empty() || entries.len() > 33 {
                    return Err("Invalid requested workspace set");
                }
                let mut peers = BTreeMap::new();
                for (n, p) in entries {
                    if (!n.is_empty() && !name(&n)) || !path(&p) || peers.insert(n, p).is_some() {
                        return Err("Invalid requested workspace set");
                    }
                }
                if peers.keys().filter(|n| !n.is_empty()).count() > 32 {
                    return Err("Invalid requested workspace set");
                }
                Some(peers)
            }
        };
        Ok(Self { selector, peers })
    }
}

#[derive(Clone, PartialEq, Eq, Deserialize)]
#[serde(deny_unknown_fields)]
struct Selector {
    name: String,
    path: String,
}
#[derive(Clone, PartialEq, Eq, Deserialize)]
#[serde(deny_unknown_fields)]
struct Context {
    home: String,
    cwd: String,
    xdg_cache_home: String,
    xdg_config_home: String,
    environment_revision: String,
}
#[derive(Clone, PartialEq, Eq, Deserialize)]
#[serde(deny_unknown_fields)]
struct Vault {
    name: String,
    display_path: String,
    canonical_path: String,
    root_identity: String,
    ordinary_db: String,
    resolved_ordinary_db: String,
    ordinary_anchor: String,
    ordinary_identity: String,
    private_db: String,
}
impl Vault {
    fn valid(&self) -> bool {
        (self.name.is_empty() || name(&self.name))
            && [
                &self.display_path,
                &self.canonical_path,
                &self.ordinary_db,
                &self.resolved_ordinary_db,
                &self.ordinary_anchor,
                &self.private_db,
            ]
            .into_iter()
            .all(|p| path(p))
            && identity(&self.root_identity)
            && identity(&self.ordinary_identity)
    }
    fn same_except_display(&self, peer: &Self) -> bool {
        let mut copy = self.clone();
        copy.display_path = peer.display_path.clone();
        &copy == peer
    }
}
#[derive(Clone, PartialEq, Eq, Deserialize)]
#[serde(deny_unknown_fields)]
struct Configuration {
    path: String,
    canonical_path: String,
    present: bool,
    identity: String,
    size: i64,
    mode: u32,
    modified_ns: i64,
    sha256: String,
}
impl Configuration {
    fn valid(&self) -> bool {
        path(&self.path)
            && path(&self.canonical_path)
            && if self.present {
                identity(&self.identity)
                    && (0..=LIMIT as i64).contains(&self.size)
                    && digest(&self.sha256)
                // modified_ns may legitimately be zero or negative. Mode is retained
                // as Go's opaque FileMode, not interpreted as Unix stat mode.
            } else {
                self.identity.is_empty()
                    && self.size == 0
                    && self.mode == 0
                    && self.modified_ns == 0
                    && self.sha256.is_empty()
            }
    }
}
#[derive(Clone, PartialEq, Eq, Deserialize)]
#[serde(deny_unknown_fields)]
struct Manifest {
    version: u32,
    selector: Selector,
    private_root: String,
    private_mode: u32,
    private_identity: String,
    context: Context,
    active: Vault,
    peers: Vec<Vault>,
    configurations: Vec<Configuration>,
    revision: String,
}
#[derive(Clone, PartialEq, Eq)]
pub(super) struct LaunchManifestObservation {
    wire: Manifest,
}
impl fmt::Debug for LaunchManifestObservation {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("LaunchManifestObservation { values: [redacted] }")
    }
}
impl LaunchManifestObservation {
    pub(super) fn parse(bytes: &[u8]) -> Result<Self, &'static str> {
        if bytes.len() > LIMIT {
            return Err(INVALID);
        }
        let wire: Manifest = serde_json::from_slice(bytes).map_err(|_| INVALID)?;
        let selector_ok = (wire.selector.name.is_empty() && text(&wire.selector.path, PATH_LIMIT))
            || (name(&wire.selector.name) && wire.selector.path.is_empty());
        if wire.version != 1
            || !selector_ok
            || !path(&wire.private_root)
            || wire.private_mode & 0x8000_0000 == 0
            || wire.private_mode & 0o077 != 0
            || !identity(&wire.private_identity)
            || !digest(&wire.revision)
            || !digest(&wire.context.environment_revision)
            || wire.context.home.is_empty()
            || !text(&wire.context.home, PATH_LIMIT)
            // Go os.Getwd may preserve an absolute lexical PWD alias.
            // Unlike resolved manifest paths, context cwd need not be clean.
            || !wire.context.cwd.starts_with('/')
            || !text(&wire.context.cwd, PATH_LIMIT)
            || !text(&wire.context.xdg_cache_home, PATH_LIMIT)
            || !text(&wire.context.xdg_config_home, PATH_LIMIT)
            || !wire.active.valid()
            || wire.peers.is_empty()
            || wire.peers.len() > 33
            || wire.configurations.len() != wire.peers.len() + 1
            || !wire.configurations.iter().all(Configuration::valid)
            || wire.configurations.iter().map(|c| c.size).sum::<i64>() > 8 * LIMIT as i64
        {
            return Err(INVALID);
        }
        let mut configurations = BTreeMap::new();
        for config in &wire.configurations {
            if configurations
                .insert(&config.path, config)
                .is_some_and(|old| old != config)
            {
                return Err(INVALID);
            }
        }
        let mut peers = BTreeMap::new();
        let mut canonical_paths = std::collections::BTreeSet::new();
        for peer in &wire.peers {
            if !peer.valid()
                || peers.insert(&peer.name, peer).is_some()
                || !canonical_paths.insert(&peer.canonical_path)
            {
                return Err(INVALID);
            }
        }
        if peers.keys().filter(|n| !n.is_empty()).count() > 32
            || (peers.contains_key(&String::new()) && !wire.active.name.is_empty())
            || !peers
                .get(&wire.active.name)
                .is_some_and(|p| wire.active.same_except_display(p))
            || (!wire.selector.name.is_empty() && wire.selector.name != wire.active.name)
        {
            return Err(INVALID);
        }
        Ok(Self { wire })
    }
    /// Exact agreement with independent requested inputs only. Success is not
    /// authority, a fresh filesystem check, nor validation of Go's digest.
    pub(super) fn check_requested_scope(
        &self,
        request: &WorkspaceScopeRequest,
    ) -> Result<(), &'static str> {
        let peers = request
            .peers
            .as_ref()
            .ok_or("Requested workspace scope is unresolved")?;
        let selected = match &request.selector {
            RequestedSelector::Registered(n) => {
                self.wire.selector.name == *n && self.wire.selector.path.is_empty()
            }
            RequestedSelector::Path(p) => {
                self.wire.selector.path == *p && self.wire.selector.name.is_empty()
            }
        };
        let observed: BTreeMap<_, _> = self
            .wire
            .peers
            .iter()
            .map(|p| (p.name.clone(), p.display_path.clone()))
            .collect();
        if !selected || peers != &observed {
            return Err("Workspace observation differs from requested scope");
        }
        Ok(())
    }
    pub(super) fn same_provenance(&self, other: &Self) -> bool {
        self == other
    }
    pub(super) fn revision(&self) -> &str {
        &self.wire.revision
    }
    pub(super) fn private_binding_matches(&self, path: &str, identity: &str) -> bool {
        self.wire.private_root == path
            && self.wire.private_identity == identity
            && self.wire.private_mode == ((1 << 31) | 0o700)
            && self
                .wire
                .peers
                .iter()
                .all(|peer| std::path::Path::new(&peer.private_db).starts_with(path))
    }
    pub(super) fn active(&self) -> (&str, &str) {
        (&self.wire.active.name, &self.wire.active.display_path)
    }
    pub(super) fn peers(&self) -> Vec<(String, String)> {
        self.wire
            .peers
            .iter()
            .map(|v| (v.name.clone(), v.display_path.clone()))
            .collect()
    }
    #[cfg(test)]
    pub(super) fn private_db(&self, name: &str) -> Option<&str> {
        self.wire
            .peers
            .iter()
            .find(|peer| peer.name == name)
            .map(|peer| peer.private_db.as_str())
    }
    pub(super) fn same_workspace(&self, other: &Self) -> bool {
        let mut normalized = other.clone();
        normalized.wire.private_root = self.wire.private_root.clone();
        normalized.wire.private_mode = self.wire.private_mode;
        normalized.wire.private_identity = self.wire.private_identity.clone();
        normalized.wire.revision = self.wire.revision.clone();
        normalized.wire.active.private_db = self.wire.active.private_db.clone();
        if normalized.wire.peers.len() != self.wire.peers.len() {
            return false;
        }
        for (next, old) in normalized.wire.peers.iter_mut().zip(&self.wire.peers) {
            next.private_db = old.private_db.clone();
        }
        self == &normalized
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::{Value, json};
    fn sample() -> Value {
        let vault = json!({"name":"work","display_path":"/alias/work","canonical_path":"/vault/work","root_identity":"root-opaque","ordinary_db":"/normal/index.db","resolved_ordinary_db":"/normal/index.db","ordinary_anchor":"/normal","ordinary_identity":"normal-opaque","private_db":"/private/work/index.db"});
        let absent = json!({"path":"/config","canonical_path":"/config","present":false,"identity":"","size":0,"mode":0,"modified_ns":0,"sha256":""});
        json!({"version":1,"selector":{"name":"work","path":""},"private_root":"/private","private_mode":0x800001c0u32,"private_identity":"private-opaque","context":{"home":"relative-home","cwd":"/cwd","xdg_cache_home":"relative/cache","xdg_config_home":"","environment_revision":"a".repeat(64)},"active":vault,"peers":[vault],"configurations":[absent, {"path":"/vault/work/.track/config.yml","canonical_path":"/vault/work/.track/config.yml","present":true,"identity":"config-opaque","size":2,"mode":420,"modified_ns":-1,"sha256":"b".repeat(64)}],"revision":"c".repeat(64)})
    }
    fn parse(v: &Value) -> Result<LaunchManifestObservation, &'static str> {
        LaunchManifestObservation::parse(&serde_json::to_vec(v).unwrap())
    }
    fn request(scope: PeerScopeRequest) -> WorkspaceScopeRequest {
        WorkspaceScopeRequest::new(RequestedSelector::Registered("work".into()), scope).unwrap()
    }
    fn explicit() -> PeerScopeRequest {
        PeerScopeRequest::ExplicitRequestedSet(vec![("work".into(), "/alias/work".into())])
    }
    #[test]
    fn independent_scope_is_required_and_exact() {
        let observation = parse(&sample()).unwrap();
        assert!(
            observation
                .check_requested_scope(&request(PeerScopeRequest::Unresolved))
                .is_err()
        );
        assert!(
            observation
                .check_requested_scope(&request(explicit()))
                .is_ok()
        );
        let changed = request(PeerScopeRequest::ExplicitRequestedSet(vec![(
            "work".into(),
            "/other".into(),
        )]));
        assert!(observation.check_requested_scope(&changed).is_err());
        let wrong =
            WorkspaceScopeRequest::new(RequestedSelector::Path("/alias/work".into()), explicit())
                .unwrap();
        assert!(observation.check_requested_scope(&wrong).is_err());
        for entries in [
            vec![],
            vec![("work".into(), "/a".into()), ("work".into(), "/b".into())],
        ] {
            assert!(
                WorkspaceScopeRequest::new(
                    RequestedSelector::Registered("work".into()),
                    PeerScopeRequest::ExplicitRequestedSet(entries)
                )
                .is_err()
            );
        }
        for selector in [
            RequestedSelector::Path("".into()),
            RequestedSelector::Path("bad\npath".into()),
            RequestedSelector::Registered("Bad".into()),
        ] {
            assert!(WorkspaceScopeRequest::new(selector, explicit()).is_err());
        }
        assert_eq!(
            format!("{observation:?}"),
            "LaunchManifestObservation { values: [redacted] }"
        );
        assert_eq!(
            format!("{:?}", request(explicit())),
            "WorkspaceScopeRequest { values: [redacted] }"
        );
    }
    #[test]
    fn raw_path_selectors_are_preserved_without_native_resolution() {
        for raw in ["relative/vault", "~/vault", "../vault", "/a/../vault"] {
            let mut value = sample();
            value["selector"] = json!({"name":"", "path":raw});
            let observation = parse(&value).unwrap();
            let request =
                WorkspaceScopeRequest::new(RequestedSelector::Path(raw.into()), explicit())
                    .unwrap();
            assert!(observation.check_requested_scope(&request).is_ok());
            // Even if Go could resolve these to the same root, spelling changes
            // are context drift. The resolved peer/display set stays separate.
            let changed =
                WorkspaceScopeRequest::new(RequestedSelector::Path(format!("{raw}/.")), explicit())
                    .unwrap();
            assert!(observation.check_requested_scope(&changed).is_err());
            let changed = WorkspaceScopeRequest::new(
                RequestedSelector::Path("/vault/work".into()),
                explicit(),
            )
            .unwrap();
            assert!(observation.check_requested_scope(&changed).is_err());
            let mut other = value.clone();
            other["selector"]["path"] = json!(format!("{raw}/."));
            assert!(!observation.same_provenance(&parse(&other).unwrap()));
        }
        let raw = "x".repeat(PATH_LIMIT);
        assert!(
            WorkspaceScopeRequest::new(RequestedSelector::Path(raw.clone()), explicit()).is_ok()
        );
        assert!(
            WorkspaceScopeRequest::new(RequestedSelector::Path(raw + "x"), explicit()).is_err()
        );
    }
    #[test]
    fn aliases_absence_and_pre_epoch_times_are_valid_without_losing_drift() {
        let original = parse(&sample()).unwrap();
        let mut lexical = sample();
        lexical["context"]["cwd"] = json!("/a/../cwd//.");
        let observed = parse(&lexical).unwrap();
        assert!(!original.same_provenance(&observed));
        assert!(observed.check_requested_scope(&request(explicit())).is_ok());
        let mut value = sample();
        value["active"]["display_path"] = json!("/another/alias");
        let alias = parse(&value).unwrap();
        assert!(alias.check_requested_scope(&request(explicit())).is_ok());
        assert!(!original.same_provenance(&alias));
        value["configurations"][1]["modified_ns"] = json!(0);
        assert!(parse(&value).is_ok());
        value["active"]["root_identity"] = json!("different");
        assert!(parse(&value).is_err());
        let mut value = sample();
        value["selector"] = json!({"name":"","path":"/unregistered"});
        value["active"]["name"] = json!("");
        value["peers"][0]["name"] = json!("");
        let parsed = parse(&value).unwrap();
        let req = WorkspaceScopeRequest::new(
            RequestedSelector::Path("/unregistered".into()),
            PeerScopeRequest::ExplicitRequestedSet(vec![("".into(), "/alias/work".into())]),
        )
        .unwrap();
        assert!(parsed.check_requested_scope(&req).is_ok());
    }
    #[test]
    fn unknown_missing_duplicate_fields_and_malformed_provenance_fail_closed() {
        for pointer in [
            "/extra",
            "/selector/extra",
            "/context/extra",
            "/active/extra",
            "/peers/0/extra",
            "/configurations/0/extra",
        ] {
            let mut v = sample();
            let (parent, key) = pointer.rsplit_once('/').unwrap();
            v.pointer_mut(parent)
                .unwrap()
                .as_object_mut()
                .unwrap()
                .insert(key.into(), json!(true));
            assert!(parse(&v).is_err());
        }
        for (pointer, value) in [
            ("/private_mode", json!(448)),
            ("/revision", json!("bad")),
            ("/active/private_db", json!("relative")),
            ("/context/cwd", json!("relative")),
            ("/configurations/0/sha256", json!("d".repeat(64))),
            ("/configurations/1/size", json!(-1)),
            ("/peers", json!([])),
        ] {
            let mut v = sample();
            *v.pointer_mut(pointer).unwrap() = value;
            assert!(parse(&v).is_err(), "{pointer}");
        }
        let mut v = sample();
        v.as_object_mut().unwrap().remove("revision");
        assert!(parse(&v).is_err());
        let raw = serde_json::to_string(&sample()).unwrap();
        let duplicate = format!("{{\"version\":1,{}", &raw[1..]);
        assert!(LaunchManifestObservation::parse(duplicate.as_bytes()).is_err());
        assert!(LaunchManifestObservation::parse(&vec![b' '; LIMIT + 1]).is_err());
    }
    #[test]
    fn go_default_modes_and_repeated_config_observations_remain_opaque() {
        let mut value = sample();
        value["selector"] = json!({"name":"", "path":""});
        // Go FileMode directory + owner-read-only + setuid/sticky is not Unix
        // stat mode. Shape checks do not establish ownership or write access.
        value["private_mode"] = json!(0x80000000u32 | 0o400 | 0x00900000);
        let default = parse(&value).unwrap();
        assert!(default.check_requested_scope(&request(explicit())).is_err());
        value["configurations"][1] = value["configurations"][0].clone();
        assert!(parse(&value).is_ok());
        value["configurations"][1]["canonical_path"] = json!("/different");
        assert!(parse(&value).is_err());
        let mut value = sample();
        value["configurations"][1]["mode"] = json!(0o644 | 0x00d00000u32);
        assert!(parse(&value).is_ok());
        value["private_mode"] = json!(0x800001c1u32);
        assert!(parse(&value).is_err());
        value = sample();
        value["selector"]["path"] = json!("/also-path");
        assert!(parse(&value).is_err());
    }
    #[test]
    fn registered_peer_bound_allows_one_unnamed_selected_peer() {
        let mut v = sample();
        let mut peers = Vec::new();
        let mut configs = vec![v["configurations"][0].clone()];
        for i in 0..33 {
            let mut peer = v["active"].clone();
            peer["canonical_path"] = json!(format!("/vault/{i}"));
            peer["name"] = json!(if i == 32 {
                String::new()
            } else {
                format!("v{i}")
            });
            peers.push(peer);
            configs.push(v["configurations"][1].clone());
        }
        v["active"] = peers[32].clone();
        v["peers"] = json!(peers);
        v["configurations"] = json!(configs);
        v["selector"] = json!({"name":"","path":"/alias/work"});
        assert!(parse(&v).is_ok());
        let mut nonactive_unnamed = v.clone();
        nonactive_unnamed["active"] = nonactive_unnamed["peers"][0].clone();
        assert!(parse(&nonactive_unnamed).is_err());
        v["peers"][32]["name"] = json!("v32");
        v["active"]["name"] = json!("v32");
        assert!(parse(&v).is_err());
    }
    #[test]
    fn private_root_is_checked_independently_and_workspace_comparison_keeps_ordinary_provenance() {
        let initial = parse(&sample()).unwrap();
        assert!(initial.private_binding_matches("/private", "private-opaque"));
        assert!(!initial.private_binding_matches("/elsewhere", "private-opaque"));
        assert!(!initial.private_binding_matches("/private", "other-identity"));
        let mut child = sample();
        child["private_root"] = json!("/child");
        child["private_identity"] = json!("child-identity");
        child["revision"] = json!("d".repeat(64));
        child["active"]["private_db"] = json!("/child/work/index.db");
        child["peers"][0]["private_db"] = json!("/child/work/index.db");
        let observed = parse(&child).unwrap();
        assert!(initial.same_workspace(&observed));
        assert!(observed.private_binding_matches("/child", "child-identity"));
        child["active"]["ordinary_identity"] = json!("changed");
        child["peers"][0]["ordinary_identity"] = json!("changed");
        assert!(!initial.same_workspace(&parse(&child).unwrap()));
    }
}
