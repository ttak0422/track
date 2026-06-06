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

print("track-e2e: PASS babel visible-lines")
vim.cmd("qa!")
