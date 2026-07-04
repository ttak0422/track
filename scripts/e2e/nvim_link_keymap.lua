-- Regression: the buffer-local <CR> "follow link" map must survive a note being closed and reopened.
-- :bdelete unloads a buffer (firing BufUnload, not BufWipeout) and drops its buffer-local keymaps, then
-- Neovim reuses the same buffer number when the file is reopened. The attach guard used to skip re-setup
-- for that reused number, so the reopened note lost its <CR> map and Enter silently stopped following
-- links. attach now re-installs the keymaps on every call, so they come back on reopen.

local function fail(message)
   print("track-e2e: FAIL: " .. message)
   vim.cmd("cquit 1")
end

local function assert_true(ok, message)
   if not ok then
      fail(message)
   end
end

local function has_follow_cr(buf)
   for _, m in ipairs(vim.api.nvim_buf_get_keymap(buf, "n")) do
      if m.lhs == "<CR>" and type(m.desc) == "string" and m.desc:match("follow link") then
         return true
      end
   end
   return false
end

local function wait_attached(buf)
   return vim.wait(5000, function()
      return #vim.lsp.get_clients({ bufnr = buf, name = "track-lsp" }) > 0
   end, 50)
end

local vault = vim.env.TRACK_VAULT
assert_true(vault and vault ~= "", "TRACK_VAULT is required")
vim.fn.mkdir(vault .. "/note", "p")
local file = vault .. "/note/200.md"
vim.fn.writefile({ "# Seed", "", "[[Other]]" }, file)

vim.cmd("edit " .. vim.fn.fnameescape(file))
local buf1 = vim.api.nvim_get_current_buf()
assert_true(wait_attached(buf1), "track-lsp did not attach")
assert_true(has_follow_cr(buf1), "track <CR> follow map missing after first attach")

-- Move the window off the note, unload it, then reopen the same file.
vim.cmd("enew")
vim.cmd("bdelete " .. buf1)
vim.cmd("edit " .. vim.fn.fnameescape(file))
local buf2 = vim.api.nvim_get_current_buf()
assert_true(buf2 == buf1, "reopen should reuse the same buffer number (" .. buf1 .. " vs " .. buf2 .. ")")
assert_true(wait_attached(buf2), "track-lsp did not re-attach after reopen")
assert_true(has_follow_cr(buf2), "track <CR> follow map missing after reopen (regression)")

print("track-e2e: PASS nvim link keymap")
vim.cmd("qa!")
