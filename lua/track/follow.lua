-- Follow a track link under the cursor.
-- Markdown action links are handled client-side; [[...]] links are resolved by the LSP.

local M = {}
local pending_context

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

local function current_context()
   local win = vim.api.nvim_get_current_win()
   local buf = vim.api.nvim_win_get_buf(win)
   local cursor = vim.api.nvim_win_get_cursor(win)
   local line = vim.api.nvim_buf_get_lines(buf, cursor[1] - 1, cursor[1], false)[1] or ""
   return {
      win = win,
      buf = buf,
      row = cursor[1],
      col = cursor[2] + 1,
      line = line,
   }
end

local function valid_context(ctx)
   return ctx ~= nil and vim.api.nvim_buf_is_valid(ctx.buf) and vim.api.nvim_win_is_valid(ctx.win)
end

local function take_context()
   local ctx = pending_context
   pending_context = nil
   if valid_context(ctx) then
      return ctx
   end
   return current_context()
end

local function definition_params(ctx)
   return {
      textDocument = { uri = vim.uri_from_bufnr(ctx.buf) },
      position = {
         line = ctx.row - 1,
         character = ctx.col - 1,
      },
   }
end

local function location_uri(location)
   return location and (location.uri or location.targetUri)
end

local function location_range(location)
   return location and (location.range or location.targetSelectionRange or location.targetRange)
end

local function location_to_item(location)
   local uri = location_uri(location)
   local range = location_range(location)
   if not uri or not range or not range.start then
      return nil
   end
   return {
      filename = vim.uri_to_fname(tostring(uri)),
      lnum = range.start.line + 1,
      col = range.start.character,
   }
end

local function open_item(ctx, item)
   if not item or not item.filename then
      return
   end
   local function edit()
      vim.cmd("keepalt edit " .. vim.fn.fnameescape(item.filename))
      if item.lnum then
         pcall(vim.api.nvim_win_set_cursor, 0, { item.lnum, item.col or 0 })
      end
   end
   if vim.api.nvim_win_is_valid(ctx.win) then
      vim.api.nvim_win_call(ctx.win, edit)
      pcall(vim.api.nvim_set_current_win, ctx.win)
   else
      edit()
   end
end

local function open_locations(ctx, result)
   local locations = result or {}
   if locations.uri or locations.targetUri then
      locations = { locations }
   end

   local items = {}
   for _, location in ipairs(locations) do
      local item = location_to_item(location)
      if item then
         items[#items + 1] = item
      end
   end

   if #items == 0 then
      vim.notify("track: no link target found", vim.log.levels.INFO)
      return
   end
   if #items > 1 then
      vim.fn.setqflist({}, " ", { title = "track link targets", items = items })
      vim.cmd.copen()
      return
   end
   open_item(ctx, items[1])
end

function M.follow(ctx)
   ctx = ctx or take_context()
   if require("track.action").run_markdown_link_at_cursor(ctx) then
      return
   end
   local client = require("track.lsp").client(ctx.buf, "textDocument/definition")
   if not client then
      vim.notify("track: LSP definition is not ready for this buffer", vim.log.levels.INFO)
      return
   end
   client:request("textDocument/definition", definition_params(ctx), function(err, result)
      if err then
         vim.schedule(function()
            vim.notify("track: " .. tostring(err.message or err), vim.log.levels.ERROR)
         end)
         return
      end
      vim.schedule(function()
         open_locations(ctx, result)
      end)
   end, ctx.buf)
end

function M.smart_action()
   local ctx = current_context()
   if require("track.action").markdown_link_at_cursor(ctx.line, ctx.col) or wiki_link_at_cursor(ctx.line, ctx.col) then
      pending_context = nil
      M.follow(ctx)
      return ""
   end
   pending_context = nil
   return "<CR>"
end

return M
