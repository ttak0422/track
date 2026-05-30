-- track.nvim configuration.
-- The vault must be explicit: either set TRACK_VAULT or pass setup({ vault_dir = ... }).

local M = {}

local function default_vault()
   local env = vim.env.TRACK_VAULT
   if env and env ~= "" then
      return env
   end
   return nil
end

M.defaults = {
   -- Binary name used when falling back to $PATH lookup.
   bin = "track",
   -- Vault directory; auto-link highlighting only attaches to files here.
   vault_dir = default_vault(),
   -- Note file extensions (without dot).
   extensions = { "md" },
   -- Autocommand group name.
   augroup = "track",
   -- Highlight group applied to auto-links.
   hl_group = "TrackLink",
   -- Debounce for re-highlighting, in milliseconds.
   debounce_ms = 150,
}

M.options = vim.deepcopy(M.defaults)

function M.setup(opts)
   M.options = vim.tbl_deep_extend("force", M.options, opts or {})
   if not M.options.vault_dir or M.options.vault_dir == "" then
      error("track: vault_dir is required. Set TRACK_VAULT or call require('track').setup({ vault_dir = ... }).")
   end
   vim.env.TRACK_VAULT = M.options.vault_dir
   return M.options
end

return M
