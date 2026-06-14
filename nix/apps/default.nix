{
  self',
  pkgs,
  ...
}:

let
  readLua = path: ''
    lua << EOF
    ${builtins.readFile path}
    EOF
  '';
  mkNeovimApp =
    {
      name,
      config,
      testVault ? false,
    }:
    let
      cfg = {
        plugins = [
          # cmp-nvim-lsp must load before track so track's setup can detect it and advertise completion.
          pkgs.vimPlugins.cmp-nvim-lsp
          pkgs.vimPlugins.nvim-cmp
          pkgs.vimPlugins.plenary-nvim
          pkgs.vimPlugins.telescope-nvim
          {
            plugin = self'.packages.track;
            config = readLua config;
          }
        ];
      };
      nvim = with pkgs; wrapNeovimUnstable neovim-unwrapped (neovimUtils.makeNeovimConfig cfg);
      launcher = pkgs.writeShellApplication {
        inherit name;
        text = ''
          ${pkgs.lib.optionalString testVault ''
            # Test launcher: isolate in a throwaway vault unless one is provided.
            if [ -z "''${TRACK_VAULT:-}" ]; then
              export TRACK_VAULT="''${TMPDIR:-/tmp}/track-test-vault"
            else
              export TRACK_VAULT
            fi
            mkdir -p "$TRACK_VAULT"
          ''}
          unset VIMINIT
          unset GVIMINIT
          exec ${nvim}/bin/nvim "$@"
        '';
      };
    in
    {
      type = "app";
      program = "${launcher}/bin/${name}";
    };
in
{
  # Real-use launcher: resolves the vault from the default config file (config.yml),
  # so it works against the vault you actually use.
  nvim = mkNeovimApp {
    name = "track-nvim";
    config = ./nvim/app.lua;
  };

  # Test launcher: forces an isolated temp vault and is used by the E2E scripts.
  test-nvim = mkNeovimApp {
    name = "track-test-nvim";
    config = ./nvim/track.lua;
    testVault = true;
  };
}
