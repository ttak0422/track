-- track.nvim - Neovim frontend for the `track` CLI (dummy scaffold).

local M = {
   config = {
      -- Binary name used when falling back to $PATH lookup.
      bin = "track",
   },
}

local bundled_binary_path = nil
local cached_binary

-- Resolve the track binary. Prefers a binary bundled next to the plugin
-- by Nix, then a binary next to the plugin (lua/track/init.lua ->
-- <plugin>/bin/track), then a local `result` symlink, then $PATH.
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

   local bin = M.config.bin
   if vim.fn.executable(bin) == 1 then
      cached_binary = bin
      return cached_binary
   end

   error("track binary not found. Install track with Nix or add it to $PATH.")
end

local function run(args)
   local cmd = { find_binary() }
   for _, item in ipairs(args) do
      table.insert(cmd, item)
   end
   return vim.fn.system(cmd)
end

-- Render `text` in a throwaway scratch buffer named `name`.
local function open_scratch(name, filetype, text)
   local existing = vim.fn.bufnr(name)
   if existing ~= -1 then
      vim.api.nvim_buf_delete(existing, { force = true })
   end

   local buf = vim.api.nvim_create_buf(true, true)
   vim.api.nvim_buf_set_name(buf, name)
   vim.api.nvim_set_option_value("bufhidden", "wipe", { buf = buf })
   vim.api.nvim_set_option_value("swapfile", false, { buf = buf })
   vim.api.nvim_set_option_value("filetype", filetype, { buf = buf })
   vim.api.nvim_buf_set_lines(buf, 0, -1, false, vim.split(text, "\n", { plain = true }))
   vim.api.nvim_set_current_buf(buf)
end

function M.dump()
   return run({ "dump" })
end

function M.setup(opts)
   M.config = vim.tbl_deep_extend("force", M.config, opts or {})

   vim.api.nvim_create_user_command("TrackDump", function()
      open_scratch("track://dump", "json", M.dump())
   end, { desc = "Open a diagnostic dump of track state" })
end

return M
