-- Real CLI/server lifecycle tests. The caller supplies an isolated config/vault/cache under root.
-- Never probe or start the user's 8765 workspace; every listener uses a fresh loopback port pair.
local function check(ok, message)
   if not ok then error("track-e2e: " .. message) end
end

local root = assert(vim.env.TRACK_WEB_STOP_FIXTURE, "TRACK_WEB_STOP_FIXTURE is required")
local cache = root .. "/cache"
local initial_config = assert(vim.env.TRACK_CONFIG)
check(initial_config:sub(1, #root + 1) == root .. "/", "config is not isolated")
-- config.setup deliberately clears an old inherited cache override. This fixture explicitly
-- selects its own cache after setup, in addition to recording it in the isolated machine config.
vim.env.TRACK_CACHE_DIR = cache
local uv = vim.uv or vim.loop
local client = require("track.client")
local config = require("track.config")
local web = require("track.web")
require("track.commands").setup()
local real = client.bin()
local notices, opened = {}, {}
vim.notify = function(message, level) notices[#notices + 1] = {message = message, level = level} end
vim.ui.open = function(url) opened[#opened + 1] = url end

local selected_ports = {}
local function free_pair()
   for _ = 1, 100 do
      local first, second = assert(uv.new_tcp()), assert(uv.new_tcp())
      assert(first:bind("127.0.0.1", 0))
      local port = first:getsockname().port
      local ok = port < 65535 and port ~= 8764 and port ~= 8765 and not selected_ports[port]
         and not selected_ports[port + 1] and second:bind("127.0.0.1", port + 1)
      first:close(); second:close()
      if ok then
         selected_ports[port], selected_ports[port + 1] = true, true
         return "127.0.0.1:" .. port
      end
   end
   error("no free loopback pair")
end

local function ready(addr)
   local result = vim.system({real, "markdown", "--addr", addr, "--check"}, {text = true}):wait()
   local ok, data = pcall(vim.json.decode, result.stdout or "")
   return result.code == 0 and ok and data.ready == true
end

local function wait_ready(addr, expected)
   check(vim.wait(10000, function() return ready(addr) == expected end, 50),
      "unexpected listener state (expected ready=" .. tostring(expected) .. "): " .. addr .. ": " .. vim.inspect(notices))
end

local function lease(addr)
   return cache .. "/web-" .. addr:gsub(":", "-") .. ".pid"
end

local function pid(addr)
   return vim.fn.readfile(lease(addr))[1]
end

local function stop(command, expected)
   local before = #notices
   vim.cmd(command)
   check(vim.wait(10000, function()
      for i = before + 1, #notices do
         if notices[i].message:find(expected, 1, true) then return true end
      end
      return false
   end, 20), "missing stop result: " .. expected .. ": " .. vim.inspect(notices))
end

check(vim.tbl_contains(vim.fn.getcompletion("Track webS", "cmdline"), "webStop"), "webStop completion missing")
local addr, other = free_pair(), free_pair()
config.options.web_addr = addr
stop("Track webStop", "no managed server")
local before = #notices
vim.cmd("Track webStop --addr " .. addr .. " extra")
check(notices[#notices].message:find("usage: :Track webStop", 1, true) and #notices > before, "bad args accepted")

-- A small shim injects CLI failures without touching permissions or unrelated processes. Startup
-- execs the real CLI, so its PID file and listener still belong to a real Track process.
local flag = root .. "/failure"
local shim = root .. "/cli-shim"
vim.env.TRACK_STOP_TEST_FLAG = flag
vim.env.TRACK_STOP_REAL_CLI = real
vim.fn.writefile({"#!/bin/sh", 'if [ "$1" = web ] && [ "$2" = stop ] && [ -f "$TRACK_STOP_TEST_FLAG" ]; then',
   '  printf \'{"error":"synthetic stop failure"}\\n\'', "  exit 7", "fi", 'exec "$TRACK_STOP_REAL_CLI" "$@"'}, shim)
vim.fn.setfperm(shim, "rwx------")
client.bin = function() return shim end

vim.cmd("Track web")
wait_ready(addr, true)
check(vim.wait(5000, function() return #opened > 0 end, 20), "initial browser open missing")
local original_pid = pid(addr)
vim.fn.writefile({"fail"}, flag)
stop("Track webStop", "synthetic stop failure")
check(ready(addr) and pid(addr) == original_pid, "stop failure lost the live server")
vim.cmd("Track web")
check(notices[#notices].message:find("already running", 1, true), "stop failure discarded the launcher job")
check(pid(addr) == original_pid, "stop failure lost the launcher handle")
vim.fn.delete(flag)

-- A missing lease must not discard a live job and let another :Track web collide with it.
local record = vim.fn.readfile(lease(addr))
vim.fn.delete(lease(addr))
stop("Track webStop", "still running")
check(ready(addr), "missing lease killed a listener")
vim.cmd("Track web")
check(notices[#notices].message:find("already running", 1, true), "missing lease discarded the launcher job")
check(ready(addr), "missing lease lost the live job")
vim.fn.writefile(record, lease(addr))

-- An explicit other address leaves the launcher job alone. With no args, actual launch context
-- wins over settings/environment changed after startup.
local external = vim.fn.jobstart({real, "web", "--addr", other})
check(external > 0, "external start failed")
wait_ready(other, true)
stop("Track webStop --addr " .. other, "track web stopped:")
wait_ready(other, false)
check(pid(addr) == original_pid and ready(addr), "stopping another address released the launcher")
config.options.web_addr = other
vim.env.TRACK_CONFIG = root .. "/other-config.yml"
vim.env.TRACK_CACHE_DIR = root .. "/other-cache"
vim.fn.writefile({"vault_dir: " .. vim.env.TRACK_VAULT, "cache_dir: " .. vim.env.TRACK_CACHE_DIR}, vim.env.TRACK_CONFIG)
stop("Track webStop", "track web stopped:")
wait_ready(addr, false)
check(vim.fn.filereadable(lease(addr)) == 0, "successful stop left a PID lease")

-- Restart can reuse the released address. Stop also cancels any delayed
-- readiness callback so a browser does not open after the user has asked to stop.
vim.env.TRACK_CONFIG, vim.env.TRACK_CACHE_DIR = initial_config, cache
config.options.web_addr = addr
vim.cmd("Track web")
wait_ready(addr, true)
check(pid(addr) ~= original_pid, "restart reused an exited process")
stop("Track webStop " .. addr, "track web stopped:")
wait_ready(addr, false)
local stopped_count = #opened
vim.wait(400, function() return false end, 20)
check(#opened == stopped_count, "delayed readiness reopened a stopped server")
stop("Track webStop", "no managed server")

-- Without a launcher job, current configuration can stop a managed server started elsewhere.
external = vim.fn.jobstart({real, "web", "--addr", addr})
check(external > 0, "second external start failed")
wait_ready(addr, true)
stop("Track webStop", "track web stopped:")
wait_ready(addr, false)
check(vim.wait(5000, function() return vim.fn.jobwait({external}, 0)[1] ~= -1 end, 20), "external job did not exit")
print("track-e2e: PASS nvim webStop (completion, stopped, normal stop, failures, context, other address, restart)")
vim.cmd("qa!")
