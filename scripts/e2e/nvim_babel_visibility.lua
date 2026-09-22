local function fail(message)
   print("track-e2e: FAIL: " .. message)
   vim.cmd("cquit 1")
end

local function assert_true(ok, message)
   if not ok then
      fail(message)
   end
end

local function hidden_rows(buf)
   local ns = vim.api.nvim_get_namespaces().track_babel_source_visibility
   assert_true(ns ~= nil, "track_babel_source_visibility namespace is missing")
   local rows = {}
   for _, mark in ipairs(vim.api.nvim_buf_get_extmarks(buf, ns, 0, -1, { details = true })) do
      local row = mark[2]
      local details = mark[4] or {}
      if details.conceal_lines ~= nil then
         rows[row] = true
      end
   end
   return rows
end

vim.cmd.enew()
local buf = vim.api.nvim_get_current_buf()
vim.api.nvim_buf_set_lines(buf, 0, -1, false, {
   "# Demo",
   "```c :visible-lines 4-5",
   "#include <stdio.h>",
   "",
   "int main(void) {",
   '    printf("hello\\n");',
   "    return 0;",
   "}",
   "```",
})
vim.bo.filetype = "markdown"
vim.api.nvim_win_set_cursor(0, { 1, 0 })

local babel = require("track.babel")
babel.apply_visibility(buf)

local rows = hidden_rows(buf)
assert_true(rows[2], "body line 1 should be hidden")
assert_true(rows[3], "body line 2 should be hidden")
assert_true(rows[4], "body line 3 should be hidden")
assert_true(not rows[5], "body line 4 should be visible")
assert_true(not rows[6], "body line 5 should be visible")
assert_true(rows[7], "body line 6 should be hidden")

vim.api.nvim_win_set_cursor(0, { 3, 0 })
babel.apply_visibility(buf)
rows = hidden_rows(buf)
assert_true(not rows[2], "cursor row should be revealed")
assert_true(rows[3] and rows[4] and rows[7], "non-cursor hidden rows should remain hidden")

vim.api.nvim_buf_set_lines(buf, 0, -1, false, {
   "````markdown :visible-lines 1",
   "shown",
   "```sh",
   "echo hidden",
   "```",
   "hidden after inner fence",
   "````",
})
vim.api.nvim_win_set_cursor(0, { 1, 0 })
babel.apply_visibility(buf)
rows = hidden_rows(buf)
assert_true(not rows[1] and rows[2] and rows[3] and rows[4] and rows[5], "long fence must contain shorter fences")

-- Exercise the frontend contract without executing user code or touching a vault.
vim.api.nvim_buf_set_name(buf, vim.fn.tempname() .. ".md")
local client = require("track.client")
local original_run = client.run_json
local display = true
client.run_json = function(args, body)
   assert_true(body:find("hidden after inner fence", 1, true) ~= nil, "current buffer must reach CLI")
   assert_true(vim.tbl_contains(args, "--body-stdin"), "restore and exec must use stdin body")
   if args[2] == "restore" then
      return { blocks = {} }
   end
   return { end_line = 6, status = "success", exit_code = 0, stdout = "result", display = display }
end
babel.exec()
local result_ns = vim.api.nvim_get_namespaces().track_babel_results
assert_true(#vim.api.nvim_buf_get_extmarks(buf, result_ns, 0, -1, {}) == 1, "output should render")
display = false
babel.exec()
assert_true(#vim.api.nvim_buf_get_extmarks(buf, result_ns, 0, -1, {}) == 0, "none/discard must clear previous result")
babel.restore()
client.run_json = original_run

print("track-e2e: PASS babel visibility and results")
vim.cmd("qa!")
