-- track.nvim CLI client.
-- Resolves the `track` binary and shells out to it, decoding the JSON subcommands return.

local config = require("track.config")

local M = {}

-- Patched by Nix at build time to the bundled track binary's store path.
local bundled_binary_path = nil
local cached_binary

-- Resolve the track binary.
-- Prefers a binary bundled next to the plugin by Nix, then a binary next to the plugin (lua/track/client.lua -> <plugin>/bin/track), then a local `result` symlink, then $PATH.
local function find_binary()
   if cached_binary then
      return cached_binary
   end

   if bundled_binary_path ~= nil and vim.fn.executable(bundled_binary_path) == 1 then
      cached_binary = bundled_binary_path
      return cached_binary
   end

   local script_path = debug.getinfo(1, "S").source:sub(2)
   local plugin_root = vim.fn.fnamemodify(script_path, ":h:h:h")

   local candidates = {
      plugin_root .. "/bin/track",
      plugin_root .. "/result/bin/track",
   }
   for _, candidate in ipairs(candidates) do
      if vim.fn.executable(candidate) == 1 then
         cached_binary = candidate
         return cached_binary
      end
   end

   local bin = config.options.bin
   if vim.fn.executable(bin) == 1 then
      cached_binary = bin
      return cached_binary
   end

   error("track binary not found. Install track with Nix or add it to $PATH.")
end

-- run executes the CLI with args and returns raw stdout.
function M.run(args, input)
   local cmd = { find_binary() }
   for _, item in ipairs(args) do
      table.insert(cmd, item)
   end
   if input ~= nil then
      return vim.fn.system(cmd, input)
   end
   return vim.fn.system(cmd)
end

-- run_json executes the CLI and decodes its JSON.
-- Returns (table) on success, or (nil, errmsg) when stdout is not JSON or carries an {"error":...} payload.
function M.run_json(args, input)
   local out = M.run(args, input)
   local ok, decoded = pcall(vim.json.decode, out)
   if not ok then
      return nil, "track: invalid JSON output: " .. tostring(out)
   end
   if type(decoded) == "table" and decoded.error then
      return nil, decoded.error
   end
   return decoded
end

return M
