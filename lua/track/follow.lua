-- Follow a track link under the cursor.
-- Markdown action links are handled client-side; [[...]] links are resolved by the LSP.

local client = require("track.client")

local M = {}

local function split_heading(target)
   local i = target:find("#", 1, true)
   if not i then
      return target, nil, 0
   end
   local rest = target:sub(i)
   local level = 0
   while level < #rest and rest:sub(level + 1, level + 1) == "#" do
      level = level + 1
   end
   local heading = vim.trim(rest:sub(level + 1))
   if heading == "" then
      return target, nil, 0
   end
   return vim.trim(target:sub(1, i - 1)), heading, level
end

local function wiki_link_at_cursor(line, col)
   local search_from = 1
   while true do
      local start_pos, end_pos, inner = line:find("%[%[([^%[%]]+)%]%]", search_from)
      if not start_pos then
         return nil
      end
      if col >= start_pos and col <= end_pos then
         local target = inner:match("^([^|]+)|") or inner
         local key, heading, level = split_heading(vim.trim(target))
         return {
            key = key,
            heading = heading,
            heading_level = level,
         }
      end
      search_from = end_pos + 1
   end
end

local function heading_text(line)
   local hashes, text = line:match("^(#+)%s+(.-)%s*$")
   if not hashes then
      return nil, nil
   end
   text = text:gsub("%s+#+%s*$", "")
   return #hashes, vim.trim(text)
end

local function jump_to_heading(heading, level)
   if not heading or level == 0 then
      return
   end
   local lines = vim.api.nvim_buf_get_lines(0, 0, -1, false)
   for i, line in ipairs(lines) do
      local hlevel, text = heading_text(line)
      if hlevel == level and text == heading then
         pcall(vim.api.nvim_win_set_cursor, 0, { i, 0 })
         return
      end
   end
end

local function open_path(path, win, heading, level)
   if vim.api.nvim_win_is_valid(win) then
      pcall(vim.api.nvim_set_current_win, win)
   end
   local ok, err = pcall(vim.cmd, "keepalt edit " .. vim.fn.fnameescape(path))
   if not ok then
      vim.notify("track: failed to open " .. path .. ": " .. tostring(err), vim.log.levels.ERROR)
      return false
   end
   jump_to_heading(heading, level)
   return true
end

local function follow_wiki_link(win)
   local line = vim.api.nvim_get_current_line()
   local col = vim.fn.col(".")
   local link = wiki_link_at_cursor(line, col)
   if not link then
      return false
   end
   local data, err = client.run_json({ "resolve", "--term", link.key })
   if not data then
      vim.notify("track: " .. tostring(err), vim.log.levels.ERROR)
      return true
   end
   if not data.found or not data.path then
      vim.notify("track: unresolved link " .. link.key, vim.log.levels.WARN)
      return true
   end
   open_path(data.path, win, link.heading, link.heading_level)
   return true
end

local function definition_params(buf, win)
   local row_col = vim.api.nvim_win_get_cursor(win)
   return {
      textDocument = { uri = vim.uri_from_bufnr(buf) },
      position = {
         line = row_col[1] - 1,
         character = row_col[2],
      },
   }
end

local function location_uri(location)
   return location and (location.uri or location.targetUri)
end

local function location_range(location)
   return location and (location.range or location.targetSelectionRange or location.targetRange)
end

local function jump_to_location(location, win)
   local uri = location_uri(location)
   local range = location_range(location)
   if not uri or not range or not range.start then
      return
   end

   if vim.api.nvim_win_is_valid(win) then
      pcall(vim.api.nvim_set_current_win, win)
   end

   local path = vim.uri_to_fname(tostring(uri))
   local ok, err = pcall(vim.cmd, "keepalt edit " .. vim.fn.fnameescape(path))
   if not ok then
      vim.notify("track: failed to open " .. path .. ": " .. tostring(err), vim.log.levels.ERROR)
      return
   end

   pcall(vim.api.nvim_win_set_cursor, 0, { range.start.line + 1, range.start.character })
end

local function follow_definition()
   local buf = vim.api.nvim_get_current_buf()
   local win = vim.api.nvim_get_current_win()
   local client = require("track.lsp").client(buf, "textDocument/definition")
   if not client then
      vim.notify("track: LSP definition is not ready for this buffer", vim.log.levels.INFO)
      return
   end
   client:request("textDocument/definition", definition_params(buf, win), function(err, result)
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
         jump_to_location(location, win)
      end)
   end, buf)
end

function M.follow()
   if require("track.action").run_markdown_link_at_cursor() then
      return
   end
   if follow_wiki_link(vim.api.nvim_get_current_win()) then
      return
   end
   follow_definition()
end

return M
