use std::{env, fs, path::PathBuf, process::Command};

fn git(root: &std::path::Path, args: &[&str]) -> Option<String> {
    let output = Command::new("git")
        .arg("-C")
        .arg(root)
        .args(args)
        .output()
        .ok()?;
    output
        .status
        .success()
        .then(|| String::from_utf8_lossy(&output.stdout).trim().to_owned())
}

fn build_identity(root: &std::path::Path) {
    let repository = root.parent().and_then(|p| p.parent()).unwrap();
    let is_repository = git(root, &["rev-parse", "--show-toplevel"])
        .is_some_and(|p| std::fs::canonicalize(p).ok() == std::fs::canonicalize(repository).ok());
    let revision = is_repository
        .then(|| git(root, &["rev-parse", "HEAD"]))
        .flatten()
        .filter(|s| s.len() == 40 && s.bytes().all(|b| b.is_ascii_hexdigit()));
    let state = if revision.is_some() {
        git(root, &["status", "--porcelain", "--untracked-files=no"])
            .map(|s| if s.is_empty() { "clean" } else { "modified" })
    } else {
        None
    };
    println!(
        "cargo:rustc-env=TRACK_BUILD_REVISION={}",
        revision.as_deref().unwrap_or("unknown")
    );
    println!(
        "cargo:rustc-env=TRACK_BUILD_SOURCE_STATE={}",
        state.unwrap_or("unknown")
    );
    println!(
        "cargo:rustc-env=TRACK_BUILD_TARGET={}",
        env::var("TARGET").unwrap()
    );
    if !is_repository {
        return;
    }
    // Watch tracked inputs and index/ref changes, including linked worktrees.
    if let Some(files) = git(repository, &["ls-files", "-z"]) {
        for file in files.split('\0').filter(|s| !s.is_empty()) {
            println!("cargo:rerun-if-changed={}", repository.join(file).display());
        }
    }
    let mut paths = vec![
        "HEAD".to_owned(),
        "index".to_owned(),
        "packed-refs".to_owned(),
    ];
    if let Some(reference) = git(root, &["symbolic-ref", "-q", "HEAD"]) {
        paths.push(reference);
    }
    for path in paths {
        if let Some(path) = git(
            root,
            &["rev-parse", "--path-format=absolute", "--git-path", &path],
        ) {
            // Missing watched files make Cargo rebuild on every invocation.
            // Watch their nearest existing parent so ref creation still invalidates.
            let mut watched = PathBuf::from(path);
            while !watched.exists() && watched.pop() {}
            println!("cargo:rerun-if-changed={}", watched.display());
        }
    }
}

fn main() {
    let root = PathBuf::from(env::var_os("CARGO_MANIFEST_DIR").unwrap());
    build_identity(&root);
    let expected = root.join("patches/native-termination.patch");
    let applied = root.join(".build-deps/gpui/.track-native-termination.patch");
    println!("cargo:rerun-if-changed={}", expected.display());
    println!("cargo:rerun-if-changed={}", applied.display());
    let expected = fs::read(expected).expect("missing pinned native termination patch");
    let applied = fs::read(applied).expect("run experiments/gpui/prepare.sh before building");
    assert!(
        expected == applied,
        "prepared GPUI patch is stale; rerun experiments/gpui/prepare.sh"
    );
}
