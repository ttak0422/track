-- Regression: <CR> on the leading "!" of a block include (![[...]]) must follow the include.
--
-- The include directive is a whole line; its link span starts at the "[[" (OpenByte), so the cursor
-- on the "!" — one byte before it — used to be "off the link": the <expr> map returned <CR> and a
-- newline was inserted instead of navigating. The follow gate (wiki_link_at_cursor) and the LSP
-- definition now both accept the leading "!" of a block include, while a "!" that merely precedes
-- an inline [[...]] is untouched.

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
local src = client.run_json({ "open", "--title", "Src", "--body", "![[Target]]" })
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

-- Cursor on the leading "!" (0-based byte 0): <CR> must follow the include to Target.
local buf = open_src()
vim.api.nvim_win_set_cursor(0, { 1, 0 })
press_cr()
assert_true(
   vim.wait(5000, function()
      return vim.api.nvim_buf_get_name(0) == target.path
   end, 50),
   "<CR> on the include bang did not follow it (landed on " .. vim.api.nvim_buf_get_name(0) .. ")"
)

-- An indented include: the "!" is still the first non-space byte, so it follows too.
local src2 = client.run_json({ "open", "--title", "Src2", "--body", "  ![[Target]]" })
assert_true(src2 ~= nil, "create Src2 failed")
vim.cmd.edit(vim.fn.fnameescape(src2.path))
vim.bo.filetype = "markdown"
local buf2 = vim.api.nvim_get_current_buf()
assert_true(
   vim.wait(5000, function()
      return #vim.lsp.get_clients({ bufnr = buf2, name = "track-lsp" }) > 0
   end, 50),
   "track-lsp did not attach to Src2"
)
vim.api.nvim_win_set_cursor(0, { 1, 2 }) -- on the "!" after two spaces
press_cr()
assert_true(
   vim.wait(5000, function()
      return vim.api.nvim_buf_get_name(0) == target.path
   end, 50),
   "<CR> on an indented include bang did not follow it (landed on "
      .. vim.api.nvim_buf_get_name(0)
      .. ")"
)

-- A "!" that only precedes an inline [[...]] (not a block include) must NOT navigate. It sits on
-- line 2 (not the title row, whose <CR> opens the metadata editor), so a non-follow <CR> stays on
-- the same note instead of jumping to the include target.
local src3 = client.run_json({ "open", "--title", "Src3", "--body", "# Src3\nsee ![[Target]]" })
assert_true(src3 ~= nil, "create Src3 failed")
vim.cmd.edit(vim.fn.fnameescape(src3.path))
vim.bo.filetype = "markdown"
local buf3 = vim.api.nvim_get_current_buf()
assert_true(
   vim.wait(5000, function()
      return #vim.lsp.get_clients({ bufnr = buf3, name = "track-lsp" }) > 0
   end, 50),
   "track-lsp did not attach to Src3"
)
vim.api.nvim_win_set_cursor(0, { 2, 4 }) -- on the "!" in line 2 "see ![[...]]"
press_cr()
vim.wait(500, function()
   return vim.api.nvim_buf_get_name(0) ~= src3.path
end, 50)
assert_true(
   vim.api.nvim_buf_get_name(0) == src3.path,
   "inline non-block bang must not follow; navigated away to " .. vim.api.nvim_buf_get_name(0)
)

print("track-e2e: PASS nvim include <CR>")
vim.cmd("qa!")
