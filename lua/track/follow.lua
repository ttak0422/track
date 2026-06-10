-- Follow a track link under the cursor.
-- Markdown action links are handled client-side; [[...]] links are resolved by the LSP.

local M = {}

local function definition_params(buf)
   local row_col = vim.api.nvim_win_get_cursor(0)
   return {
      textDocument = { uri = vim.uri_from_bufnr(buf) },
      position = {
         line = row_col[1] - 1,
         character = row_col[2],
      },
   }
end

local function jump_to_location(location)
   local ok = pcall(vim.lsp.util.jump_to_location, location, "utf-8", true)
   if not ok then
      pcall(vim.lsp.util.jump_to_location, location, "utf-8")
   end
end

local function follow_definition()
   local buf = vim.api.nvim_get_current_buf()
   local client = require("track.lsp").client(buf, "textDocument/definition")
   if not client then
      vim.notify("track: LSP definition is not ready for this buffer", vim.log.levels.INFO)
      return
   end
   client:request("textDocument/definition", definition_params(buf), function(err, result)
      if err then
         vim.schedule(function()
            vim.notify("track: " .. tostring(err.message or err), vim.log.levels.ERROR)
         end)
         return
      end
      local location = result
      if type(result) == "table" and result[1] then
         location = result[1]
      end
      if not location then
         return
      end
      vim.schedule(function()
         jump_to_location(location)
      end)
   end, buf)
end

function M.follow()
   if require("track.action").run_markdown_link_at_cursor() then
      return
   end
   follow_definition()
end

return M
