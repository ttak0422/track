-- Hatena-style auto-link highlighting. Registered note titles/aliases that
-- appear in a vault buffer are underlined and made followable. Only the
-- visible window range is scanned, on a debounce, to stay cheap on large notes.

local config = require("track.config")
local keywords = require("track.keywords")
local matcher = require("track.matcher")

local M = {}

local uv = vim.uv or vim.loop
local ns = vim.api.nvim_create_namespace("track_autolink")

-- matches[buf][row] = { {s_col, e_col, term, note_id, path}, ... } for the
-- currently rendered (visible) lines, used by follow.
local matches = {}
local timers = {}
local attached = {}
local built = nil

local function get_matcher()
   if not built then
      built = matcher.build(keywords.all())
   end
   return built
end

-- reload rebuilds the keyword dictionary and re-highlights the current buffer.
-- Call after creating or reindexing notes.
function M.reload()
   keywords.invalidate()
   built = nil
   local buf = vim.api.nvim_get_current_buf()
   if attached[buf] then
      M.refresh(buf)
   end
end

local function footmatter_range(buf)
   local fm = config.options.footmatter
   local total = vim.api.nvim_buf_line_count(buf)
   local start = math.max(0, total - 200)
   local lines = vim.api.nvim_buf_get_lines(buf, start, total, false)
   local close_i, open_i
   for i = #lines, 1, -1 do
      local t = vim.trim(lines[i])
      if not close_i then
         if t == fm.close then
            close_i = start + i - 1
         end
      elseif t == fm.open then
         open_i = start + i - 1
         break
      end
   end
   if open_i and close_i and close_i > open_i then
      return open_i, close_i
   end
   return nil, nil
end

-- fenced_rows returns a set (0-based row -> true) of lines that are fence
-- delimiters or inside a fenced code block, for rows up to `bot`.
local function fenced_rows(buf, bot)
   local lines = vim.api.nvim_buf_get_lines(buf, 0, bot + 1, false)
   local rows = {}
   local in_fence = false
   for i = 1, #lines do
      local t = vim.trim(lines[i])
      if t:sub(1, 3) == "```" then
         in_fence = not in_fence
         rows[i - 1] = true
      elseif in_fence then
         rows[i - 1] = true
      end
   end
   return rows
end

-- refresh re-highlights the visible range of buf in the current window.
function M.refresh(buf)
   local win = vim.api.nvim_get_current_win()
   if vim.api.nvim_win_get_buf(win) ~= buf then
      return
   end

   local top = vim.fn.line("w0") - 1
   local bot = vim.fn.line("w$") - 1
   if bot < top then
      return
   end

   vim.api.nvim_buf_clear_namespace(buf, ns, 0, -1)
   matches[buf] = {}

   local m = get_matcher()
   local fm_open, fm_close = footmatter_range(buf)
   local fences = fenced_rows(buf, bot)
   local lines = vim.api.nvim_buf_get_lines(buf, top, bot + 1, false)

   for offset, text in ipairs(lines) do
      local row = top + offset - 1
      local in_footmatter = fm_open ~= nil and row >= fm_open and row <= fm_close
      if not in_footmatter and not fences[row] then
         local hits = m:line(text)
         if #hits > 0 then
            matches[buf][row] = hits
            for _, h in ipairs(hits) do
               vim.api.nvim_buf_set_extmark(buf, ns, row, h.s_col, {
                  end_col = h.e_col,
                  hl_group = config.options.hl_group,
                  priority = 100,
               })
            end
         end
      end
   end
end

local function schedule(buf)
   local timer = timers[buf]
   if not timer then
      timer = uv.new_timer()
      timers[buf] = timer
   end
   timer:stop()
   timer:start(
      config.options.debounce_ms,
      0,
      vim.schedule_wrap(function()
         if vim.api.nvim_buf_is_valid(buf) then
            M.refresh(buf)
         end
      end)
   )
end

-- match_at returns the auto-link match covering (row, col) in buf, or nil.
-- Columns are 0-based byte offsets.
function M.match_at(buf, row, col)
   local rows = matches[buf]
   if not rows then
      return nil
   end
   local hits = rows[row]
   if not hits then
      return nil
   end
   for _, h in ipairs(hits) do
      if col >= h.s_col and col < h.e_col then
         return h
      end
   end
   return nil
end

-- attach wires refresh autocmds to a buffer (idempotent).
function M.attach(buf)
   if attached[buf] then
      return
   end
   attached[buf] = true

   vim.keymap.set("n", "<CR>", function()
      require("track.follow").follow()
   end, { buffer = buf, desc = "track: follow link under cursor" })

   local group = vim.api.nvim_create_augroup(config.options.augroup .. "_buf_" .. buf, { clear = true })
   vim.api.nvim_create_autocmd({ "BufEnter", "TextChanged", "TextChangedI", "WinScrolled" }, {
      group = group,
      buffer = buf,
      callback = function()
         schedule(buf)
      end,
   })
   vim.api.nvim_create_autocmd("BufWipeout", {
      group = group,
      buffer = buf,
      callback = function()
         attached[buf] = nil
         matches[buf] = nil
         if timers[buf] then
            timers[buf]:stop()
            timers[buf]:close()
            timers[buf] = nil
         end
      end,
   })
   schedule(buf)
end

local function under_vault(buf)
   local name = vim.api.nvim_buf_get_name(buf)
   if name == "" then
      return false
   end
   local vault = vim.fn.fnamemodify(config.options.vault_dir, ":p")
   local path = vim.fn.fnamemodify(name, ":p")
   return path:sub(1, #vault) == vault
end

-- setup registers the highlight group and the FileType autocmd that attaches
-- auto-linking to vault buffers.
function M.setup()
   vim.api.nvim_set_hl(0, config.options.hl_group, { default = true, link = "Underlined" })
   local group = vim.api.nvim_create_augroup(config.options.augroup .. "_autolink", { clear = true })
   vim.api.nvim_create_autocmd("FileType", {
      group = group,
      pattern = "markdown",
      callback = function(ev)
         if under_vault(ev.buf) then
            M.attach(ev.buf)
         end
      end,
   })
end

return M
