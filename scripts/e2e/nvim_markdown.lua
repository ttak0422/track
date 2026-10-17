-- Real CLI/server integration with an isolated vault/config/cache supplied by CI or the caller.
-- Browser opening is captured; API consumption verifies the exact data behind each one-use URL.
local function check(ok, message)
   if not ok then error("track-e2e: " .. message) end
end

local uv = vim.uv or vim.loop
local client = require("track.client")
local config = require("track.config")
local web = require("track.web")
local parent = assert(vim.env.TRACK_MARKDOWN_FIXTURE, "TRACK_MARKDOWN_FIXTURE is required")
vim.fn.mkdir(parent .. "/日本語 folder/docs", "p")
local filename = parent .. "/日本語 folder/現在 ' # $().md"
vim.fn.writefile({ "# Current saved body", "[Next](docs/next.md)", "![Image](pixel.png)" }, filename)
vim.fn.writefile({ "# Readme is not current" }, parent .. "/日本語 folder/README.md")
vim.fn.writefile({ "# Next" }, parent .. "/日本語 folder/docs/next.md")
local png = string.char(137, 80, 78, 71, 13, 10, 26, 10)
local image = assert(io.open(parent .. "/日本語 folder/pixel.png", "wb")); image:write(png); image:close()

local function free_pair()
   for _ = 1, 50 do
      local first = assert(uv.new_tcp()); assert(first:bind("127.0.0.1", 0))
      local port = first:getsockname().port
      local second = assert(uv.new_tcp())
      local ok = port < 65535 and second:bind("127.0.0.1", port + 1)
      first:close(); second:close()
      if ok then return "127.0.0.1:" .. port end
   end
   error("no free loopback pair")
end
local addr = free_pair()
config.options.web_addr = addr
local opened, notices = {}, {}
vim.ui.open = function(url) opened[#opened + 1] = url end
vim.notify = function(message) notices[#notices + 1] = message end
vim.cmd("enew")
vim.cmd("Track markdown")
check(#opened == 0 and table.concat(notices, " "):find("saved .md", 1, true), "unnamed buffer was accepted")
vim.cmd.edit(vim.fn.fnameescape(filename))

local function wait_for_open(count)
   check(vim.wait(20000, function() return #opened >= count end, 50), "no browser URL: " .. table.concat(notices, "; "))
   local token = opened[count]:match("/markdown#handoff=([a-f0-9]+)$")
   check(token and #token == 64, "missing capability")
   check(not opened[count]:find("日本語", 1, true), "filename leaked into URL")
   local result = vim.system({ "curl", "-fsS", "--max-time", "5", "-H", "Content-Type: application/json", "-d", vim.json.encode({ token = token }), "http://" .. addr .. "/api/markdown/consume" }, { text = true }):wait()
   check(result.code == 0, "cannot consume handoff: " .. tostring(result.stderr))
   local snapshot = vim.json.decode(result.stdout)
   check(snapshot.active == "現在 ' # $().md" and #snapshot.files == 4, "wrong file/folder")
   local files = {}; for _, file in ipairs(snapshot.files) do files[file.path] = vim.base64.decode(file.data) end
   check(files["現在 ' # $().md"]:find("# Current saved body", 1, true), "wrong disk body")
   check(files["docs/next.md"] and files["pixel.png"] == png, "missing relative link/image")
end

-- Unnamed and modified buffers do not start a server, save a file, or open a browser.
vim.api.nvim_buf_set_lines(0, 0, -1, false, { "unsaved" })
vim.cmd("Track markdown")
check(#opened == 0 and vim.bo.modified, "modified buffer was accepted or saved")
check(table.concat(notices, " "):find("save the current file", 1, true), "missing save guidance")
vim.cmd("edit!")
-- A custom-address workspace started via :Track web is the one :Track markdown should reuse.
web.open({ addr }, function() end)
config.options.web_addr = "127.0.0.1:1"
vim.cmd("Track markdown")
wait_for_open(1)
local pid = vim.fn.system({ "curl", "-fsS", "--max-time", "2", "http://" .. addr .. "/api/markdown" })
check(pid:find("track%-markdown%-v1"), "server never started")
vim.cmd("Track markdown")
wait_for_open(2)
check(opened[1] ~= opened[2], "handoff capability was reused")

-- Starting from a stopped server is covered separately below by the real markdown launcher.
client.run({ "web", "stop", "--addr", addr })
addr = free_pair(); config.options.web_addr = addr
vim.cmd("Track markdown")
wait_for_open(3)

-- Reuse a server started elsewhere, without claiming ownership of its process.
client.run({ "web", "stop", "--addr", addr })
addr = free_pair(); config.options.web_addr = addr
local external = vim.fn.jobstart({ client.bin(), "web", "--addr", addr })
check(external > 0, "external server start failed")
check(vim.wait(10000, function()
   local result = client.run_json({ "markdown", "--addr", addr, "--check" })
   return result and result.ready
end, 100), "external server not ready")
vim.cmd("Track markdown")
wait_for_open(4)
check(vim.fn.jobwait({ external }, 0)[1] == -1, "reused server was stopped")
client.run({ "web", "stop", "--addr", addr })
check(vim.wait(5000, function() return vim.fn.jobwait({ external }, 0)[1] ~= -1 end, 50), "external server did not stop")
print("track-e2e: PASS nvim markdown (saved Unicode paths, scope, images, startup and reuse)")
vim.cmd("qa!")
