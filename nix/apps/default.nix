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
  cfg = {
    plugins = [
      # cmp-nvim-lsp must load before track so track's setup can detect it and advertise completion.
      pkgs.vimPlugins.cmp-nvim-lsp
      pkgs.vimPlugins.nvim-cmp
      pkgs.vimPlugins.plenary-nvim
      pkgs.vimPlugins.telescope-nvim
      {
        plugin = self'.packages.track;
        config = readLua ./nvim/track.lua;
      }
    ];
  };
  nvim = with pkgs; wrapNeovimUnstable neovim-unwrapped (neovimUtils.makeNeovimConfig cfg);
  launcher = pkgs.writeShellApplication {
    name = "track-test-nvim";
    # The vault defaults to $HOME/track (see nvim/track.lua); TRACK_VAULT or config.yml override it.
    # VIMINIT/GVIMINIT are cleared so the launcher ignores any ambient init and stays self-contained.
    text = ''
      unset VIMINIT
      unset GVIMINIT
      exec ${nvim}/bin/nvim "$@"
    '';
  };
in
{
  test-nvim = {
    type = "app";
    program = "${launcher}/bin/track-test-nvim";
  };
}
