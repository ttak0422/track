-- Daily journal notes.
-- Each day maps to a stable yyyyMMdd note id, so opening the same day is idempotent.

local client = require("track.client")
local autolink = require("track.autolink")

local M = {}

-- open opens (creating if needed) the journal note `offset` days from today (0 = today, -1 = yesterday, 1 = tomorrow).
function M.open(offset)
   local data, err = client.run_json({ "journal", "--offset", tostring(offset or 0) })
   if not data then
      vim.notify("track: " .. tostring(err), vim.log.levels.ERROR)
      return
   end
   if data.created then
      -- a new journal note adds its date as a keyword
      autolink.reload()
   end
   vim.cmd.edit(vim.fn.fnameescape(data.path))
end

return M
