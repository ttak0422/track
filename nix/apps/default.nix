{
  self',
  pkgs,
  ...
}:

let
  mkNeovimApp = cfg: {
    type = "app";
    program = "${
      with pkgs; wrapNeovimUnstable neovim-unwrapped (neovimUtils.makeNeovimConfig cfg)
    }/bin/nvim";
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
      {
        plugin = self'.packages.track;
        config = readLua ./nvim/track.lua;
      }
    ];
  };
}
