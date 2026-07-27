-- Regression: the checkbox conceal glyph must come back when the cursor leaves the buffer. The cursor
-- row is deliberately revealed (raw "- [ ]" instead of "☐") for editing, but every repaint trigger is
-- a buffer-local autocmd that only fires while the buffer is current, so leaving the buffer used to
-- strand that row un-concealed in any window still showing the note until it was revisited. A BufLeave
-- repaint now restores the glyph, and BufEnter renders directly so re-entering reveals the cursor row
-- again without waiting for the debounced LSP refresh.

local function fail(message)
   print("track-e2e: FAIL: " .. message)
   vim.cmd("cquit 1")
end

local function assert_true(ok, message)
   if not ok then
      fail(message)
   end
end

local ns = vim.api.nvim_create_namespace("track_lsp_links")

local function has_glyph(buf, row)
   local marks = vim.api.nvim_buf_get_extmarks(buf, ns, { row, 0 }, { row, -1 }, { details = true })
   for _, mark in ipairs(marks) do
      if mark[4].conceal == "☐" then
         return true
      end
   end
   return false
end

local vault = vim.env.TRACK_VAULT
assert_true(vault and vault ~= "", "TRACK_VAULT is required")
vim.fn.mkdir(vault .. "/note", "p")
local file = vault .. "/note/300.md"
vim.fn.writefile({ "- [ ] zero", "", "- [ ] one", "- [ ] two" }, file)

vim.cmd("edit " .. vim.fn.fnameescape(file))
local buf = vim.api.nvim_get_current_buf()
local attached = vim.wait(5000, function()
   return #vim.lsp.get_clients({ bufnr = buf, name = "track-lsp" }) > 0
end, 50)
assert_true(attached, "track-lsp did not attach")

-- Initial paint: cursor sits on row 0, so its checkbox is revealed while the others conceal.
assert_true(vim.wait(5000, function()
   return has_glyph(buf, 2) and has_glyph(buf, 3)
end, 50), "initial conceal glyphs did not appear")
assert_true(not has_glyph(buf, 0), "cursor row should be revealed on initial paint")

-- Let attach's deferred refresh (debounce_ms * 4) drain, so no stray repaint races the assertions below.
vim.wait(800)

-- Moving the cursor onto another task row shifts the reveal there. Headless cursor sets do not fire
-- CursorMoved themselves, so fire the autocmd the way a real cursor move would.
vim.api.nvim_win_set_cursor(0, { 3, 0 })
vim.api.nvim_exec_autocmds("CursorMoved", { buffer = buf })
assert_true(has_glyph(buf, 0), "row 0 should conceal after the cursor left it")
assert_true(not has_glyph(buf, 2), "cursor row 2 should be revealed")

-- Leaving the buffer fires BufLeave; the scheduled repaint must conceal the abandoned cursor row.
vim.cmd("enew")
assert_true(vim.wait(2000, function()
   return has_glyph(buf, 2)
end, 50), "checkbox conceal did not return after leaving the buffer (regression)")

-- Re-entering repaints one tick later (after the remembered cursor is restored): the cursor row
-- reveals again and the window conceals.
vim.cmd("buffer " .. buf)
local row = vim.api.nvim_win_get_cursor(0)[1] - 1
assert_true(vim.wait(2000, function()
   return not has_glyph(buf, row)
end, 50), "cursor row should be revealed on re-enter")
assert_true(
   vim.api.nvim_get_option_value("conceallevel", { win = 0 }) == 2,
   "conceallevel should be applied on buffer enter"
)

print("track-e2e: PASS nvim task conceal")
vim.cmd("qa!")
