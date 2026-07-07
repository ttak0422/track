-- track.nvim include rendering (ADR 0031).
-- The LSP server owns include parsing and extraction (the track.includes command); this module only
-- draws the returned lines as read-only virtual lines below each ![[...]] directive — the same
-- org-transclusion shape babel results use. The namespace is separate from the link highlights on
-- purpose: cursor moves repaint those every time, and re-stamping virt_lines on every cursor move
-- would flicker, so includes repaint only when a track.includes response arrives.

local config = require("track.config")

local M = {}

local ns = vim.api.nvim_create_namespace("track_include")
-- cache[buf] = the last track.includes result, so toggling expansion repaints without a round trip.
local cache = {}
-- expanded["buf:row"] = true once the user expanded a truncated include past include_max_lines.
local expanded = {}

local function vline(text, hl)
   return { { text, hl } }
end

local function render(buf)
   if not vim.api.nvim_buf_is_valid(buf) then
      return
   end
   vim.api.nvim_buf_clear_namespace(buf, ns, 0, -1)
   local prefix = config.options.include_prefix
   local max = config.options.include_max_lines
   local last_row = vim.api.nvim_buf_line_count(buf) - 1
   for _, inc in ipairs(cache[buf] or {}) do
      local row = inc.range.start.line
      -- The buffer may have been edited since the response; a stale out-of-range row just waits for
      -- the in-flight refresh instead of erroring.
      if row <= last_row then
         local vlines = {}
         if inc.error then
            vlines[#vlines + 1] = vline(prefix .. "⚠ " .. inc.error, "TrackIncludeError")
         else
            local lines = inc.lines or {}
            local limit = #lines
            if max > 0 and #lines > max and not expanded[buf .. ":" .. row] then
               limit = max
            end
            for i = 1, limit do
               vlines[#vlines + 1] = vline(prefix .. lines[i], config.options.include_hl)
            end
            if limit < #lines then
               vlines[#vlines + 1] =
                  vline(prefix .. ("… (+%d lines — :Track include_toggle)"):format(#lines - limit), "TrackIncludeMore")
            end
         end
         for _, bad in ipairs(inc.bad_options or {}) do
            vlines[#vlines + 1] = vline(prefix .. "⚠ unknown option: " .. bad, "TrackIncludeError")
         end
         if #vlines > 0 then
            vim.api.nvim_buf_set_extmark(buf, ns, row, 0, {
               virt_lines = vlines,
               virt_lines_above = false,
            })
         end
      end
   end
end

-- refresh asks the server for the buffer's include directives and repaints. It rides the same
-- debounce as link highlighting (track.lsp calls it from its refresh), so edits cost one extra
-- request, not a new autocmd pipeline.
function M.refresh(buf)
   if not vim.api.nvim_buf_is_valid(buf) then
      return
   end
   local client = require("track.lsp").client(buf)
   if not client then
      return
   end
   client:request("workspace/executeCommand", {
      command = "track.includes",
      arguments = { { uri = vim.uri_from_bufnr(buf) } },
   }, function(err, result)
      if err or not vim.api.nvim_buf_is_valid(buf) then
         return
      end
      cache[buf] = result or {}
      render(buf)
   end, buf)
end

-- toggle expands or re-truncates the include on the cursor line, from cache — no server round trip.
function M.toggle()
   local buf = vim.api.nvim_get_current_buf()
   local row = vim.api.nvim_win_get_cursor(0)[1] - 1
   for _, inc in ipairs(cache[buf] or {}) do
      if inc.range.start.line == row then
         local key = buf .. ":" .. row
         expanded[key] = not expanded[key] or nil
         render(buf)
         return
      end
   end
   vim.notify("track: no include on this line", vim.log.levels.INFO)
end

function M.setup()
   vim.api.nvim_set_hl(0, "TrackInclude", { default = true, link = "Comment" })
   vim.api.nvim_set_hl(0, "TrackIncludeMore", { default = true, link = "NonText" })
   vim.api.nvim_set_hl(0, "TrackIncludeError", { default = true, link = "DiagnosticWarn" })

   local group = vim.api.nvim_create_augroup(config.options.augroup .. "_include", { clear = true })
   -- Saving any vault note may change what other open notes embed from it, so repaint every buffer
   -- that currently shows includes. The including buffer's own edits already refresh via track.lsp.
   vim.api.nvim_create_autocmd("BufWritePost", {
      group = group,
      pattern = "*.md",
      callback = function()
         for buf in pairs(cache) do
            if vim.api.nvim_buf_is_valid(buf) then
               M.refresh(buf)
            end
         end
      end,
   })
   vim.api.nvim_create_autocmd("BufWipeout", {
      group = group,
      callback = function(ev)
         cache[ev.buf] = nil
         for key in pairs(expanded) do
            if key:match("^" .. ev.buf .. ":") then
               expanded[key] = nil
            end
         end
      end,
   })
end

return M
