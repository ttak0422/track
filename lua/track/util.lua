-- Shared helpers for track.nvim.

local M = {}

-- open_scratch renders `text` in a throwaway scratch buffer named `name`.
function M.open_scratch(name, filetype, text)
   local existing = vim.fn.bufnr(name)
   if existing ~= -1 then
      vim.api.nvim_buf_delete(existing, { force = true })
   end

   local buf = vim.api.nvim_create_buf(true, true)
   vim.api.nvim_buf_set_name(buf, name)
   vim.api.nvim_set_option_value("bufhidden", "wipe", { buf = buf })
   vim.api.nvim_set_option_value("swapfile", false, { buf = buf })
   vim.api.nvim_set_option_value("filetype", filetype, { buf = buf })
   vim.api.nvim_buf_set_lines(buf, 0, -1, false, vim.split(text, "\n", { plain = true }))
   vim.api.nvim_set_current_buf(buf)
end

return M
