-- Outgoing links for the current note.

local M = {}

local function qf_item(source_path, link)
   local range = link.range or {}
   local start = range.start or {}
   local title = link.title or ("#" .. tostring(link.note_id or "?"))
   local preview = link.preview or ""
   return {
      filename = source_path,
      lnum = (start.line or 0) + 1,
      col = (start.character or 0) + 1,
      text = title .. "\t" .. preview,
   }
end

function M.show()
   local buf = vim.api.nvim_get_current_buf()
   local source_path = vim.api.nvim_buf_get_name(buf)
   vim.lsp.buf_request(buf, "track/outgoingLinks", { textDocument = { uri = vim.uri_from_bufnr(buf) } }, function(err, result)
      if err then
         vim.notify("track: " .. tostring(err.message or err), vim.log.levels.ERROR)
         return
      end
      local items = {}
      for _, link in ipairs(result or {}) do
         items[#items + 1] = qf_item(source_path, link)
      end
      if #items == 0 then
         vim.notify("track: no outgoing links", vim.log.levels.INFO)
         return
      end
      vim.fn.setqflist({}, " ", { title = "track links", items = items })
      vim.cmd.copen()
   end)
end

return M
