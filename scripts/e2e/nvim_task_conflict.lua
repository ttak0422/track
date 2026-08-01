-- Regression: task_cycle must identify the exact note version shown in the buffer. If another
-- writer changes the file before the CLI mutation, keep the user's view in place and surface the
-- conflict as a warning instead of silently cycling the task now occupying the old line.

local function fail(message)
   print("track-e2e: FAIL: " .. message)
   vim.cmd("cquit 1")
end

local function assert_true(ok, message)
   if not ok then
      fail(message)
   end
end

local captured_args
package.loaded["track.client"] = {
   run_json = function(args)
      captured_args = args
      return nil, "note changed on disk; reload and retry"
   end,
}
package.loaded["track.tasks"] = nil

local notices = {}
vim.notify = function(message, level)
   table.insert(notices, { message = message, level = level })
end

local vault = vim.env.TRACK_VAULT
assert_true(vault and vault ~= "", "TRACK_VAULT is required")
vim.fn.mkdir(vault .. "/note", "p")
local file = vault .. "/note/301.md"
local shown = { "# Tasks", "", "- [ ] alpha" }
vim.fn.writefile(shown, file)
vim.o.swapfile = false
vim.cmd("edit " .. vim.fn.fnameescape(file))
vim.api.nvim_win_set_cursor(0, { 3, 0 })

local content = table.concat(shown, "\n") .. "\n"
local expected_etag = vim.fn.sha256(content):sub(1, 32)
vim.fn.writefile({ "# Tasks", "", "- [ ] inserted", "- [ ] alpha" }, file)

require("track.tasks").cycle()

assert_true(captured_args ~= nil, "task cycle did not call the CLI")
local passed_etag
for i, arg in ipairs(captured_args) do
   if arg == "--etag" then
      passed_etag = captured_args[i + 1]
   end
end
assert_true(passed_etag == expected_etag, "task cycle did not pass the displayed buffer etag")
assert_true(#notices == 1, "task conflict should emit one notification")
assert_true(notices[1].level == vim.log.levels.WARN, "task conflict notification should be WARN")
assert_true(notices[1].message:match("changed") ~= nil, "task conflict notification should explain the stale note")
assert_true(
   table.concat(vim.api.nvim_buf_get_lines(0, 0, -1, false), "\n") == table.concat(shown, "\n"),
   "a refused task cycle must not reload the externally changed file"
)

print("track-e2e: PASS nvim task conflict")
vim.cmd("qa!")
