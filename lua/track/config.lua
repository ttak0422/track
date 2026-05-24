-- track.nvim configuration. Defaults mirror the Go CLI's resolution so the
-- editor and engine agree on where the vault lives.

local M = {}

local function default_vault()
   local env = vim.env.TRACK_VAULT
   if env and env ~= "" then
      return env
   end
   local xdg = vim.env.XDG_DATA_HOME
   if xdg and xdg ~= "" then
      return xdg .. "/track"
   end
   return vim.fn.expand("~/.local/share/track")
end

M.defaults = {
   -- Binary name used when falling back to $PATH lookup.
   bin = "track",
   -- Vault directory; auto-link highlighting only attaches to files here.
   vault_dir = default_vault(),
   -- Note file extensions (without dot).
   extensions = { "md" },
   -- Footmatter delimiters; matched against the Go engine's defaults. Lines
   -- inside this block are excluded from auto-linking.
   footmatter = { open = "<!--track", close = "-->" },
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
   return M.options
end

return M
