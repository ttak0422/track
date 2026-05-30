-- track.nvim LSP integration.
-- The Go LSP server owns link discovery; Lua starts the server and renders document links as underlined extmarks.

local config = require("track.config")

local M = {}

-- Patched by Nix at build time to the bundled track-lsp binary's store path.
local bundled_lsp_binary_path = nil
local cached_binary
local client_id

local uv = vim.uv or vim.loop
local ns = vim.api.nvim_create_namespace("track_lsp_links")
local timers = {}
local attached = {}
-- resolved_cache[buf] = set of "row:start:end" keys the server returned as resolved document links,
-- kept so cursor moves can repaint (toggle anti-conceal) without another LSP round trip.
local resolved_cache = {}

local function find_binary()
   if cached_binary then
      return cached_binary
   end

   if bundled_lsp_binary_path ~= nil and vim.fn.executable(bundled_lsp_binary_path) == 1 then
      cached_binary = bundled_lsp_binary_path
      return cached_binary
   end

   local script_path = debug.getinfo(1, "S").source:sub(2)
   local plugin_root = vim.fn.fnamemodify(script_path, ":h:h:h")
   local candidates = {
      plugin_root .. "/bin/track-lsp",
      plugin_root .. "/result/bin/track-lsp",
   }
   for _, candidate in ipairs(candidates) do
      if vim.fn.executable(candidate) == 1 then
         cached_binary = candidate
         return cached_binary
      end
   end

   local bin = config.options.lsp_bin
   if vim.fn.executable(bin) == 1 then
      cached_binary = bin
      return cached_binary
   end

   error("track-lsp binary not found. Install track with Nix or add track-lsp to $PATH.")
end

