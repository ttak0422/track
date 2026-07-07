-- track.nvim LSP integration.
-- The Go LSP server owns link discovery; Lua starts the server and renders document links as underlined extmarks.

local config = require("track.config")

local M = {}

-- Patched by Nix at build time to the bundled track-lsp binary's store path.
local bundled_lsp_binary_path = nil
local cached_binary

local uv = vim.uv or vim.loop
local ns = vim.api.nvim_create_namespace("track_lsp_links")
local timers = {}
-- attached[buf] guards the one-time per-buffer setup (keymaps, render autocmds). The LSP client itself
-- is owned by vim.lsp.start, which dedupes by name + root_dir, so we never cache a client id here.
local attached = {}
-- resolved_cache[buf] = set of "row:start:end" keys the server returned as resolved document links,
-- kept so cursor moves can repaint (toggle anti-conceal) without another LSP round trip.
local resolved_cache = {}
local create_command_registered = false
local rename_command_registered = false

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
   if path:sub(1, #vault) ~= vault then
      return false
   end
   local rel = path:sub(#vault + 1)
   local dir = rel:match("^([^/]+)/")
   return dir == "note" or dir == "journal"
end

local function text_document_params(buf)
   return { uri = vim.uri_from_bufnr(buf) }
end

local method_capabilities = {
   ["textDocument/definition"] = "definitionProvider",
   ["textDocument/documentLink"] = "documentLinkProvider",
   ["textDocument/hover"] = "hoverProvider",
}

local function lsp_clients(buf)
   if vim.lsp.get_clients then
      return vim.lsp.get_clients({ bufnr = buf })
   end
   return vim.lsp.get_active_clients({ bufnr = buf })
end

local function supports_method(client, method, buf)
   if type(client.supports_method) == "function" then
      local ok, supported = pcall(client.supports_method, client, method, buf)
      if ok then
         return supported
      end
      ok, supported = pcall(client.supports_method, client, method, { bufnr = buf })
      if ok then
         return supported
      end
      ok, supported = pcall(client.supports_method, client, method)
      if ok then
         return supported
      end
   end
   local capability = method_capabilities[method]
   return capability ~= nil and client.server_capabilities and client.server_capabilities[capability] ~= nil
end

local function track_client(buf, method)
   for _, client in ipairs(lsp_clients(buf)) do
      if client.name == "track-lsp" and (method == nil or supports_method(client, method, buf)) then
         return client
      end
   end
   return nil
end

function M.client(buf, method)
   return track_client(buf or vim.api.nvim_get_current_buf(), method)
end

function M.bin()
   return find_binary()
end

local function publish_web_follow(buf)
   local ok, web = pcall(require, "track.web")
   if ok and type(web.publish_follow) == "function" then
      web.publish_follow(buf)
   end
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
            local is_resolved = resolved[row .. ":" .. inner_start .. ":" .. inner_end]

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

            -- Only resolved links are underlined; an unresolved link stays plain and is flagged by the
            -- server's "unresolved-link" diagnostic instead.
            if is_resolved and inner_end > hl_start then
               vim.api.nvim_buf_set_extmark(buf, ns, row, hl_start, {
                  end_col = inner_end,
                  hl_group = config.options.hl_group,
                  priority = 120,
               })
            end
            init = e + 1
         end
      end
   end
end

-- reveal_code_fences rewrites the markdown treesitter highlights query to drop its conceal/conceal_lines
-- directives, so raising conceallevel for links does not also hide ```lang fence delimiters. This edits a
-- global query, which is appropriate for a markdown note environment; it is a no-op without the parser.
local function reveal_code_fences()
   local ok, files = pcall(vim.treesitter.query.get_files, "markdown", "highlights")
   if not ok or not files then
      return
   end
   local parts = {}
   for _, file in ipairs(files) do
      local fd = io.open(file, "r")
      if fd then
         parts[#parts + 1] = fd:read("*a")
         fd:close()
      end
   end
   local src = table.concat(parts, "\n"):gsub("%(#set!%s+conceal[^%)]*%)", "")
   pcall(vim.treesitter.query.set, "markdown", "highlights", src)
end

-- apply_conceal_options enables concealment in every window currently showing buf.
local function apply_conceal_options(buf)
   if not config.options.conceal then
      return
   end
   for _, win in ipairs(vim.fn.win_findbuf(buf)) do
      vim.api.nvim_set_option_value("conceallevel", 2, { scope = "local", win = win })
      -- "nvc" (no "i"): in normal/visual/command mode Vim does not reveal the cursor line, so our
      -- per-link anti-conceal extmarks decide what shows. In insert mode Vim reveals the whole cursor
      -- line, keeping byte and screen columns aligned so the completion popup doesn't drift while typing.
      vim.api.nvim_set_option_value("concealcursor", "nvc", { scope = "local", win = win })
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

   local client = track_client(buf, "textDocument/documentLink")
   if not client then
      return
   end

   client:request("textDocument/documentLink", { textDocument = text_document_params(buf) }, function(err, result)
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
   end, buf)
   -- Includes repaint on the same debounced trigger; their extmarks live in their own namespace so
   -- cursor-move repaints of the link highlights never touch them.
   require("track.include").refresh(buf)
end

local function register_create_note_command()
   if create_command_registered then
      return
   end
   create_command_registered = true

   vim.lsp.commands = vim.lsp.commands or {}
   vim.lsp.commands["track.createNote"] = function(command, ctx)
      local arg = (command.arguments or {})[1] or {}
      local title = type(arg) == "table" and arg.title or tostring(arg)
      title = title and vim.trim(title) or ""
      if title == "" or title == "nil" then
         vim.notify("track: note title is empty", vim.log.levels.WARN)
         return
      end
      local uri = type(arg) == "table" and arg.uri or nil
      local lsp_client = ctx and ctx.client_id and vim.lsp.get_client_by_id(ctx.client_id)
      if not lsp_client then
         vim.notify("track: LSP client is not available", vim.log.levels.ERROR)
         return
      end
      local buf = (ctx and ctx.bufnr) or vim.api.nvim_get_current_buf()
      lsp_client:request("workspace/executeCommand", {
         command = "track.createNote",
         arguments = { { title = title, uri = uri } },
      }, function(err, result)
         if err then
            vim.notify("track: " .. tostring(err.message or err), vim.log.levels.ERROR)
            return
         end
         if result and result.path then
            vim.notify("track: created " .. result.path, vim.log.levels.INFO)
         end
         vim.schedule(function()
            refresh(buf)
         end)
      end, buf)
   end
end

-- The "Rename note" code action carries the target note and position; the actual rename runs through
-- textDocument/rename. We register a client-side command so selecting the action prompts for the new
-- title (prefilled with the current one) instead of round-tripping to the server, which cannot prompt.
local function register_rename_note_command()
   if rename_command_registered then
      return
   end
   rename_command_registered = true

   vim.lsp.commands = vim.lsp.commands or {}
   vim.lsp.commands["track.renameNote"] = function(command, ctx)
      local arg = (command.arguments or {})[1] or {}
      local title = (type(arg) == "table" and arg.title) or ""
      local uri = type(arg) == "table" and arg.uri or nil
      local pos = type(arg) == "table" and arg.position or nil
      local buf = (ctx and ctx.bufnr) or vim.api.nvim_get_current_buf()
      local lsp_client = ctx and ctx.client_id and vim.lsp.get_client_by_id(ctx.client_id)
      if not lsp_client then
         vim.notify("track: LSP client is not available", vim.log.levels.ERROR)
         return
      end
      vim.ui.input({ prompt = "Rename note: ", default = title }, function(input)
         if input == nil then
            return
         end
         input = vim.trim(input)
         if input == "" or input == title then
            return
         end
         lsp_client:request("textDocument/rename", {
            textDocument = { uri = uri or vim.uri_from_bufnr(buf) },
            position = pos or { line = 0, character = 0 },
            newName = input,
         }, function(err, result)
            if err then
               vim.notify("track: " .. tostring(err.message or err), vim.log.levels.ERROR)
               return
            end
            if result then
               vim.lsp.util.apply_workspace_edit(result, lsp_client.offset_encoding or "utf-8")
            end
            vim.notify("track: renamed note to " .. input, vim.log.levels.INFO)
            vim.schedule(function()
               refresh(buf)
            end)
         end, buf)
      end)
   end
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

-- ensure_client starts (or re-binds to) the track-lsp client for buf. vim.lsp.start reuses a client
-- whose name and root_dir match, so this is idempotent and cheap to call on every BufEnter. That call
-- pattern is the self-heal: if the server crashed, the next time the note is entered a fresh client is
-- started, so links recover without any manual command. Returns the client id, or nil on failure.
local function ensure_client(buf)
   local ok, id = pcall(vim.lsp.start, {
      name = "track-lsp",
      cmd = { find_binary() },
      root_dir = vim.fn.fnamemodify(config.options.vault_dir, ":p"),
      cmd_env = {
         TRACK_VAULT = config.options.vault_dir,
         TRACK_CACHE_DIR = config.options.cache_dir,
      },
      capabilities = make_capabilities(),
   }, { bufnr = buf })
   if not ok then
      vim.notify("track: failed to start track-lsp: " .. tostring(id), vim.log.levels.WARN)
      return nil
   end
   return id
end

-- attach wires buf to the server and sets up its keymaps and link-rendering hooks. ensure_client and
-- the keymaps run every call (self-heal); the heavier one-time setup (user hook, autocmds) is guarded
-- by attached[buf].
local function attach(buf)
   if not ensure_client(buf) then
      return
   end

   -- Keymaps are buffer-local and are dropped when a buffer is unloaded (e.g. :bdelete, which fires
   -- BufUnload but not BufWipeout, so it does not clear attached[buf]). Neovim reuses the same buffer
   -- number when the note is reopened, so re-set the maps on every attach — otherwise the reopened
   -- buffer keeps the stale guard and loses <CR>/K, and Enter silently stops following links. Autocmds
   -- survive the unload, so they stay behind the one-time guard below.
   vim.keymap.set("n", "<CR>", function()
      return require("track.follow").smart_action()
   end, { expr = true, buffer = buf, desc = "track: follow link under cursor" })
   vim.keymap.set("n", "K", function()
      if track_client(buf, "textDocument/hover") then
         vim.lsp.buf.hover()
      else
         vim.notify("track: LSP hover is not ready for this buffer", vim.log.levels.INFO)
      end
   end, { buffer = buf, desc = "track: hover note link" })

   if attached[buf] then
      return
   end
   attached[buf] = true

   -- User hook for a freshly attached track note buffer. It runs once per buffer, after the built-in
   -- keymaps, so a config can add buffer-local mappings (e.g. point a references key at Track backlinks,
   -- which lists by title instead of the epoch filename) without writing its own LspAttach autocmd. A
   -- failing hook is reported but must not abort the rest of the buffer setup (link-render autocmds).
   if config.options.on_attach then
      local ok, err = pcall(config.options.on_attach, buf)
      if not ok then
         vim.notify("track: on_attach failed: " .. tostring(err), vim.log.levels.ERROR)
      end
   end

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
   -- Re-entering the buffer re-binds the client (self-heal after a crash) before repainting.
   vim.api.nvim_create_autocmd("BufEnter", {
      group = group,
      buffer = buf,
      callback = function()
         ensure_client(buf)
         schedule(buf)
         publish_web_follow(buf)
      end,
   })
   vim.api.nvim_create_autocmd({ "TextChanged", "TextChangedI", "WinScrolled" }, {
      group = group,
      buffer = buf,
      callback = function()
         schedule(buf)
         publish_web_follow(buf)
      end,
   })
   -- Cursor moves only toggle anti-conceal, so repaint from cache without re-querying the server.
   vim.api.nvim_create_autocmd({ "CursorMoved", "CursorMovedI" }, {
      group = group,
      buffer = buf,
      callback = function()
         render(buf)
         publish_web_follow(buf)
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
   register_create_note_command()
   register_rename_note_command()
   if config.options.conceal and config.options.reveal_code_fences then
      reveal_code_fences()
   end
   local group = vim.api.nvim_create_augroup(config.options.augroup .. "_lsp", { clear = true })
   -- FileType wires a freshly loaded note once; from then on its per-buffer BufEnter autocmd re-binds
   -- the client on every entry, which is what recovers the LSP after a crash.
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
