-- Regression: <CR> on the note's first line.
--
-- The map is an <expr> map, so its callback runs under textlock. The first-line branch used to call
-- the metadata editor straight from there, which opens a buffer and a window — E565, surfaced as
-- E5108 — and it ran before the follow branch, so a [[link]] written on line 1 could never be
-- followed at all. Enter now follows first and defers the popup.

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
local target = client.run_json({ "open", "--title", "Target" })
assert_true(target ~= nil, "create Target failed")
-- Both links sit on line 1, after multibyte text, which is where the report came from.
local src = client.run_json({ "open", "--title", "Src", "--body", "テスト [[Target]], [[Target]]" })
assert_true(src ~= nil, "create Src failed")

local function open_src()
   vim.cmd.edit(vim.fn.fnameescape(src.path))
   vim.bo.filetype = "markdown"
   local buf = vim.api.nvim_get_current_buf()
   assert_true(
      vim.wait(5000, function()
         return #vim.lsp.get_clients({ bufnr = buf, name = "track-lsp" }) > 0
      end, 50),
      "track-lsp did not attach"
   )
   return buf
end

-- Press Enter for real: an <expr> map fed through feedkeys runs under the same textlock the bug hit.
local function press_cr()
   vim.api.nvim_feedkeys(vim.api.nvim_replace_termcodes("<CR>", true, false, true), "x", false)
end

local buf = open_src()
local line = vim.api.nvim_buf_get_lines(buf, 0, 1, false)[1] or ""
local first = line:find("%[%[[^%[%]]+%]%]")
assert_true(first ~= nil, "fixture line carries no link: " .. line)
local second = select(2, line:find("%]%], ", first))

-- Line 1, on the first link: follows instead of opening the metadata popup.
vim.api.nvim_win_set_cursor(0, { 1, first + 1 })
press_cr()
assert_true(
   vim.wait(5000, function()
      return vim.api.nvim_buf_get_name(0) == target.path
   end, 50),
   "<CR> on a line-1 link did not follow it (landed on " .. vim.api.nvim_buf_get_name(0) .. ")"
)

-- Line 1, on the second link: the one after the comma follows too.
open_src()
vim.api.nvim_win_set_cursor(0, { 1, second + 1 })
press_cr()
assert_true(
   vim.wait(5000, function()
      return vim.api.nvim_buf_get_name(0) == target.path
   end, 50),
   "<CR> on the second line-1 link did not follow it"
)

-- Line 1, off any link: the metadata editor still opens, and does so without E565.
open_src()
vim.api.nvim_win_set_cursor(0, { 1, 0 })
press_cr()
assert_true(
   vim.wait(5000, function()
      return vim.api.nvim_buf_get_name(0):match("track://meta/") ~= nil
   end, 50),
   "<CR> on line 1 off a link did not open the metadata editor (buffer "
      .. vim.api.nvim_buf_get_name(0)
      .. ")"
)

print("track-e2e: PASS nvim first-line <CR>")
vim.cmd("qa!")
