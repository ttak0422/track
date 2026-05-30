-- Create new notes (keywords) from three entry points: the word under the cursor, a visual selection, or a prompt/command argument.

local client = require("track.client")
local autolink = require("track.autolink")

local M = {}

-- create makes a new note titled `title`, then runs a full reindex so other notes' inbound links to the new title are picked up, reloads the keyword cache, and opens the note.
function M.create(title)
   title = vim.trim(title or "")
   if title == "" then
      vim.notify("track: empty title", vim.log.levels.WARN)
      return
   end

   local data, err = client.run_json({ "new", "--title", title, "--id", tostring(os.time()) })
   if not data then
      vim.notify("track: " .. tostring(err), vim.log.levels.ERROR)
      return
   end

   local _, rerr = client.run_json({ "reindex", "--full" })
   if rerr then
      vim.notify("track: reindex failed: " .. tostring(rerr), vim.log.levels.WARN)
   end
   autolink.reload()

   vim.cmd.edit(vim.fn.fnameescape(data.path))
end

-- prompt asks for a title (prefilled with `default`, e.g. the word under the cursor) and creates a note from it.
function M.prompt(default)
   vim.ui.input({ prompt = "Note title: ", default = default or "" }, function(input)
      if input and vim.trim(input) ~= "" then
         M.create(input)
      end
   end)
end

-- from_visual creates a note titled with the last visual selection.
function M.from_visual()
   local s = vim.fn.getpos("'<")
   local e = vim.fn.getpos("'>")
   local last = vim.api.nvim_buf_get_lines(0, e[2] - 1, e[2], false)[1] or ""
   local end_col = math.min(e[3], #last)
   local lines = vim.api.nvim_buf_get_text(0, s[2] - 1, s[3] - 1, e[2] - 1, end_col, {})
   M.create(table.concat(lines, " "))
end

return M
