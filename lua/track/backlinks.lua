-- Backlinks for the current note.

local M = {}

-- request fetches the backlinks of the current buffer's note via the LSP server and hands the raw
-- result list to cb. Both the quickfix and Telescope presentations build on this so they stay in sync.
function M.request(cb)
   local buf = vim.api.nvim_get_current_buf()
   vim.lsp.buf_request(buf, "track/backlinks", { textDocument = { uri = vim.uri_from_bufnr(buf) } }, function(err, result)
      if err then
         vim.notify("track: " .. tostring(err.message or err), vim.log.levels.ERROR)
         return
      end
      cb(result or {})
   end)
end

local function backlink_title(backlink)
   return backlink.title or ("#" .. tostring(backlink.note_id or "?"))
end

-- qf_item maps a backlink to a quickfix entry. The note title goes in `module`, which the quickfix
-- window shows in place of the filename, so the opaque epoch path (note/<epoch>.md) stays hidden while
-- `filename` still drives the jump.
local function qf_item(backlink)
   local range = backlink.range or {}
   local start = range.start or {}
   return {
      filename = backlink.path,
      module = backlink_title(backlink),
      lnum = (start.line or 0) + 1,
      col = (start.character or 0) + 1,
      text = backlink.preview or "",
   }
end

function M.show()
   M.request(function(result)
      local items = {}
      for _, backlink in ipairs(result) do
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
