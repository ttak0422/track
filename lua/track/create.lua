-- Open or create notes (keywords) from three entry points: the word under the cursor, a visual selection, or a prompt/command argument.

local client = require("track.client")

local M = {}

-- create opens the note titled `title`, creating it only when none exists. Titles are link keywords,
-- so resolving an existing title instead of minting a duplicate keeps them unique. A freshly created
-- note triggers a full reindex so other notes' inbound links to the new title are picked up; opening
-- an existing note needs none. The LSP server reloads the keyword dictionary per request, so no
-- client-side cache refresh is needed.
function M.create(title, template)
   title = vim.trim(title or "")
   if title == "" then
      vim.notify("track: empty title", vim.log.levels.WARN)
      return
   end

   local args = { "open", "--title", title }
   template = vim.trim(template or "")
   if template ~= "" then
      vim.list_extend(args, { "--template", template })
   end
   local data, err = client.run_json(args)
   if not data then
      vim.notify("track: " .. tostring(err), vim.log.levels.ERROR)
      return
   end

   if data.created then
      local _, rerr = client.run_json({ "reindex", "--full" })
      if rerr then
         vim.notify("track: reindex failed: " .. tostring(rerr), vim.log.levels.WARN)
      end
   end

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
