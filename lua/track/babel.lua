-- Babel: run the fenced source block under the cursor and show its result as virtual lines.
-- The note body is never modified; results render below the closing fence (and persist in the sidecar),
-- so even multi-line output just adds more virtual lines without touching the buffer text.

local config = require("track.config")
local client = require("track.client")

local M = {}

local ns = vim.api.nvim_create_namespace("track_babel_results")
local visibility_ns = vim.api.nvim_create_namespace("track_babel_source_visibility")
local augroup = vim.api.nvim_create_augroup("track_babel_restore", { clear = true })
local visibility_augroup = vim.api.nvim_create_augroup("track_babel_source_visibility", { clear = true })

-- lines_of splits captured output into display lines, dropping the trailing newline.
local function lines_of(s)
   s = (s or ""):gsub("\n+$", "")
   if s == "" then
      return {}
   end
   return vim.split(s, "\n", { plain = true })
end

-- clear_at removes any result already rendered at end_line so a re-run replaces it in place,
-- leaving results on other blocks untouched.
local function clear_at(buf, end_line)
   local marks = vim.api.nvim_buf_get_extmarks(buf, ns, { end_line, 0 }, { end_line, -1 }, {})
   for _, m in ipairs(marks) do
      vim.api.nvim_buf_del_extmark(buf, ns, m[1])
   end
end

