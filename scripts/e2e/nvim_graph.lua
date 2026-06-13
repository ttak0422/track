local function fail(message)
   print("track-e2e: FAIL: " .. message)
   vim.cmd("cquit 1")
end

local function assert_true(ok, message)
   if not ok then
      fail(message)
   end
end

local client = require("track.client")
local vault = vim.env.TRACK_VAULT
assert_true(vault and vault ~= "", "TRACK_VAULT is required")

local function run(args)
   local data, err = client.run_json(args)
   assert_true(data ~= nil, table.concat(args, " ") .. ": " .. tostring(err))
   return data
end

run({ "new", "--title", "Go", "--id", "100" })
run({ "new", "--title", "Other", "--id", "200" })
run({ "new", "--title", "Test", "--id", "300" })
vim.fn.writefile({ "[[Test]]" }, vault .. "/note/100.md")
vim.fn.writefile({ "[[Go]]" }, vault .. "/note/200.md")
run({ "reindex", "--full" })

vim.cmd.edit(vim.fn.fnameescape(vault .. "/note/100.md"))
require("track.graph").show()

assert_true(vim.api.nvim_buf_get_name(0):match("track://graph$") ~= nil, "graph buffer was not opened")
local text = table.concat(vim.api.nvim_buf_get_lines(0, 0, -1, false), "\n")
assert_true(text:find("Go", 1, true) ~= nil, "graph should include center note")
assert_true(text:find("Other", 1, true) ~= nil, "graph should include incoming note")
assert_true(text:find("Test", 1, true) ~= nil, "graph should include outgoing note")

print("track-e2e: PASS graph")
vim.cmd("qa!")
