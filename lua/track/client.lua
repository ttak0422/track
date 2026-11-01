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

-- bin returns the resolved track CLI path for asynchronous callers.
function M.bin()
   return find_binary()
end

-- Capture the JSON contract separately from diagnostics. Waiting here preserves
-- existing synchronous callers; no automatic retry can repeat a committed write.
local function execute(args, input)
   local cmd = { find_binary() }
   for _, item in ipairs(args) do table.insert(cmd, item) end
   return vim.system(cmd, { text = true, stdin = input }):wait()
end

-- run returns stdout for raw-output callers such as Markdown export.
function M.run(args, input)
   return execute(args, input).stdout or ""
end

local function failure(result, message)
   local diagnostic = (result.stderr or ""):sub(1, 512):gsub("%s+$", "")
   local status = string.format(" (exit %s, signal %s)", result.code, result.signal)
   return "track: " .. message .. status .. (diagnostic ~= "" and ": " .. diagnostic or "")
end

-- Successful warnings remain diagnostics, without a repeated intrusive popup.
-- Partial data is represented in the decoded payload's read_state.
function M.run_json(args, input)
   local launched, result = pcall(execute, args, input)
   if not launched then return nil, "track: could not run CLI: " .. tostring(result):sub(1, 512) end
   local ok, decoded = pcall(vim.json.decode, result.stdout or "")
   if ok and type(decoded) == "table" and decoded.error then return nil, decoded.error end
   if result.code ~= 0 or result.signal ~= 0 then return nil, failure(result, "CLI failed") end
   if not ok then return nil, failure(result, "invalid JSON output") end
   return decoded
end

return M
