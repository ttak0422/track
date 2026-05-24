-- Follow the auto-link under the cursor.

local autolink = require("track.autolink")
local client = require("track.client")

local M = {}

local function open(path)
   vim.cmd.edit(vim.fn.fnameescape(path))
end

-- follow opens the note linked under the cursor. It uses the highlighter's
-- match cache first (so it follows exactly what is underlined), then falls back
-- to resolving the word under the cursor through the CLI.
function M.follow()
   local buf = vim.api.nvim_get_current_buf()
   local pos = vim.api.nvim_win_get_cursor(0) -- {row (1-based), col (0-based byte)}
   local m = autolink.match_at(buf, pos[1] - 1, pos[2])
   if m and m.path then
      open(m.path)
      return
   end

   local word = vim.fn.expand("<cword>")
   if word == "" then
      vim.notify("track: no link under cursor", vim.log.levels.INFO)
      return
   end
   local data, err = client.run_json({ "resolve", "--term", word })
   if not data then
      vim.notify("track: " .. tostring(err), vim.log.levels.ERROR)
      return
   end
   if data.found then
      open(data.path)
   else
      vim.notify("track: no note for '" .. word .. "'", vim.log.levels.INFO)
   end
end

return M
