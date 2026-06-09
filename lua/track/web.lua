-- track.nvim web workspace launcher.

local client = require("track.client")
local config = require("track.config")

local M = {}

local job_id
local running_addr
local stopping = false
local autostop_registered = false

local function is_running()
   return job_id ~= nil and vim.fn.jobwait({ job_id }, 0)[1] == -1
end

local function notify_lines(data, level)
   if not data then
      return
   end
   local lines = {}
   for _, line in ipairs(data) do
      if line and line ~= "" then
         lines[#lines + 1] = line
      end
   end
   if #lines == 0 then
      return
   end
   vim.schedule(function()
      for _, line in ipairs(lines) do
         vim.notify(line, level)
      end
   end)
end

local function open_url(url)
   if type(vim.ui.open) ~= "function" then
      return
   end
   pcall(vim.ui.open, url)
end

local function register_autostop()
   if autostop_registered then
      return
   end
   autostop_registered = true
   local group = vim.api.nvim_create_augroup(config.options.augroup .. "_web", { clear = true })
   vim.api.nvim_create_autocmd("VimLeavePre", {
      group = group,
      callback = function()
         if is_running() then
            stopping = true
            vim.fn.jobstop(job_id)
         end
      end,
   })
end

local function parse_addr(args)
   args = args or {}
   if #args == 0 then
      return config.options.web_addr
   end
   if #args == 1 then
      return args[1]
   end
   if #args == 2 and args[1] == "--addr" then
      return args[2]
   end
   return nil, "usage: :Track web [addr] or :Track web --addr addr"
end

function M.open(args)
   local addr, err = parse_addr(args)
   if not addr then
      vim.notify("track: " .. err, vim.log.levels.ERROR)
      return
   end

   local url = "http://" .. addr
   if is_running() then
      local running_url = "http://" .. (running_addr or addr)
      vim.notify("track web already running: " .. running_url, vim.log.levels.INFO)
      open_url(running_url)
      return
   end

   register_autostop()

   local cmd = { client.bin(), "web", "--addr", addr }
   local env = {
      TRACK_VAULT = config.options.vault_dir,
      TRACK_CACHE_DIR = config.options.cache_dir,
   }
   stopping = false
   job_id = vim.fn.jobstart(cmd, {
      env = env,
      stdout_buffered = false,
      stderr_buffered = false,
      on_stdout = function(_, data)
         notify_lines(data, vim.log.levels.INFO)
      end,
      on_stderr = function(_, data)
         notify_lines(data, vim.log.levels.INFO)
      end,
      on_exit = function(_, code)
         local stopped = stopping
         job_id = nil
         running_addr = nil
         stopping = false
         if code ~= 0 and not stopped then
            vim.schedule(function()
               vim.notify("track web exited with code " .. tostring(code), vim.log.levels.ERROR)
            end)
         end
      end,
   })

   if job_id <= 0 then
      local failed = job_id
      job_id = nil
      running_addr = nil
      vim.notify("track: failed to start web server job (" .. tostring(failed) .. ")", vim.log.levels.ERROR)
      return
   end

   running_addr = addr
   open_url(url)
end

return M
