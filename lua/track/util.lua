-- Shared helpers for track.nvim.

local M = {}

local uv = vim.uv or vim.loop

-- registry_paths caches the registered vault directories from `track vault list`, fetched once per
-- session: the registry lives in the machine config, which does not change under a running editor.
local registry_paths

local function registered_vaults()
   if registry_paths then
      return registry_paths
   end
   local ok, data = pcall(require("track.client").run_json, { "vault", "list" })
   if not (ok and type(data) == "table" and type(data.vaults) == "table") then
      -- Do not cache a failure: the CLI may simply not be on $PATH yet (a plugin manager still
      -- building it). Caching {} here would disable per-vault LSP and cross-vault follow for the
      -- rest of the session, silently and unrecoverably.
      return {}
   end
   local paths = {}
   for _, row in ipairs(data.vaults) do
      if type(row.path) == "string" and row.path ~= "" then
         table.insert(paths, row.path)
      end
   end
   registry_paths = paths
   return registry_paths
end

-- vault_of returns the vault directory buf belongs to: the configured active vault or any
-- registered one. A buffer is a vault note when it sits directly under <vault>/note/ or /journal/.
-- Both the per-vault LSP (one client per vault, rooted at its own directory) and the web follow
-- publisher (which must name the vault its cursor is in) resolve a buffer's vault through this, so
-- they can never disagree about which vault a buffer belongs to.
function M.vault_of(buf)
   local name = vim.api.nvim_buf_get_name(buf)
   if name == "" then
      return nil
   end
   local path = uv.fs_realpath(name) or vim.fn.fnamemodify(name, ":p")
   path = vim.fn.fnamemodify(path, ":p")
   local candidates = { require("track.config").options.vault_dir }
   vim.list_extend(candidates, registered_vaults())
   for _, dir in ipairs(candidates) do
      local vault = uv.fs_realpath(dir) or vim.fn.fnamemodify(dir, ":p")
      vault = vim.fn.fnamemodify(vault, ":p")
      if path:sub(1, #vault) == vault then
         local rel = path:sub(#vault + 1)
         local sub = rel:match("^([^/]+)/")
         if sub == "note" or sub == "journal" then
            return vault
         end
      end
   end
   return nil
end

-- open_scratch renders `text` in a throwaway scratch buffer named `name`.
function M.open_scratch(name, filetype, text)
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

return M
