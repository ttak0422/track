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

-- Target is the note we want backlinks for; Source links to it, so it should show up as a backlink.
local target, terr = client.run_json({ "open", "--title", "Target" })
assert_true(target ~= nil, "create Target failed: " .. tostring(terr))
local source, serr = client.run_json({ "open", "--title", "Source", "--body", "see [[Target]]" })
assert_true(source ~= nil, "create Source failed: " .. tostring(serr))

-- Open the Target note and wait for the LSP server to attach.
vim.cmd.edit(vim.fn.fnameescape(target.path))
vim.bo.filetype = "markdown"
local bufnr = vim.api.nvim_get_current_buf()
local attached = vim.wait(5000, function()
   return #vim.lsp.get_clients({ bufnr = bufnr, name = "track-lsp" }) > 0
end, 50)
assert_true(attached, "track-lsp did not attach")

-- The custom request carries the source note's title, so backlinks can be listed by title rather than
-- the opaque epoch filename. This is what lets the file naming scheme stay epoch.md.
local result
require("track.backlinks").request(function(res)
   result = res
end)
local got = vim.wait(5000, function()
   return result ~= nil
end, 50)
assert_true(got, "track/backlinks request timed out")
assert_true(type(result) == "table" and #result >= 1, "expected at least one backlink, got " .. vim.inspect(result))

local found_title
for _, backlink in ipairs(result) do
   if backlink.title == "Source" then
      found_title = true
   end
end
assert_true(found_title, "backlink did not carry the source title: " .. vim.inspect(result))

-- The Telescope picker export must exist so :Track backlinks can prefer it.
local exports = require("telescope").extensions.track
assert_true(type(exports.backlinks) == "function", "backlinks export is missing")
local ok, err = pcall(exports.backlinks, {})
assert_true(ok, "backlinks picker failed: " .. tostring(err))

vim.defer_fn(function()
   print("track-e2e: PASS backlinks")
   vim.cmd("qa!")
end, 500)