local function under_vault(buf)
   local name = vim.api.nvim_buf_get_name(buf)
   if name == "" then
      return false
   end
   local vault = uv.fs_realpath(config.options.vault_dir) or vim.fn.fnamemodify(config.options.vault_dir, ":p")
   local path = uv.fs_realpath(name) or vim.fn.fnamemodify(name, ":p")
   vault = vim.fn.fnamemodify(vault, ":p")
   path = vim.fn.fnamemodify(path, ":p")
   return path:sub(1, #vault) == vault
end

local function text_document_params(buf)
   return { uri = vim.uri_from_bufnr(buf) }
end

-- fenced_rows returns a set (0-based row -> true) of lines that are fence delimiters or inside a fenced code block, plus the buffer lines.
local function fenced_rows(buf)
   local lines = vim.api.nvim_buf_get_lines(buf, 0, -1, false)
   local rows = {}
   local in_fence = false
   for i, line in ipairs(lines) do
      if vim.trim(line):sub(1, 3) == "```" then
         in_fence = not in_fence
         rows[i - 1] = true
      elseif in_fence then
         rows[i - 1] = true
      end
   end
   return rows, lines
end

-- highlight_links underlines every [[...]] and, when conceal is on, hides the brackets (and the
-- "target|" of a display alias) so only the link text shows. The server reports document-link ranges
-- over the inner text (between the brackets); we mirror that span as the key so resolved vs. unresolved
-- can be told apart, and color each link accordingly.
local function highlight_links(buf, resolved, cursor)
   local conceal = config.options.conceal
   local fences, lines = fenced_rows(buf)
   for i, text in ipairs(lines) do
      local row = i - 1
      if not fences[row] then
         local init = 1
         while true do
            local s, e = text:find("%[%[[^%[%]]+%]%]", init)
            if not s then
               break
            end
            local open_start = s - 1 -- 0-based "[" of "[["
            local inner_start = s + 1 -- 0-based byte col just after "[["
            local inner_end = e - 2 -- 0-based exclusive end just before "]]"
            local hl = resolved[row .. ":" .. inner_start .. ":" .. inner_end]
                  and config.options.hl_group
               or config.options.hl_group_unresolved

            -- Reveal (skip conceal for) the link the cursor sits on, so its raw text can be edited.
            local revealed = cursor ~= nil
               and cursor.row == row
               and cursor.col >= open_start
               and cursor.col < e

            local hl_start = inner_start
            if conceal and not revealed then
               -- A non-empty display alias ([[target|display]]) shows only "display".
               local inner = text:sub(s + 2, e - 2)
               local pipe = inner:find("|", 1, true)
               if pipe and pipe < #inner then
                  hl_start = inner_start + pipe -- first byte after the "|"
               end
               -- Conceal the leading "[[" (plus "target|" when present) and the trailing "]]".
               vim.api.nvim_buf_set_extmark(buf, ns, row, open_start, {
                  end_col = hl_start,
                  conceal = "",
                  priority = 120,
               })
               vim.api.nvim_buf_set_extmark(buf, ns, row, inner_end, {
                  end_col = e,
                  conceal = "",
                  priority = 120,
               })
            end

            if inner_end > hl_start then
               vim.api.nvim_buf_set_extmark(buf, ns, row, hl_start, {
                  end_col = inner_end,
                  hl_group = hl,
                  priority = 120,
               })
            end
            init = e + 1
         end
      end
   end
end

-- apply_conceal_options enables concealment in every window currently showing buf.
local function apply_conceal_options(buf)
   if not config.options.conceal then
      return
   end
   for _, win in ipairs(vim.fn.win_findbuf(buf)) do
      vim.api.nvim_set_option_value("conceallevel", 2, { scope = "local", win = win })
      -- Suppress Vim's line-wise reveal of the cursor line so anti-conceal can reveal just the link
      -- under the cursor (an extmark with no conceal), keeping other links on the same line hidden.
      vim.api.nvim_set_option_value("concealcursor", "nvic", { scope = "local", win = win })
   end
end

-- current_cursor returns the cursor position (0-based row/col) when buf is shown in the current window.
-- It drives anti-conceal: the link under the cursor is revealed. Returns nil when conceal is off.
local function current_cursor(buf)
   if not config.options.conceal then
      return nil
   end
   local win = vim.api.nvim_get_current_win()
   if vim.api.nvim_win_get_buf(win) ~= buf then
      return nil
   end
   local pos = vim.api.nvim_win_get_cursor(win)
   return { row = pos[1] - 1, col = pos[2] }
end

-- render repaints extmarks from the cached resolved set and the current cursor, with no LSP round trip.
-- Cursor moves call this directly so anti-conceal stays cheap.
local function render(buf)
   if not vim.api.nvim_buf_is_valid(buf) then
      return
   end
   vim.api.nvim_buf_clear_namespace(buf, ns, 0, -1)
   highlight_links(buf, resolved_cache[buf] or {}, current_cursor(buf))
end

-- refresh re-fetches document links, caches which [[...]] resolve, then renders.
-- Call it after text changes; cursor-only moves should call render instead.
local function refresh(buf)
   if not vim.api.nvim_buf_is_valid(buf) then
      return
   end

   vim.lsp.buf_request(buf, "textDocument/documentLink", { textDocument = text_document_params(buf) }, function(err, result)
      if err or not vim.api.nvim_buf_is_valid(buf) then
         return
      end
      local resolved = {}
      for _, link in ipairs(result or {}) do
         local range = link.range
         if range then
            resolved[range.start.line .. ":" .. range.start.character .. ":" .. range["end"].character] = true
         end
      end
      resolved_cache[buf] = resolved
      render(buf)
   end)
end

local function schedule(buf)
   local timer = timers[buf]
   if not timer then
      timer = uv.new_timer()
      timers[buf] = timer
   end
   timer:stop()
   timer:start(config.options.debounce_ms, 0, vim.schedule_wrap(function()
      refresh(buf)
   end))
end

-- make_capabilities advertises completion to the server, merging cmp-nvim-lsp's capabilities when nvim-cmp is installed
-- so `[[` completion flows through the user's nvim-cmp setup. The server stays on utf-8 byte positions either way.
local function make_capabilities()
   local caps = vim.lsp.protocol.make_client_capabilities()
   local ok, cmp_lsp = pcall(require, "cmp_nvim_lsp")
   if ok then
      caps = vim.tbl_deep_extend("force", caps, cmp_lsp.default_capabilities())
   end
   caps.general = caps.general or {}
   caps.general.positionEncodings = { "utf-8" }
   return caps
end

local function start_client(buf)
   if client_id and vim.lsp.get_client_by_id(client_id) then
      vim.lsp.buf_attach_client(buf, client_id)
      return
   end

   client_id = vim.lsp.start({
      name = "track-lsp",
      cmd = { find_binary() },
      root_dir = vim.fn.fnamemodify(config.options.vault_dir, ":p"),
      cmd_env = {
         TRACK_VAULT = config.options.vault_dir,
      },
      capabilities = make_capabilities(),
   }, { bufnr = buf })
end

local function attach(buf)
   if attached[buf] then
      return
   end
   attached[buf] = true

   start_client(buf)
   vim.keymap.set("n", "<CR>", vim.lsp.buf.definition, { buffer = buf, desc = "track: follow link under cursor" })

   local group = vim.api.nvim_create_augroup(config.options.augroup .. "_lsp_buf_" .. buf, { clear = true })
   vim.api.nvim_create_autocmd("LspAttach", {
      group = group,
      buffer = buf,
      callback = function(ev)
         local client = vim.lsp.get_client_by_id(ev.data.client_id)
         if client and client.name == "track-lsp" then
            refresh(buf)
         end
      end,
   })
   vim.api.nvim_create_autocmd({ "BufEnter", "TextChanged", "TextChangedI", "WinScrolled" }, {
      group = group,
      buffer = buf,
      callback = function()
         schedule(buf)
      end,
   })
   -- Cursor moves only toggle anti-conceal, so repaint from cache without re-querying the server.
   vim.api.nvim_create_autocmd({ "CursorMoved", "CursorMovedI" }, {
      group = group,
      buffer = buf,
      callback = function()
         render(buf)
      end,
   })
   apply_conceal_options(buf)
   vim.api.nvim_create_autocmd("BufWinEnter", {
      group = group,
      buffer = buf,
      callback = function()
         apply_conceal_options(buf)
      end,
   })
   vim.api.nvim_create_autocmd("BufWipeout", {
      group = group,
      buffer = buf,
      callback = function()
         attached[buf] = nil
         resolved_cache[buf] = nil
         if timers[buf] then
            timers[buf]:stop()
            timers[buf]:close()
            timers[buf] = nil
         end
      end,
   })
   schedule(buf)
   vim.defer_fn(function()
      refresh(buf)
   end, config.options.debounce_ms * 4)
end

function M.setup()
   vim.api.nvim_set_hl(0, config.options.hl_group, { default = true, link = "Underlined" })
   vim.api.nvim_set_hl(0, config.options.hl_group_unresolved, { default = true, link = "Comment" })
   local group = vim.api.nvim_create_augroup(config.options.augroup .. "_lsp", { clear = true })
   vim.api.nvim_create_autocmd("FileType", {
      group = group,
      pattern = "markdown",
      callback = function(ev)
         if under_vault(ev.buf) then
            attach(ev.buf)
         end
      end,
   })
   vim.schedule(function()
      for _, buf in ipairs(vim.api.nvim_list_bufs()) do
         if vim.api.nvim_buf_is_valid(buf) and vim.bo[buf].filetype == "markdown" and under_vault(buf) then
            attach(buf)
         end
      end
   end)
end

return M
