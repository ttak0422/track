-- Regression: the note title used to render as virt_text with virt_text_pos = "overlay"
-- on the first buffer line, so it overlapped the body when line 1 already had content.
-- The title now renders as a virt_lines_above virtual line, which never covers body text
-- and leaves the first buffer line fully editable.

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

-- Line 1 of the body has content on purpose: the overlay used to draw the title on top of it.
local created, cerr = client.run_json({ "open", "--title", "Virt line title", "--body", "first body line" })
assert_true(created ~= nil, "create note failed: " .. tostring(cerr))

vim.cmd.edit(vim.fn.fnameescape(created.path))
local buf = vim.api.nvim_get_current_buf()

local ns = vim.api.nvim_create_namespace("track_title")
-- vim.wait converts a truthy callback return to `true`, so capture the details in a closure.
local found, details = false, nil
vim.wait(5000, function()
   local marks = vim.api.nvim_buf_get_extmarks(buf, ns, 0, -1, { details = true })
   if #marks == 0 then
      return false
   end
   found, details = true, marks[1][4]
   return true
end, 50)
assert_true(found, "title extmark never appeared")

assert_true(details.virt_lines ~= nil, "title must render as virt_lines, not overlay: " .. vim.inspect(details))
assert_true(details.virt_lines_above == true, "title must sit above the first buffer line: " .. vim.inspect(details))
assert_true(details.virt_text == nil, "title must not use virt_text overlay: " .. vim.inspect(details))
assert_true(details.virt_lines[1][1][1] == "Virt line title", "title text missing: " .. vim.inspect(details))

-- The first body line must still be the buffer's line 1, untouched by the title.
assert_true(vim.api.nvim_buf_get_lines(buf, 0, 1, false)[1] == "first body line", "body line 1 was overwritten")

-- copy_title copies the cached title string into the + register. Headless CI has no clipboard
-- provider, so capture the value passed to setreg instead of reading the register back.
local copied
local orig_setreg = vim.fn.setreg
vim.fn.setreg = function(reg, val)
   if reg == "+" then
      copied = val
   end
   return orig_setreg(reg, val)
end
require("track.title").copy_title()
assert_true(copied == "Virt line title", "copy_title did not copy the title: " .. vim.inspect(copied))

print("track-e2e: PASS nvim title virtline")
vim.cmd("qa!")
