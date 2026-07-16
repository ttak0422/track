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
            # Builtin templates are embedded (builtin/*.template.md via go:embed), so the non-.go assets
            # must be part of the build source too.
            ./builtin
          ];

          # The React frontend source, excluding generated/installed directories.
          webFiles = fileset.difference ./web (
            fileset.unions [
              (fileset.maybeMissing ./web/node_modules)
              (fileset.maybeMissing ./web/dist)
            ]
          );

          # web-dist builds the Vite frontend; its output is the contents of web/dist, which the CLI
          # embeds (see internal/track/webui/embed.go).
          web-dist = pkgs.buildNpmPackage {
            pname = "track-web";
            version = "0.1.0";
            src = fileset.toSource {
              root = ./web;
              fileset = webFiles;
            };
            npmDepsHash = "sha256-XI12TL7V9EluVRJvG1SP2YJRF3jyfxXxePkVbid8nas=";
            installPhase = ''
              runHook preInstall
              cp -r dist $out
              runHook postInstall
            '';
          };

          track-cli = pkgs.buildGoModule {
            pname = "track";
            version = "0.1.0";
            src = fileset.toSource {
              root = ./.;
              fileset = goFiles;
            };
            vendorHash = "sha256-RVRG4u1UhQ04w1qGRii9Xl+P/ISUKIw3VhFjqd7DT+Y=";
            subPackages = [
              "cmd/track"
              "cmd/track-lsp"
            ];
            # Replace the committed placeholder dist with the real frontend build before the embed runs.
            preBuild = ''
              mkdir -p internal/track/webui/dist
              cp -rf ${web-dist}/. internal/track/webui/dist/
            '';
          };

          # Fetch tools are separate binaries in the same module (docs/spec/fetch.md): each converts
          # one external source into Canonical JSONL, packaged on its own so installing track never
          # pulls in fetchers (and vice versa). No frontend embed — they are pure data converters.
          track-fetch-rss = pkgs.buildGoModule {
            pname = "track-fetch-rss";
            version = "0.1.0";
            src = fileset.toSource {
              root = ./.;
              fileset = goFiles;
            };
            vendorHash = "sha256-RVRG4u1UhQ04w1qGRii9Xl+P/ISUKIw3VhFjqd7DT+Y=";
            subPackages = [ "cmd/track-fetch-rss" ];
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
              substituteInPlace lua/track/lsp.lua \
                --replace-fail \
                  'local bundled_lsp_binary_path = nil' \
                  'local bundled_lsp_binary_path = "${track-cli}/bin/track-lsp"'
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
            inherit track track-cli track-fetch-rss;
          };

          devShells.default = pkgs.mkShell {
            inherit (self'.checks.pre-commit-check) shellHook;
            packages = [
              pkgs.go
              pkgs.nodejs_22
            ];
          };
        };
    };
}
