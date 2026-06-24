-- Notes worked on the day represented by the current daily journal.

local client = require("track.client")

local M = {}

local function note_title(note)
   return note.title or ("#" .. tostring(note.note_id or "?"))
end

-- date_from_path returns YYYY-MM-DD for a daily journal path
-- (.../journal/yyyyMMdd.md). Month/year summary journals deliberately do not
-- match.
function M.date_from_path(path)
   path = (path or ""):gsub("\\", "/")
   local compact = path:match("/journal/(%d%d%d%d%d%d%d%d)%.md$")
   if not compact then
      return nil
   end
   return compact:sub(1, 4) .. "-" .. compact:sub(5, 6) .. "-" .. compact:sub(7, 8)
end

-- request fetches the notes active on the current daily journal's date.
function M.request(cb)
   local path = vim.api.nvim_buf_get_name(vim.api.nvim_get_current_buf())
   local date = M.date_from_path(path)
   if not date then
      vim.notify("track: on_this_day requires a daily journal buffer", vim.log.levels.WARN)
      return
   end

   local data, err = client.run_json({ "agenda", "--date", date })
   if not data then
      vim.notify("track: " .. tostring(err), vim.log.levels.ERROR)
      return
   end
   cb(data.notes or {}, data.date or date)
end

local function qf_item(note)
   return {
      filename = note.path,
      module = note_title(note),
      lnum = 1,
      col = 1,
      text = "",
   }
end

-- show presents On this day through quickfix when Telescope is unavailable.
function M.show()
   M.request(function(notes, date)
      local items = {}
      for _, note in ipairs(notes) do
         items[#items + 1] = qf_item(note)
      end
      if #items == 0 then
         vim.notify("track: no notes worked on " .. date, vim.log.levels.INFO)
         return
      end
      vim.fn.setqflist({}, " ", { title = "track on this day: " .. date, items = items })
      vim.cmd.copen()
   end)
end

return M
