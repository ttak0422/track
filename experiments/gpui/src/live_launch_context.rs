//! Inactive, supplied-input resolution context. This is not an admission grant,
//! manifest proof, canonical filesystem identity, or a registry of allowed peers.
//! Go alone interprets path/environment semantics and produces the manifest.
use std::ffi::OsString;
use std::fmt;
use std::os::unix::ffi::OsStrExt;
use std::path::{Path, PathBuf};

const MAX_ENTRIES: usize = 256;
const MAX_JOINED_BYTES: usize = 64 * 1024;
const MAX_PATH_BYTES: usize = 4096;

#[derive(Clone, PartialEq, Eq)]
pub(super) struct ResolutionContextRequest {
    // Requested lexical cwd, not a claim about a subsequently observed Go cwd.
    cwd: PathBuf,
    // PWD can influence Go os.Getwd, but is NOT part of its environment hash.
    pwd: Option<String>,
    // Sorted complete KEY=value entries, exactly as Go's manifestContext sorts
    // them. Presence with an empty value is distinct from an absent entry.
    environment: Vec<String>,
}

impl fmt::Debug for ResolutionContextRequest {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("ResolutionContextRequest { values: [redacted] }")
    }
}

impl ResolutionContextRequest {
    pub(super) fn capture(
        cwd: &Path,
        supplied: &[(OsString, OsString)],
    ) -> Result<Self, &'static str> {
        let cwd_text = cwd.to_str().ok_or("Invalid resolution cwd")?;
        // Pure lexical validation. Path::components alone normalizes './' and
        // repeated separators, which would silently change supplied context.
        if !cwd.is_absolute()
            || cwd_text.len() > MAX_PATH_BYTES
            || cwd_text.chars().any(char::is_control)
            || (cwd_text != "/"
                && cwd_text[1..]
                    .split('/')
                    .any(|part| part.is_empty() || part == "." || part == ".."))
        {
            return Err("Resolution cwd must be bounded, absolute and clean");
        }
        let mut environment = Vec::new();
        let mut pwd = None;
        let mut keys = std::collections::HashSet::new();
        let mut joined_bytes = 0usize;
        let mut home_present = false;
        for (key, value) in supplied {
            let raw = key.as_os_str().as_bytes();
            if raw == b"PWD" {
                let value = value.to_str().ok_or("Invalid resolution PWD")?;
                if pwd.is_some()
                    || value.len() > MAX_PATH_BYTES
                    || value.chars().any(char::is_control)
                {
                    return Err("Invalid or duplicate resolution PWD");
                }
                pwd = Some(value.to_owned());
                continue;
            }
            if raw != b"HOME" && !raw.starts_with(b"TRACK_") && !raw.starts_with(b"XDG_") {
                continue;
            }
            if environment.len() == MAX_ENTRIES {
                return Err("Resolution environment exceeds entry bound");
            }
            let key = key.to_str().ok_or("Invalid resolution environment key")?;
            let value = value
                .to_str()
                .ok_or("Invalid resolution environment value")?;
            if key.contains('=')
                || key.chars().any(char::is_control)
                || value.chars().any(char::is_control)
                || !keys.insert(key)
            {
                return Err("Invalid or duplicate resolution environment entry");
            }
            if key == "HOME" {
                if value.is_empty() {
                    return Err("Resolution HOME must be present and nonempty");
                }
                home_present = true;
            }
            // Match Go's explicit context path fields. Other variables remain
            // opaque, including relative XDG values, tilde TRACK_CONFIG and
            // TRACK_CACHE_DIR (capturing it does not make it authoritative).
            if matches!(key, "HOME" | "XDG_CACHE_HOME" | "XDG_CONFIG_HOME")
                && value.len() > MAX_PATH_BYTES
            {
                return Err("Resolution context path exceeds bound");
            }
            joined_bytes = joined_bytes
                .checked_add(key.len())
                .and_then(|n| n.checked_add(1))
                .and_then(|n| n.checked_add(value.len()))
                .and_then(|n| n.checked_add(usize::from(!environment.is_empty())))
                .filter(|n| *n <= MAX_JOINED_BYTES)
                .ok_or("Resolution environment exceeds byte bound")?;
            environment.push(format!("{key}={value}"));
        }
        if !home_present {
            return Err("Resolution HOME must be present and nonempty");
        }
        environment.sort();
        Ok(Self {
            cwd: cwd.to_owned(),
            pwd,
            environment,
        })
    }

    pub(super) fn matches_observation(
        &self,
        cwd: &Path,
        supplied: &[(OsString, OsString)],
    ) -> bool {
        Self::capture(cwd, supplied).is_ok_and(|other| self == &other)
    }

    pub(super) fn cwd(&self) -> &Path {
        &self.cwd
    }
    pub(super) fn pwd(&self) -> Option<&str> {
        self.pwd.as_deref()
    }
    pub(super) fn environment(&self) -> &[String] {
        &self.environment
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::os::unix::ffi::OsStringExt;

    fn env(entries: &[(&str, &str)]) -> Vec<(OsString, OsString)> {
        entries
            .iter()
            .map(|(k, v)| ((*k).into(), (*v).into()))
            .collect()
    }
    fn capture(entries: &[(&str, &str)]) -> Result<ResolutionContextRequest, &'static str> {
        ResolutionContextRequest::capture(
            Path::new("/supplied/not-required-to-exist"),
            &env(entries),
        )
    }
    #[test]
    fn preserves_opaque_values_presence_and_go_entry_sorting() {
        let input = env(&[
            ("HOME", "/home/synthetic"),
            ("TRACK_CONFIG", "~/relative.yml"),
            ("XDG_CACHE_HOME", "relative/cache"),
            ("XDG_CONFIG_HOME", ""),
            ("TRACK_CACHE_DIR", "relative/cache-override"),
            ("TRACK_NEW", ""),
            ("XDG_UNKNOWN", "opaque"),
            ("TRACK_AZ", "z"),
            ("TRACK_A", "a"),
            ("UNRELATED", "ignored"),
        ]);
        let original =
            ResolutionContextRequest::capture(Path::new("/supplied/cwd"), &input).unwrap();
        assert_eq!(original.cwd(), Path::new("/supplied/cwd"));
        assert!(
            original
                .environment()
                .contains(&"TRACK_CONFIG=~/relative.yml".into())
        );
        assert!(
            original
                .environment()
                .contains(&"XDG_CACHE_HOME=relative/cache".into())
        );
        assert!(original.environment().contains(&"XDG_CONFIG_HOME=".into()));
        assert!(
            original
                .environment()
                .contains(&"TRACK_CACHE_DIR=relative/cache-override".into())
        );
        assert!(
            original
                .environment()
                .windows(2)
                .all(|pair| pair[0] < pair[1])
        );
        let mut reordered = input.clone();
        reordered.reverse();
        assert!(original.matches_observation(Path::new("/supplied/cwd"), &reordered));
        for key in [
            "TRACK_NEW",
            "XDG_UNKNOWN",
            "TRACK_CACHE_DIR",
            "XDG_CONFIG_HOME",
        ] {
            let mut changed = input.clone();
            changed.retain(|(k, _)| k != key);
            assert!(!original.matches_observation(Path::new("/supplied/cwd"), &changed));
        }
        let mut changed = input.clone();
        changed[1].1 = "different".into();
        assert!(!original.matches_observation(Path::new("/supplied/cwd"), &changed));
        assert!(!original.matches_observation(Path::new("/different"), &input));
        assert_eq!(
            format!("{original:?}"),
            "ResolutionContextRequest { values: [redacted] }"
        );
    }
    #[test]
    fn rejects_ambiguous_or_unrepresentable_relevant_inputs() {
        for entries in [
            vec![],
            vec![("HOME", "")],
            vec![("HOME", "x"), ("HOME", "x")],
            vec![("HOME", "x"), ("TRACK_X", "a\nb")],
            vec![("HOME", "x"), ("XDG_A=B", "x")],
            vec![("HOME", "x"), ("TRACK_\t", "x")],
        ] {
            assert!(capture(&entries).is_err());
        }
        for (key, value) in [
            (OsString::from_vec(b"TRACK_\xff".to_vec()), "v".into()),
            ("XDG_UNKNOWN".into(), OsString::from_vec(vec![255])),
        ] {
            let mut entries = env(&[("HOME", "x")]);
            entries.push((key, value));
            assert!(ResolutionContextRequest::capture(Path::new("/cwd"), &entries).is_err());
        }
        for cwd in [
            "relative",
            "",
            "/cwd/../else",
            "/cwd/./else",
            "/cwd//else",
            "/cwd/",
            "//cwd",
            "/cwd\n",
        ] {
            assert!(
                ResolutionContextRequest::capture(Path::new(cwd), &env(&[("HOME", "x")])).is_err()
            );
        }
        assert!(
            ResolutionContextRequest::capture(Path::new("/"), &env(&[("HOME", "relative-home")]))
                .is_ok()
        );
    }
    #[test]
    fn pwd_is_exact_but_separate_from_go_environment_entries() {
        let absent = capture(&[("HOME", "x")]).unwrap();
        let empty = capture(&[("HOME", "x"), ("PWD", "")]).unwrap();
        let lexical = capture(&[("HOME", "x"), ("PWD", "/lexical/alias")]).unwrap();
        assert_eq!(absent.pwd(), None);
        assert_eq!(empty.pwd(), Some(""));
        assert_eq!(lexical.pwd(), Some("/lexical/alias"));
        assert_eq!(absent.environment(), lexical.environment());
        assert_ne!(absent, empty);
        assert_ne!(empty, lexical);
        assert!(!lexical.matches_observation(
            Path::new("/supplied/not-required-to-exist"),
            &env(&[("HOME", "x"), ("PWD", "/different")])
        ));
        assert!(capture(&[("HOME", "x"), ("PWD", "a"), ("PWD", "a")]).is_err());
        assert!(capture(&[("HOME", "x"), ("PWD", "a\nb")]).is_err());
        assert!(capture(&[("HOME", "x"), ("PWD", &"x".repeat(MAX_PATH_BYTES))]).is_ok());
        assert!(capture(&[("HOME", "x"), ("PWD", &"x".repeat(MAX_PATH_BYTES + 1))]).is_err());
        let input = vec![
            ("HOME".into(), "x".into()),
            ("PWD".into(), OsString::from_vec(vec![255])),
        ];
        assert!(ResolutionContextRequest::capture(Path::new("/cwd"), &input).is_err());
    }
    #[test]
    fn exact_entry_byte_and_path_bounds() {
        let mut entries = env(&[("HOME", "x")]);
        for i in 0..255 {
            entries.push((format!("TRACK_{i}").into(), "".into()));
        }
        assert!(ResolutionContextRequest::capture(Path::new("/cwd"), &entries).is_ok());
        entries.push(("XDG_OVERFLOW".into(), "".into()));
        assert!(ResolutionContextRequest::capture(Path::new("/cwd"), &entries).is_err());
        // HOME=x + LF + TRACK_X= + payload is exactly 64 KiB.
        let payload = "x".repeat(MAX_JOINED_BYTES - "HOME=x\nTRACK_X=".len());
        assert!(capture(&[("HOME", "x"), ("TRACK_X", &payload)]).is_ok());
        assert!(capture(&[("HOME", "x"), ("TRACK_X", &(payload + "x"))]).is_err());
        for key in ["HOME", "XDG_CACHE_HOME", "XDG_CONFIG_HOME"] {
            let value = "x".repeat(MAX_PATH_BYTES);
            let mut input = env(&[("HOME", "x")]);
            if key == "HOME" {
                input.clear();
            }
            input.push((key.into(), value.clone().into()));
            assert!(ResolutionContextRequest::capture(Path::new("/cwd"), &input).is_ok());
            input.last_mut().unwrap().1 = (value + "x").into();
            assert!(ResolutionContextRequest::capture(Path::new("/cwd"), &input).is_err());
        }
        let cwd = format!("/{}", "x".repeat(MAX_PATH_BYTES - 1));
        assert!(ResolutionContextRequest::capture(Path::new(&cwd), &env(&[("HOME", "x")])).is_ok());
        assert!(
            ResolutionContextRequest::capture(Path::new(&(cwd + "x")), &env(&[("HOME", "x")]))
                .is_err()
        );
    }
}
