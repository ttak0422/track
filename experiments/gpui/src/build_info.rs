//! Identity captured at compile time, never read from the checkout at runtime.
fn format(version: &str, revision: &str, state: &str, target: &str) -> String {
    format!("Track Native {version}\nCommit: {revision}\nTracked source: {state}\nTarget: {target}")
}

pub fn display() -> String {
    format(
        env!("CARGO_PKG_VERSION"),
        env!("TRACK_BUILD_REVISION"),
        env!("TRACK_BUILD_SOURCE_STATE"),
        env!("TRACK_BUILD_TARGET"),
    )
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn preserves_full_revision_and_modified_state() {
        let revision = "0123456789012345678901234567890123456789";
        let text = format("0.1.0", revision, "modified", "aarch64-apple-darwin");
        assert!(text.contains(revision));
        assert!(text.contains("Tracked source: modified"));
        assert!(text.contains("aarch64-apple-darwin"));
    }

    #[test]
    fn missing_metadata_is_explicit() {
        assert!(format("0.1.0", "unknown", "unknown", "target").contains("Commit: unknown"));
        assert!(display().starts_with("Track Native "));
    }
}
