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

local function refresh(buf)
   if not vim.api.nvim_buf_is_valid(buf) then
      return
   end

   vim.lsp.buf_request(buf, "textDocument/documentLink", { textDocument = text_document_params(buf) }, function(err, result)
      if err or not vim.api.nvim_buf_is_valid(buf) then
         return
      end
      vim.api.nvim_buf_clear_namespace(buf, ns, 0, -1)
      for _, link in ipairs(result or {}) do
         local range = link.range
         if range then
            vim.api.nvim_buf_set_extmark(buf, ns, range.start.line, range.start.character, {
               end_col = range["end"].character,
               hl_group = config.options.hl_group,
               priority = 120,
            })
         end
      end
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
      capabilities = {
         general = {
            positionEncodings = { "utf-8" },
         },
      },
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
   vim.api.nvim_create_autocmd("BufWipeout", {
      group = group,
      buffer = buf,
      callback = function()
         attached[buf] = nil
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
