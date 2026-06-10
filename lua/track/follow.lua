-- Follow a track link under the cursor.
-- Markdown action links are handled client-side; [[...]] links are resolved by the LSP.

local M = {}

local function wiki_link_at_cursor(line, col)
   local search_from = 1
   while true do
      local start_pos, end_pos = line:find("%[%[[^%[%]]+%]%]", search_from)
      if not start_pos then
         return false
      end
      if col >= start_pos and col <= end_pos then
         return true
      end
      search_from = end_pos + 1
   end
end

function M.follow()
   if require("track.action").run_markdown_link_at_cursor() then
      return
   end
   if not require("track.lsp").client(0, "textDocument/definition") then
      vim.notify("track: LSP definition is not ready for this buffer", vim.log.levels.INFO)
      return
   end
   vim.lsp.buf.definition({
      on_list = function(items)
         local entries = items.items or {}
         if #entries == 0 then
            vim.notify("track: no link target found", vim.log.levels.INFO)
            return
         end
         if #entries > 1 then
            vim.fn.setqflist({}, " ", { title = "track link targets", items = entries })
            vim.cmd.copen()
            return
         end
         local entry = entries[1]
         if not entry.filename then
            return
         end
         vim.cmd("keepalt edit " .. vim.fn.fnameescape(entry.filename))
         if entry.lnum then
            pcall(vim.api.nvim_win_set_cursor, 0, { entry.lnum, entry.col or 0 })
         end
      end,
   })
end

function M.smart_action()
   local line = vim.api.nvim_get_current_line()
   local col = vim.fn.col(".")
   if require("track.action").markdown_link_at_cursor(line, col) or wiki_link_at_cursor(line, col) then
      return "<cmd>Track follow<cr>"
   end
   return "<CR>"
end

return M
