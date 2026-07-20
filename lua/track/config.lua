-- track.nvim configuration.
-- The vault is resolved from config.yml or setup({ vault_dir = ... }); when neither is set it defaults
-- to $HOME/track (ADR 0015). TRACK_VAULT remains a test/one-off override.

local M = {}

local function expand_home(path)
   if path == "~" then
      return vim.env.HOME
   end
   if type(path) == "string" and path:sub(1, 2) == "~/" then
      return vim.env.HOME .. path:sub(2)
   end
   return path
end

local function config_path()
   if vim.env.TRACK_CONFIG and vim.env.TRACK_CONFIG ~= "" then
      return expand_home(vim.env.TRACK_CONFIG)
   end

   local home = vim.env.HOME
   if vim.loop.os_uname().sysname == "Darwin" then
      return home .. "/Library/Application Support/track/config.yml"
   end

   local xdg = vim.env.XDG_CONFIG_HOME
   if xdg and xdg ~= "" then
      return xdg .. "/track/config.yml"
   end
   return home .. "/.config/track/config.yml"
end

local function parse_config_file()
   local path = config_path()
   local ok, lines = pcall(vim.fn.readfile, path)
   if not ok then
      return {}
   end

   local parsed = {}
   for _, line in ipairs(lines) do
      local key, value = line:match("^%s*([%w_]+)%s*:%s*(.-)%s*$")
      if key and value and value ~= "" then
         value = value:gsub("%s+#.*$", "")
         value = value:gsub('^"(.*)"$', "%1")
         value = value:gsub("^'(.*)'$", "%1")
         parsed[key] = expand_home(value)
      end
   end
   return parsed
end

local file_config = parse_config_file()

local function default_vault()
   local env = vim.env.TRACK_VAULT
   if env and env ~= "" then
      return env
   end
   if file_config.vault_dir and file_config.vault_dir ~= "" then
      return file_config.vault_dir
   end
   -- Fall back to $HOME/track (ADR 0015) so the plugin works without any explicit configuration.
   local home = vim.env.HOME
   if home and home ~= "" then
      return home .. "/track"
   end
   return nil
end

local function default_cache_dir()
   local env = vim.env.TRACK_CACHE_DIR
   if env and env ~= "" then
      return env
   end
   return file_config.cache_dir or (vim.fn.stdpath("cache") .. "/track")
end

M.defaults = {
   -- Binary name used when falling back to $PATH lookup.
   bin = "track",
   -- LSP binary name used when falling back to $PATH lookup.
   lsp_bin = "track-lsp",
   -- Vault directory; link highlighting only attaches to files here.
   vault_dir = default_vault(),
   -- Rebuildable SQLite cache directory. Kept outside the vault so synced folders do not sync DB locks.
   cache_dir = default_cache_dir(),
   -- Address used by `:Track web` when no address argument is supplied.
   web_addr = "127.0.0.1:8765",
   -- Note file extensions (without dot).
   extensions = { "md" },
   -- Autocommand group name.
   augroup = "track",
   -- Highlight group applied to resolved [[...]] links. Unresolved links are left unstyled and
   -- surfaced by the server's "unresolved-link" diagnostic instead.
   hl_group = "TrackLink",
   -- Conceal the [[ ]] brackets (and the "target|" of display aliases), showing just the link text.
   -- Sets conceallevel locally in windows showing vault buffers; the cursor line stays raw for editing.
   conceal = true,
   -- Raising conceallevel for links also lets Neovim's bundled treesitter markdown query conceal
   -- code-fence delimiters (```lua disappears). When true, track reveals those fences again.
   reveal_code_fences = true,
   -- Task-notation decoration (docs/help/tasks.md), mirroring the web workspace's inline rendering:
   -- on a task line, bracket tokens conceal down to chips ("[#A]" → "#A", "[sched:d]" → "▷ d",
   -- "[due:d]" → "! d", "[done:d]" → "✓ d", cookies lose their brackets) and a done-family line is
   -- struck through (TrackTaskDone). Concealing follows the conceal option above; the cursor line
   -- stays raw. task_chars lists the state markers, task_done_chars the done-family subset — they
   -- mirror the engine's default task_states, so align them with the vault's config.yml when
   -- customized. Set task_chars = "" to disable the decoration entirely.
   task_chars = " /?x-",
   task_done_chars = "x-",
   -- Debounce for re-highlighting, in milliseconds.
   debounce_ms = 150,
   -- ![[...]] includes (ADR 0031) render as virtual lines below the directive. include_max_lines
   -- caps how many show before an "… (+N lines)" tail (0 = no cap); :TrackIncludeToggle expands.
   include_max_lines = 15,
   include_prefix = "│ ",
   include_hl = "TrackInclude",
   -- Highlight groups for rendered babel results (status header, stdout, stderr).
   babel_hl_header = "TrackBabelHeader",
   babel_hl_result = "TrackBabelResult",
   babel_hl_error = "TrackBabelError",
   -- Optional hook run once per track note buffer right after it attaches, as on_attach(buf). The place
   -- for buffer-local keymaps (e.g. mapping a references key to require("track.backlinks").show). Left
   -- unset by default; a nil table value is omitted, so it is documented here rather than assigned.
   -- on_attach = nil,
}

M.options = vim.deepcopy(M.defaults)

function M.setup(opts)
   M.options = vim.tbl_deep_extend("force", M.options, opts or {})
   if not M.options.vault_dir or M.options.vault_dir == "" then
      error("track: vault_dir is required in config.yml or require('track').setup({ vault_dir = ... }).")
   end
   vim.env.TRACK_VAULT = M.options.vault_dir
   vim.env.TRACK_CACHE_DIR = M.options.cache_dir
   return M.options
end

return M
