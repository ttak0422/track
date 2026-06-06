-- Follow a track link under the cursor.
-- Markdown track:// action links are handled client-side; [[...]] links are resolved by the LSP.

local M = {}

function M.follow()
   if require("track.action").run_markdown_link_at_cursor() then
      return
   end
   vim.lsp.buf.definition()
end

return M
