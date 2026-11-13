use std::{env, fs, path::PathBuf};

fn main() {
    let root = PathBuf::from(env::var_os("CARGO_MANIFEST_DIR").unwrap());
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
