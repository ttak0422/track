-- Daily journal notes.
-- Each day maps to a stable yyyyMMdd note id, so opening the same day is idempotent.

local client = require("track.client")

local M = {}

-- open opens (creating if needed) the journal note `offset` days from today (0 = today, -1 = yesterday, 1 = tomorrow).
function M.open(offset, template)
   local args = { "journal", "--offset", tostring(offset or 0) }
   template = vim.trim(template or "")
   if template ~= "" then
      vim.list_extend(args, { "--template", template })
   end
   local data, err = client.run_json(args)
   if not data then
      vim.notify("track: " .. tostring(err), vim.log.levels.ERROR)
      return
   end
   -- A new journal note adds its date as a keyword; the LSP server picks it up on the next request.
   vim.cmd.edit(vim.fn.fnameescape(data.path))
end

return M
