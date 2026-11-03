-- Exercise the real Telescope picker with an isolated, deliberately slow CLI.
-- No user vault or actual track process is accessed.
local function check(ok, message)
   if not ok then error(message, 0) end
end
local tmp = vim.fn.tempname()
vim.fn.mkdir(tmp, "p")
local started, finished = tmp .. "/started", tmp .. "/finished"
local note = tmp .. "/note.md"
vim.fn.writefile({ "example" }, note)
local payload = vim.json.encode({ results = { { title = "早い", path = note, note_id = 1 } } })
local executable = tmp .. "/mock track"
vim.fn.writefile({
   "#!/bin/sh",
   'if [ "$5" = slow ]; then',
   '  touch "' .. started .. '"',
   '  sleep 1',
   '  touch "' .. finished .. '"',
   '  printf \'%s\\n\' \'{"results":[]}\'',
   'elif [ "$5" = zero ]; then',
   '  printf \'%s\\n\' \'{"results":[]}\'',
   'elif [ "$5" = failure ]; then',
   '  echo bad >&2; exit 1',
   'else',
   "  printf '%s\\n' '" .. payload .. "'",
   'fi',
}, executable)
vim.fn.setfperm(executable, "rwx------")
require("track.client").bin = function() return executable end
package.loaded["track.vault"] = { current_name = function() return "" end }
local created, opened, copied = {}, {}, {}
package.loaded["track.create"] = { create = function(title) created[#created + 1] = title end }
require("track.editor_view").open = function(path) opened[#opened + 1] = path; return true end
require("track.editor_view").copy_link = function(path) copied[#copied + 1] = path end

require("track.telescope").search_title({ cache_picker = { num_pickers = 1, limit_entries = 100, ignore_empty_prompt = false } })
local state = require("telescope.actions.state")
local actions = require("telescope.actions")
local prompt = vim.api.nvim_get_current_buf()
local picker = state.get_current_picker(prompt)
check(picker ~= nil, "search picker did not open")
picker:set_prompt("slow")
check(vim.wait(5000, function() return vim.uv.fs_stat(started) ~= nil end, 10), "slow CLI did not start")
-- This assertion fails with the old synchronous finder: by the time Neovim can
-- observe the start marker, the sleep has ended and the finish marker exists.
check(not vim.uv.fs_stat(finished), "slow CLI blocked Neovim's event loop")
actions.select_default(prompt)
check(vim.api.nvim_buf_is_valid(prompt) and #created == 0 and #opened == 0, "Enter during search acted on a transient empty list")
picker:set_prompt("早い")
check(vim.wait(5000, function()
   return picker.manager and picker.manager:num_results() == 1
end, 10), "new Unicode query did not populate")
local entry = picker.manager:get_entry(1)
check(entry.value.title == "早い" and entry.value.path == note, "new result was replaced or corrupted")
-- Wait beyond the old process's delay: late completion must not clear the newer result.
vim.wait(1200, function() return false end, 10)
check(picker.manager:num_results() == 1 and picker.manager:get_entry(1).value.title == "早い", "stale completion replaced the current query")
-- Copy the current hit, then change the prompt. Before Telescope starts the new
-- finder the old highlighted entry must neither be opened nor copied.
check(vim.wait(5000, function() return picker.finder:owns(state.get_selected_entry()) end, 10), "current result was not selected")
local copy_key = vim.fn.maparg("<C-y>", "i", false, true).callback
check(type(copy_key) == "function", "copy mapping missing")
copy_key()
check(#copied == 1 and copied[1] == note, "copy did not use the current hit")
picker:set_prompt("failure")
copy_key()
check(#copied == 1, "copy used a stale selection")
actions.select_default(prompt)
check(vim.api.nvim_buf_is_valid(prompt) and #opened == 0 and #created == 0, "Enter opened a stale selection")
check(vim.wait(5000, function() return picker.finder and picker.finder.status == "error" end, 10), "failure was not reported")
actions.select_default(prompt)
check(vim.api.nvim_buf_is_valid(prompt) and #opened == 0 and #created == 0, "Enter after failed search acted on a transient empty list")
picker:set_prompt("zero")
check(vim.wait(5000, function() return picker.finder:can_select("zero") end, 10), "empty search did not complete")
actions.select_default(prompt)
check(#created == 1 and created[1] == "zero" and #opened == 0, "successful empty search did not create the prompted title")

require("telescope.builtin").resume()
prompt = vim.api.nvim_get_current_buf()
picker = state.get_current_picker(prompt)
check(vim.wait(5000, function() return picker and picker.finder:can_select("zero") end, 10), "cached search did not resume")
picker:set_prompt("早い")
check(vim.wait(5000, function()
   local entry = state.get_selected_entry()
   return picker.finder:can_select("早い") and entry and picker.finder:owns(entry)
end, 10), "new result was not selected")
actions.select_default(prompt)
check(#opened == 1 and opened[1] == note, "Enter did not open the selected result")
print("track-e2e: PASS telescope asynchronous search")
vim.cmd("qa!")
