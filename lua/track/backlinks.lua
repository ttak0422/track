-- Backlinks for the current note.

local M = {}

local function qf_item(backlink)
   local range = backlink.range or {}
   local start = range.start or {}
   local title = backlink.title or ("#" .. tostring(backlink.note_id or "?"))
   local preview = backlink.preview or ""
   return {
      filename = backlink.path,
      lnum = (start.line or 0) + 1,
      col = (start.character or 0) + 1,
      text = title .. "\t" .. preview,
   }
end

function M.show()
   local buf = vim.api.nvim_get_current_buf()
   vim.lsp.buf_request(buf, "track/backlinks", { textDocument = { uri = vim.uri_from_bufnr(buf) } }, function(err, result)
      if err then
         vim.notify("track: " .. tostring(err.message or err), vim.log.levels.ERROR)
         return
      end
      local items = {}
      for _, backlink in ipairs(result or {}) do
         items[#items + 1] = qf_item(backlink)
      end
      if #items == 0 then
         vim.notify("track: no backlinks", vim.log.levels.INFO)
         return
      end
      vim.fn.setqflist({}, " ", { title = "track backlinks", items = items })
      vim.cmd.copen()
   end)
end

return M
