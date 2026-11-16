{
  system ? builtins.currentSystem,
  package ? "gpui",
}:
let
  lock = builtins.fromJSON (builtins.readFile ../../flake.lock);
  nixpkgs = builtins.fetchTree lock.nodes.${lock.nodes.root.inputs.nixpkgs}.locked;
  pkgs = import nixpkgs.outPath { inherit system; };
  sources = {
    gpui = pkgs.applyPatches {
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
    };
    xattr = pkgs.applyPatches {
      name = "track-xattr-0.2.3-enodata";
      src = pkgs.fetchurl {
        name = "xattr-0.2.3.tar.gz";
        url = "https://static.crates.io/crates/xattr/xattr-0.2.3.crate";
        hash = "sha256-bRUmu+Wq616waIX02Ye836XiMYcFXem4P+ABVqgh+rw=";
      };
      patches = [ ./patches/xattr-enodata.patch ];
      postPatch = ''
        cp ${./patches/xattr-enodata.patch} .track-xattr-enodata.patch
      '';
    };
  };
in
sources.${package}
