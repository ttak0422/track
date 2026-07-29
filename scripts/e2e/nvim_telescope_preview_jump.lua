local function fail(message)
   print("track-e2e: FAIL: " .. message)
   vim.cmd("cquit 1")
end

local function assert_true(ok, message)
   if not ok then
      fail(message)
   end
end

local vault = vim.env.TRACK_VAULT
assert_true(vault and vault ~= "", "TRACK_VAULT is required")

local client = require("track.client")

-- A match buried deep in a long note: the body-search preview must scroll to the matched
-- line and highlight it (the grep previewer reads entry.lnum), not sit at the top of the
-- file the way the old file_previewer did.
local match_line = 50
local body_lines = {}
for i = 1, 60 do
   body_lines[i] = "filler line " .. i
end
body_lines[match_line] = "the buried needle sits on this line"

local note, err = client.run_json({ "open", "--title", "Deep note", "--body", table.concat(body_lines, "\n") })
assert_true(note ~= nil, "create note failed: " .. tostring(err))
local reindexed, rerr = client.run_json({ "reindex" })
assert_true(reindexed ~= nil, "reindex failed: " .. tostring(rerr))

-- The CLI writes --body verbatim (no heading prefix), so the reported matched line is
-- exactly match_line; assert that so a future body-offset regression fails loudly here.
local cli, cerr = client.run_json({ "search", "--scope", "body", "--query", "needle", "--limit", "10" })
assert_true(cli ~= nil, "cli body search failed: " .. tostring(cerr))
assert_true(#(cli.results or {}) == 1, "expected 1 cli hit, got " .. vim.inspect(cli.results))
local expected_line = cli.results[1].line
assert_true(
   expected_line == match_line,
   "cli reported line " .. tostring(expected_line) .. ", want " .. match_line
)

require("track.telescope").search_body({ query = "needle" })
local action_state = require("telescope.actions.state")
local picker = action_state.get_current_picker(vim.api.nvim_get_current_buf())
assert_true(picker ~= nil, "no active picker after search_body")

local ready = vim.wait(5000, function()
   return picker.manager and picker.manager:num_results() >= 1
end, 50)
assert_true(ready, "picker did not populate a result")

local entry = picker.manager:get_entry(1)
assert_true(entry.lnum == expected_line, "entry.lnum = " .. tostring(entry.lnum) .. ", want " .. expected_line)

-- The preview loads its buffer asynchronously; wait for the previewer to highlight the
-- matched line (an extmark in telescope's preview namespace).
local ns = vim.api.nvim_create_namespace("telescope.previewers")
local state
local previewed = vim.wait(8000, function()
   local previewer = picker.previewer
   state = previewer and previewer.state
   if not (state and state.bufnr and vim.api.nvim_buf_is_valid(state.bufnr)) then
      return false
   end
   if not (state.winid and vim.api.nvim_win_is_valid(state.winid)) then
      return false
   end
   return #vim.api.nvim_buf_get_extmarks(state.bufnr, ns, 0, -1, {}) > 0
end, 50)
assert_true(previewed, "preview never highlighted a line")

-- The highlight must sit on the matched line (extmark rows are 0-based; allow the range
-- to start there or cover it), and the preview window must actually show that line.
local marks = vim.api.nvim_buf_get_extmarks(state.bufnr, ns, 0, -1, {})
local on_match = false
for _, mark in ipairs(marks) do
   if mark[2] == expected_line - 1 then
      on_match = true
   end
end
assert_true(on_match, "highlight rows " .. vim.inspect(marks) .. " miss matched line " .. expected_line)

local first_visible = vim.api.nvim_win_call(state.winid, function()
   return vim.fn.line("w0")
end)
local last_visible = vim.api.nvim_win_call(state.winid, function()
   return vim.fn.line("w$")
end)
assert_true(
   first_visible <= expected_line and expected_line <= last_visible,
   string.format("preview shows %d-%d, matched line %d is off screen", first_visible, last_visible, expected_line)
)
assert_true(first_visible > 1, "preview did not scroll (still shows the top of the file)")

vim.defer_fn(function()
   print("track-e2e: PASS telescope preview jump")
   vim.cmd("qa!")
end, 500)
