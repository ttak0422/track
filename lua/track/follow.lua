-- Follow the [[...]] link under the cursor.
-- The Go LSP server owns link resolution; follow just asks it to jump to the definition.

local M = {}

function M.follow()
   vim.lsp.buf.definition()
end

return M
