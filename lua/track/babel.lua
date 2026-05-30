-- Babel: run the fenced source block under the cursor and show its result as virtual lines.
-- The note body is never modified; results render below the closing fence (and persist in the sidecar),
-- so even multi-line output just adds more virtual lines without touching the buffer text.

local config = require("track.config")
local client = require("track.client")

local M = {}

local ns = vim.api.nvim_create_namespace("track_babel_results")
local augroup = vim.api.nvim_create_augroup("track_babel_restore", { clear = true })

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

local function run(buf, path, row, confirmed)
   local args = { "babel", "exec", "--path", path, "--line", tostring(row) }
   if confirmed then
      args[#args + 1] = "--yes"
   end
   local data, err = client.run_json(args)
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
end

return M
