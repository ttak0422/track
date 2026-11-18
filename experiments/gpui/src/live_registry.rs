//! Registry comparison only, not launch admission or config interpretation.
//! Expected identities must originate independently of the response being checked.
//! No constructor resolves YAML/HOME, captures filesystem authority, or permits
//! spawning. Ordinary launch remains fixture-gated. Storage health/epoch and
//! capability checks remain separate from a matching registry observation. Callers
//! must bind observations to the owned launch/source/epoch; this model holds no
//! generation or persistent authority. Display paths are compared exactly, not
//! canonicalized or treated as an engine-resolved protection manifest.
use serde::Deserialize;
use std::collections::{BTreeMap, BTreeSet};
use std::path::{Component, Path};

const MAX_PEERS: usize = 128;
const MAX_WIRE: usize = 1024 * 1024;
const MAX_PATH: usize = 4096;
const MAX_NAME: usize = 256;
const MAX_REASON: usize = 4096;

#[derive(Clone, Debug)]
pub(super) struct ExpectedRegistry {
    selected: String,
    paths: BTreeMap<String, String>,
}
#[derive(Debug, PartialEq, Eq)]
pub(super) enum RegistryObservation {
    /// Recognized bootstrap placeholder only; never grants identity/readiness.
    Initializing,
    /// Exact union matched. Unavailable peers are retained, not silently omitted.
    /// This is NOT a storage health or filesystem protection proof.
    Matched {
        unavailable: BTreeMap<String, String>,
    },
}
#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Wire {
    active: Active,
    vaults: Vec<Entry>,
    unavailable: Vec<Unavailable>,
    #[serde(default, deserialize_with = "present_string")]
    storage_state: Option<String>,
    #[serde(default)]
    read_state: Option<ReadDiagnostics>,
}
#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct ReadDiagnostics {
    #[serde(rename = "partial")]
    _partial: bool,
    #[serde(rename = "cache_used")]
    _cache_used: bool,
    #[serde(rename = "checked_at")]
    _checked_at: i64,
    issues: Vec<serde_json::Value>,
    #[serde(default)]
    #[serde(rename = "omitted")]
    _omitted: u64,
}
#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Active {
    name: String,
    path: String,
}
#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Entry {
    name: String,
    path: String,
    active: bool,
}
#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Unavailable {
    name: String,
    path: String,
    #[serde(default, deserialize_with = "present_string")]
    error: Option<String>,
}
fn present_string<'de, D: serde::Deserializer<'de>>(d: D) -> Result<Option<String>, D::Error> {
    String::deserialize(d).map(Some)
}
fn bounded(value: &str, limit: usize) -> bool {
    value.len() <= limit && !value.chars().any(char::is_control)
}
fn absolute_path(value: &str) -> bool {
    bounded(value, MAX_PATH)
        && !value.split('/').any(|part| part == "." || part == "..")
        && Path::new(value).is_absolute()
        && Path::new(value)
            .components()
            .all(|c| matches!(c, Component::RootDir | Component::Normal(_)))
}
impl ExpectedRegistry {
    pub(super) fn new(selected: String, paths: BTreeMap<String, String>) -> Result<Self, String> {
        if paths.is_empty() || paths.len() > MAX_PEERS || !paths.contains_key(&selected) {
            return Err(
                "Expected registry must contain selected identity within peer limit".into(),
            );
        }
        let mut unique = BTreeSet::new();
        for (name, path) in &paths {
            if !bounded(name, MAX_NAME)
                || (name.is_empty() && name != &selected)
                || !absolute_path(path)
                || !unique.insert(path)
            {
                return Err("Invalid or duplicate expected registry identity".into());
            }
        }
        Ok(Self { selected, paths })
    }
    pub(super) fn observe(&self, bytes: &[u8]) -> Result<RegistryObservation, String> {
        if bytes.len() > MAX_WIRE {
            return Err("Registry response exceeds byte limit".into());
        }
        // Typed decoding rejects duplicate JSON fields and wrong types, unlike
        // validation after decoding through a generic Value.
        let wire: Wire =
            serde_json::from_slice(bytes).map_err(|e| format!("Invalid registry shape: {e}"))?;
        if wire
            .read_state
            .as_ref()
            .is_some_and(|state| state.issues.len() > 256)
        {
            return Err("Registry diagnostic issue limit exceeded".into());
        }
        if wire.vaults.len().saturating_add(wire.unavailable.len()) > MAX_PEERS + 1
            || !bounded(&wire.active.name, MAX_NAME)
            || !bounded(&wire.active.path, MAX_PATH)
            || wire
                .vaults
                .iter()
                .any(|e| !bounded(&e.name, MAX_NAME) || !bounded(&e.path, MAX_PATH))
            || wire.unavailable.iter().any(|e| {
                !bounded(&e.name, MAX_NAME)
                    || !bounded(&e.path, MAX_PATH)
                    || e.error.as_ref().is_some_and(|s| !bounded(s, MAX_REASON))
            })
        {
            return Err("Registry field/count limit exceeded".into());
        }
        let pending = match wire.storage_state.as_deref() {
            None | Some("ready") => false,
            Some("initializing" | "partial" | "unavailable") => true,
            _ => return Err("Unknown registry storage phase".into()),
        };
        let selected_path = &self.paths[&self.selected];
        let active_matches =
            wire.active.name == self.selected && &wire.active.path == selected_path;
        let placeholder_gap = wire.unavailable.len() == 1
            && wire.unavailable[0].name.is_empty()
            && wire.unavailable[0].path.is_empty();
        if pending && placeholder_gap {
            let empty = wire.active.name.is_empty()
                && wire.active.path.is_empty()
                && wire.vaults.is_empty();
            let selected_only = active_matches
                && wire.vaults.len() == 1
                && wire.vaults[0].name == self.selected
                && &wire.vaults[0].path == selected_path
                && wire.vaults[0].active;
            if empty || selected_only {
                return Ok(RegistryObservation::Initializing);
            }
            return Err("Malformed bootstrap registry placeholder".into());
        }
        if !active_matches {
            return Err("Active registry identity mismatch".into());
        }
        let mut names = BTreeSet::new();
        let mut active_found = false;
        let mut unavailable = BTreeMap::new();
        for entry in &wire.vaults {
            self.match_entry(&entry.name, &entry.path, &mut names)?;
            if entry.active != (entry.name == self.selected) {
                return Err("Registry active flag mismatch".into());
            }
            active_found |= entry.active;
        }
        for entry in &wire.unavailable {
            self.match_entry(&entry.name, &entry.path, &mut names)?;
            unavailable.insert(
                entry.name.clone(),
                entry.error.clone().unwrap_or_else(|| "Unavailable".into()),
            );
        }
        if names.len() != self.paths.len() || !active_found {
            return Err("Incomplete registry identity union".into());
        }
        Ok(RegistryObservation::Matched { unavailable })
    }
    fn match_entry(
        &self,
        name: &str,
        path: &str,
        names: &mut BTreeSet<String>,
    ) -> Result<(), String> {
        let expected = self.paths.get(name).ok_or("Unexpected registry name")?;
        if expected != path || !names.insert(name.to_owned()) {
            return Err("Changed or duplicate registry identity".into());
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::{Value, json};
    fn expected() -> ExpectedRegistry {
        ExpectedRegistry::new(
            "work".into(),
            BTreeMap::from([
                ("main".into(), "/fixture/main".into()),
                ("work".into(), "/fixture/work".into()),
            ]),
        )
        .unwrap()
    }
    fn wire() -> Value {
        json!({"active":{"name":"work","path":"/fixture/work"},"vaults":[{"name":"work","path":"/fixture/work","active":true},{"name":"main","path":"/fixture/main","active":false}],"unavailable":[]})
    }
    fn observe(v: Value) -> Result<RegistryObservation, String> {
        expected().observe(&serde_json::to_vec(&v).unwrap())
    }
    #[test]
    fn available_unavailable_transitions_preserve_exact_union() {
        assert_eq!(
            observe(wire()),
            Ok(RegistryObservation::Matched {
                unavailable: BTreeMap::new()
            })
        );
        let mut v = wire();
        v["vaults"].as_array_mut().unwrap().pop();
        v["unavailable"] =
            json!([{"name":"main","path":"/fixture/main","error":"temporarily unavailable"}]);
        assert_eq!(
            observe(v.clone()),
            Ok(RegistryObservation::Matched {
                unavailable: BTreeMap::from([("main".into(), "temporarily unavailable".into())])
            })
        );
        v["storage_state"] = json!("partial");
        assert!(matches!(
            observe(v),
            Ok(RegistryObservation::Matched { .. })
        ));
        assert!(matches!(
            observe(wire()),
            Ok(RegistryObservation::Matched { .. })
        ));
    }
    #[test]
    fn missing_duplicate_changed_and_active_mismatch_are_errors() {
        for change in 0..8 {
            let mut v = wire();
            match change {
                0 => {
                    v["vaults"].as_array_mut().unwrap().pop();
                }
                1 => v["vaults"][1]["name"] = json!("work"),
                2 => v["vaults"][1]["path"] = json!("/elsewhere"),
                3 => v["active"]["name"] = json!("main"),
                4 => v["vaults"][1]["active"] = json!(true),
                5 => v["vaults"][1] = json!({"name":"unknown","active":false}),
                6 => v["vaults"][1]["path"] = json!(42),
                _ => v["unavailable"] = json!([{"name":"main","path":"/fixture/main"}]),
            }
            assert!(observe(v).is_err(), "case {change}");
        }
    }
    #[test]
    fn only_exact_bounded_bootstrap_shapes_are_pending() {
        let mut v = json!({"active":{"name":"","path":""},"vaults":[],"unavailable":[{"name":"","path":"","error":"opening"}],"storage_state":"initializing"});
        assert_eq!(observe(v.clone()), Ok(RegistryObservation::Initializing));
        v["active"] = wire()["active"].clone();
        v["vaults"] = json!([wire()["vaults"][0].clone()]);
        assert_eq!(observe(v.clone()), Ok(RegistryObservation::Initializing));
        v["vaults"][0]["path"] = json!("/wrong");
        assert!(observe(v.clone()).is_err());
        v["storage_state"] = json!("unknown");
        assert!(observe(v).is_err());
        let mut v = wire();
        v["storage_state"] = json!(null);
        assert!(observe(v).is_err());
    }
    #[test]
    fn wire_limits_and_duplicate_fields_fail_without_truncation() {
        assert!(expected().observe(&vec![b' '; MAX_WIRE + 1]).is_err());
        let mut v = wire();
        v["active"]["name"] = json!("x".repeat(MAX_NAME + 1));
        assert!(observe(v).is_err());
        let mut v = wire();
        v["vaults"] = Value::Array(vec![wire()["vaults"][0].clone(); MAX_PEERS + 2]);
        assert!(observe(v).is_err());
        let mut v = wire();
        v["unavailable"] =
            json!([{"name":"main","path":"/fixture/main","error":"x".repeat(MAX_REASON+1)}]);
        assert!(observe(v).is_err());
        let raw = serde_json::to_string(&wire()).unwrap();
        let duplicate = raw.replacen("\"active\":true", "\"active\":true,\"active\":true", 1);
        assert!(expected().observe(duplicate.as_bytes()).is_err());
        let mut v = wire();
        v["unexpected"] = json!(true);
        assert!(observe(v).is_err());
    }
    #[test]
    fn expectations_are_independent_and_unregistered_launch_alias_is_explicit() {
        assert!(ExpectedRegistry::new("other".into(), expected().paths).is_err());
        assert!(
            ExpectedRegistry::new(
                "main".into(),
                BTreeMap::from([
                    ("main".into(), "/same".into()),
                    ("work".into(), "/same".into())
                ])
            )
            .is_err()
        );
        for path in ["relative", "/a/../b", "/a/./b"] {
            assert!(
                ExpectedRegistry::new("".into(), BTreeMap::from([("".into(), path.into())]))
                    .is_err()
            );
        }
        let alias = ExpectedRegistry::new(
            "".into(),
            BTreeMap::from([("".into(), "/fixture/main".into())]),
        )
        .unwrap();
        let v = json!({"active":{"name":"","path":"/fixture/main"},"vaults":[{"name":"","path":"/fixture/main","active":true}],"unavailable":[]});
        assert!(matches!(
            alias.observe(&serde_json::to_vec(&v).unwrap()),
            Ok(RegistryObservation::Matched { .. })
        ));
    }
}
