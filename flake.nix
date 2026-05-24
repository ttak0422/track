{
  description = "track - dummy tracker (Go CLI + Neovim/Teal plugin)";

  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs/nixos-unstable";
    flake-parts.url = "github:hercules-ci/flake-parts";
    git-hooks = {
      url = "github:cachix/git-hooks.nix";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs =
    inputs@{ flake-parts, ... }:
    flake-parts.lib.mkFlake { inherit inputs; } {
      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];
      perSystem =
        {
          self',
          system,
          pkgs,
          lib,
          ...
        }:
        let
          inherit (lib) fileset;

          goFiles = fileset.unions [
            ./go.mod
            (fileset.maybeMissing ./go.sum)
            (fileset.fileFilter (file: file.hasExt "go") ./.)
          ];

          track-cli = pkgs.buildGoModule {
            pname = "track";
            version = "0.1.0";
            src = fileset.toSource {
              root = ./.;
              fileset = goFiles;
            };
            vendorHash = "sha256-REKtx4+UjxLUD+8yxSotjx8CCKuchP/l/BKqstZcogA=";
            subPackages = [ "cmd/track" ];
          };

          track = pkgs.vimUtils.buildVimPlugin {
            pname = "track";
            version = "0.1.0";
            src = fileset.toSource {
              root = ./.;
              fileset = ./lua;
            };
            postPatch = ''
              substituteInPlace lua/track/client.lua \
                --replace-fail \
                  'local bundled_binary_path = nil' \
                  'local bundled_binary_path = "${track-cli}/bin/track"'
            '';
          };
        in
        {
          checks = {
            pre-commit-check = inputs.git-hooks.lib.${system}.run {
              src = ./.;
              hooks = {
                deadnix.enable = true;
                nixfmt.enable = true;
                statix.enable = true;
                gofmt.enable = true;
              };
            };
          };

          apps = import ./nix/apps {
            inherit self' pkgs;
          };

          packages = {
            default = track;
            inherit track track-cli;
          };

          devShells.default = pkgs.mkShell {
            inherit (self'.checks.pre-commit-check) shellHook;
            packages = [
              pkgs.go
            ];
          };
        };
    };
}
