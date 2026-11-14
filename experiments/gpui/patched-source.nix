{
  system ? builtins.currentSystem,
}:
let
  lock = builtins.fromJSON (builtins.readFile ../../flake.lock);
  nixpkgs = builtins.fetchTree lock.nodes.${lock.nodes.root.inputs.nixpkgs}.locked;
  pkgs = import nixpkgs.outPath { inherit system; };
in
pkgs.applyPatches {
  name = "track-gpui-0.2.2-native-termination";
  src = pkgs.fetchurl {
    name = "gpui-0.2.2.tar.gz";
    url = "https://static.crates.io/crates/gpui/gpui-0.2.2.crate";
    hash = "sha256-l5tFz6bscjtvQjMJFaGzdpuTDQKy1QX5aX+MpgK+5wc=";
  };
  patches = [ ./patches/native-termination.patch ];
  postPatch = ''
    cp ${./patches/native-termination.patch} .track-native-termination.patch
  '';
}