-- render shows the run result as virtual lines just below the block's closing fence.
local function render(buf, end_line, data)
   if not end_line then
      return
   end
   clear_at(buf, end_line)
   local vlines = {
      { { ("=> %s (exit %d)"):format(data.status or "?", data.exit_code or 0), config.options.babel_hl_header } },
   }
   for _, l in ipairs(lines_of(data.stdout)) do
      vlines[#vlines + 1] = { { l, config.options.babel_hl_result } }
   end
   for _, l in ipairs(lines_of(data.stderr)) do
      vlines[#vlines + 1] = { { l, config.options.babel_hl_error } }
   end
   vim.api.nvim_buf_set_extmark(buf, ns, end_line, 0, {
      virt_lines = vlines,
      virt_lines_above = false,
   })
end

local function render_all(buf, blocks)
   vim.api.nvim_buf_clear_namespace(buf, ns, 0, -1)
   for _, block in ipairs(blocks or {}) do
      render(buf, block.end_line, block)
   end
end

local function is_fence(line)
   return vim.trim(line):sub(1, 3) == "```"
end

local function first_header_value(info, key)
   local tokens = vim.split(vim.trim(info or ""), "%s+")
   local active
   for _, token in ipairs(tokens) do
      if vim.startswith(token, ":") then
         active = token:sub(2)
      elseif active == key then
         return token
      end
   end
end

local function parse_visible_lines(spec, line_count)
   spec = vim.trim(spec or "")
   if spec == "" then
      return nil
   end
   local visible = {}
   for part in spec:gmatch("[^,]+") do
      part = vim.trim(part)
      local first, last = part:match("^(%d+)%-(%d+)$")
      if first then
         first = tonumber(first)
         last = tonumber(last)
      else
         first = tonumber(part:match("^(%d+)$"))
         last = first
      end
      if first and last and first <= last then
         for line = first, last do
            if line >= 1 and line <= line_count then
               visible[line] = true
            end
         end
      end
   end
   return next(visible) and visible or nil
end

local function current_cursor_row(buf)
   local win = vim.api.nvim_get_current_win()
   if vim.api.nvim_win_get_buf(win) ~= buf then
      return nil
   end
   return vim.api.nvim_win_get_cursor(win)[1] - 1
end

local function ensure_conceallevel(buf)
   for _, win in ipairs(vim.fn.win_findbuf(buf)) do
      local level = vim.api.nvim_get_option_value("conceallevel", { scope = "local", win = win })
      if level == 0 then
         vim.api.nvim_set_option_value("conceallevel", 2, { scope = "local", win = win })
      end
   end
end

-- apply_visibility conceals source block body lines outside :visible-lines.
-- It never changes the buffer text or execution body; the cursor row is revealed so hidden code stays editable.
function M.apply_visibility(buf)
   buf = buf or vim.api.nvim_get_current_buf()
   if not vim.api.nvim_buf_is_valid(buf) then
      return
   end
   vim.api.nvim_buf_clear_namespace(buf, visibility_ns, 0, -1)

   local lines = vim.api.nvim_buf_get_lines(buf, 0, -1, false)
   local cursor_row = current_cursor_row(buf)
   local any_hidden = false
   local i = 1
   while i <= #lines do
      if is_fence(lines[i]) then
         local info = vim.trim(lines[i]):sub(4)
         local lang = vim.split(vim.trim(info), "%s+")[1] or ""
         local start_line = i
         local j = i + 1
         while j <= #lines and not is_fence(lines[j]) do
            j = j + 1
         end
         if j <= #lines and lang ~= "" then
            local body_count = j - start_line - 1
            local visible = parse_visible_lines(first_header_value(info, "visible-lines"), body_count)
            if visible then
               for body_line = 1, body_count do
                  local row = start_line + body_line - 1
                  if not visible[body_line] and row ~= cursor_row then
                     vim.api.nvim_buf_set_extmark(buf, visibility_ns, row, 0, {
                        end_row = row + 1,
                        end_col = 0,
                        conceal_lines = "",
                        priority = 130,
                     })
                     any_hidden = true
                  end
               end
            end
         end
         i = j + 1
      else
         i = i + 1
      end
   end

   if any_hidden then
      ensure_conceallevel(buf)
   end
end

local function current_body(buf)
   local body = table.concat(vim.api.nvim_buf_get_lines(buf, 0, -1, false), "\n")
   if vim.bo[buf].endofline then
      body = body .. "\n"
   end
   return body
end

local function run(buf, path, row, confirmed)
   local args = { "babel", "exec", "--path", path, "--line", tostring(row), "--body-stdin" }
   if confirmed then
      args[#args + 1] = "--yes"
   end
   local data, err = client.run_json(args, current_body(buf))
   if not data then
      -- A block with :eval query is refused until confirmed; ask, then re-run with --yes.
      if not confirmed and tostring(err):find("eval query") then
         vim.ui.select({ "yes", "no" }, { prompt = "track: block has :eval query. Run it?" }, function(choice)
            if choice == "yes" then
               run(buf, path, row, true)
            end
         end)
         return
      end
      vim.notify("track: " .. tostring(err), vim.log.levels.ERROR)
      return
   end
   render(buf, data.end_line, data)
end

-- exec runs the source block under the cursor and renders its result below the block.
function M.exec()
   local buf = vim.api.nvim_get_current_buf()
   local path = vim.api.nvim_buf_get_name(buf)
   if path == "" then
      vim.notify("track: buffer has no file", vim.log.levels.WARN)
      return
   end
   local row = vim.api.nvim_win_get_cursor(0)[1] - 1
   run(buf, path, row, false)
end

-- restore renders stored run results from the note sidecar without evaluating any block.
function M.restore(opts)
   opts = opts or {}
   local buf = opts.buf or vim.api.nvim_get_current_buf()
   if not vim.api.nvim_buf_is_valid(buf) then
      return
   end
   M.apply_visibility(buf)
   local path = vim.api.nvim_buf_get_name(buf)
   if path == "" then
      if not opts.silent then
         vim.notify("track: buffer has no file", vim.log.levels.WARN)
      end
      return
   end
   local data, err = client.run_json({ "babel", "restore", "--path", path })
   if not data then
      if not opts.silent then
         vim.notify("track: " .. tostring(err), vim.log.levels.ERROR)
      end
      return
   end
   render_all(buf, data.blocks)
end

-- clear removes all rendered babel results from the current buffer.
function M.clear()
   vim.api.nvim_buf_clear_namespace(vim.api.nvim_get_current_buf(), ns, 0, -1)
end

function M.setup()
   vim.api.nvim_set_hl(0, config.options.babel_hl_header, { default = true, link = "Comment" })
   vim.api.nvim_set_hl(0, config.options.babel_hl_result, { default = true, link = "String" })
   vim.api.nvim_set_hl(0, config.options.babel_hl_error, { default = true, link = "ErrorMsg" })

   vim.api.nvim_create_autocmd({ "BufReadPost", "BufWritePost" }, {
      group = augroup,
      pattern = "*.md",
      callback = function(args)
         vim.schedule(function()
            M.restore({ buf = args.buf, silent = true })
         end)
      end,
      desc = "Restore stored track Babel results",
   })
   vim.api.nvim_create_autocmd({ "BufReadPost", "BufWritePost", "TextChanged", "TextChangedI", "CursorMoved", "CursorMovedI", "BufEnter", "WinEnter" }, {
      group = visibility_augroup,
      pattern = "*.md",
      callback = function(args)
         vim.schedule(function()
            M.apply_visibility(args.buf)
         end)
      end,
      desc = "Apply track Babel source visibility",
   })
end

return M
