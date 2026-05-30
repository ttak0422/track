{
  self',
  pkgs,
  ...
}:

let
  mkNeovimApp =
    cfg:
    let
      nvim = with pkgs; wrapNeovimUnstable neovim-unwrapped (neovimUtils.makeNeovimConfig cfg);
      launcher = pkgs.writeShellApplication {
        name = "track-test-nvim";
        text = ''
          if [ -z "''${TRACK_VAULT:-}" ]; then
            export TRACK_VAULT="''${TMPDIR:-/tmp}/track-test-vault"
          else
            export TRACK_VAULT
          fi
          unset VIMINIT
          unset GVIMINIT
          mkdir -p "$TRACK_VAULT"
          exec ${nvim}/bin/nvim "$@"
        '';
      };
    in
    {
      type = "app";
      program = "${launcher}/bin/track-test-nvim";
    };
  readLua = path: ''
    lua << EOF
    ${builtins.readFile path}
    EOF
  '';
in
{
  test-nvim = mkNeovimApp {
    plugins = [
      # cmp-nvim-lsp must load before track so track's setup can detect it and advertise completion.
      pkgs.vimPlugins.cmp-nvim-lsp
      pkgs.vimPlugins.nvim-cmp
      {
        plugin = self'.packages.track;
        config = readLua ./nvim/track.lua;
      }
    ];
  };
}
