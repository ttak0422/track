-- track.nvim vault identity (read-only).
--
-- Reports which vault the CLI considers selected; the CLI is the source of
-- truth, so this shells out to `track vault current` / `track vault list`
-- instead of reimplementing the selection precedence (flag > TRACK_VAULT >
-- default_vault > vault_dir > $HOME/track, ADR 0061 / agent-workflows.md).
-- There is deliberately no switch command here: the active vault is chosen
-- explicitly (machine config or TRACK_VAULT), never inferred from the
-- current buffer's directory.
--
-- Both calls are synchronous (one CLI spawn). An heirline/lualine segment
-- should cache the value (e.g. refresh on BufEnter) rather than calling
-- per redraw:
--
--   local name = require("track.vault").current_name()
--   return name ~= "" and ("[" .. name .. "]") or "[local]"

local client = require("track.client")

local M = {}

-- current returns the `track vault current` payload ({name, path, source}),
-- or nil plus an error message when the CLI cannot answer.
function M.current()
   return client.run_json({ "vault", "current" })
end

-- current_name returns the selected vault's registry name for display, or ""
-- when the vault is unregistered or the CLI cannot answer. It never throws:
-- a prompt title must degrade to the bare title, not an error.
function M.current_name()
   local ok, data = pcall(client.run_json, { "vault", "current" })
   if not ok or type(data) ~= "table" then
      return ""
   end
   if type(data.name) == "string" then
      return data.name
   end
   return ""
end

-- list returns the `track vault list` payload ({active, vaults}), or nil
-- plus an error message when the CLI cannot answer.
function M.list()
   return client.run_json({ "vault", "list" })
end

return M
