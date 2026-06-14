-- Health checks for :checkhealth track.

local M = {}

local function health_api()
   local h = vim.health or require("health")
   return {
      start = h.start or h.report_start,
      ok = h.ok or h.report_ok,
      warn = h.warn or h.report_warn,
      error = h.error or h.report_error,
      info = h.info or h.report_info,
   }
end

local function check_binary(h, label, resolve)
   local ok, path = pcall(resolve)
   if not ok then
      h.error(label .. " not found", { tostring(path) })
      return
   end
   if vim.fn.executable(path) == 1 then
      h.ok(label .. ": " .. path)
   else
      h.error(label .. " is not executable: " .. path)
   end
end

function M.check()
   local h = health_api()
   local config = require("track.config")

   h.start("track.nvim")
   check_binary(h, "track CLI", function()
      return require("track.client").bin()
   end)
   check_binary(h, "track-lsp", function()
      return require("track.lsp").bin()
   end)

   local vault = config.options.vault_dir
   if vault and vault ~= "" and vim.fn.isdirectory(vault) == 1 then
      h.ok("vault_dir: " .. vault)
   elseif vault and vault ~= "" then
      h.warn("vault_dir does not exist: " .. vault)
   else
      h.error("vault_dir is not configured")
   end

   local cache = config.options.cache_dir
   if cache and cache ~= "" then
      h.info("cache_dir: " .. cache)
   else
      h.warn("cache_dir is not configured")
   end

   local buf = vim.api.nvim_get_current_buf()
   local client = require("track.lsp").client(buf)
   if client then
      h.ok("track-lsp attached to current buffer")
   else
      h.info("track-lsp is not attached to the current buffer")
   end
end

return M
