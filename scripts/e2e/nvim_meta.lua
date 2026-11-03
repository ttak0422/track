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

local created, cerr = client.run_json({ "open", "--title", "Meta target" })
assert_true(created ~= nil, "create note failed: " .. tostring(cerr))

vim.cmd.edit(vim.fn.fnameescape(created.path))
-- Keep a second ordinary window so an erroneous :wq of the source cannot exit headless
-- Neovim before the assertions below run. The active source window itself must survive.
vim.cmd.vsplit()
local source_win = vim.api.nvim_get_current_win()

-- Open the metadata popup: a floating YAML buffer seeded with the full editable document.
require("track.meta").edit()
local buf = vim.api.nvim_get_current_buf()
local win = vim.api.nvim_get_current_win()
assert_true(vim.api.nvim_buf_get_name(buf):find("^track://meta/") ~= nil, "meta popup buffer not focused")
local seeded = table.concat(vim.api.nvim_buf_get_lines(buf, 0, -1, false), "\n")
for _, key in ipairs({ "title: Meta target", "tags:", "description:", "image:", "props:" }) do
   assert_true(seeded:find(key, 1, true) ~= nil, "seed document missing " .. key .. ":\n" .. seeded)
end
assert_true(seeded:find("{}", 1, true) == nil, "empty fields must render bare, not {}:\n" .. seeded)

-- A document rejected by the engine (image is not a vault asset) keeps the popup open. The
-- error-level notify surfaces as an error in headless mode, so the write is pcall-wrapped.
vim.api.nvim_buf_set_lines(buf, 0, -1, false, {
   "tags:",
   "  - go",
   "image: assets/nope.png",
})
pcall(vim.cmd, "wq")
assert_true(vim.api.nvim_win_is_valid(win), "popup should stay open on a validation error")
assert_true(vim.api.nvim_win_is_valid(source_win), "failed :wq must preserve the source window")
assert_true(vim.bo[buf].modified, "failed :wq must preserve the metadata draft")

-- A valid document (a new title, tags with a duplicate, a description, and props) saves and
-- closes the popup; the title change goes through the rename path.
vim.api.nvim_buf_set_lines(buf, 0, -1, false, {
   "# comment lines are ignored by the engine",
   "title: Meta target v2",
   "tags:",
   "  - go",
   "  - go",
   "  - lua",
   "description: an e2e summary",
   "props:",
   "  status: draft",
   "  rating: 8",
})
local write_ok, write_err = pcall(vim.cmd, "wq")
assert_true(write_ok, ":wq failed: " .. tostring(write_err))
assert_true(vim.api.nvim_win_is_valid(source_win), ":wq closed the source window")
assert_true(not vim.api.nvim_win_is_valid(win), "popup should close after a successful :wq")
assert_true(vim.api.nvim_buf_is_valid(buf) == false, "meta buffer should be wiped after :wq")

-- The engine round-trips the document: title renamed, tags deduplicated, props typed.
local meta, merr = client.run_json({ "meta", "--path", created.path })
assert_true(meta ~= nil, "read meta failed: " .. tostring(merr))
assert_true(meta.title == "Meta target v2", "title did not round-trip: " .. vim.inspect(meta.title))
assert_true(
   type(meta.tags) == "table" and #meta.tags == 2 and meta.tags[1] == "go" and meta.tags[2] == "lua",
   "tags did not round-trip deduplicated: " .. vim.inspect(meta.tags)
)
assert_true(meta.description == "an e2e summary", "description did not round-trip: " .. vim.inspect(meta.description))
assert_true(
   type(meta.props) == "table" and meta.props.status == "draft" and meta.props.rating == 8,
   "props did not round-trip: " .. vim.inspect(meta.props)
)

-- Plain :w still closes the popup on the next event loop turn, without closing the source.
require("track.meta").edit()
local write_buf = vim.api.nvim_get_current_buf()
local write_win = vim.api.nvim_get_current_win()
vim.api.nvim_buf_set_lines(write_buf, 0, -1, false, { "description: another summary" })
vim.cmd.write()
assert_true(vim.wait(1000, function() return not vim.api.nvim_win_is_valid(write_win) end), ":w should close the popup")
assert_true(vim.api.nvim_win_is_valid(source_win), ":w must preserve the source window")

-- A failed write from an unchanged popup must also abort :wq instead of closing it.
require("track.meta").edit()
local unchanged_buf = vim.api.nvim_get_current_buf()
local unchanged_win = vim.api.nvim_get_current_win()
assert_true(not vim.bo[unchanged_buf].modified, "new popup should be unmodified")
-- Schedule a successful close, then fail a later write before that callback runs.
vim.cmd.write()
local real_run_json = client.run_json
client.run_json = function(args, input)
   if args[1] == "meta" and args[4] == "--edit" then
      return nil, "simulated save failure"
   end
   return real_run_json(args, input)
end
local failed_ok, failed_err = pcall(vim.cmd, "wq")
client.run_json = real_run_json
vim.wait(100, function() return false end)
assert_true(not failed_ok, "unchanged failed :wq must report an error")
assert_true(tostring(failed_err):find("simulated save failure", 1, true), "wrong failure: " .. tostring(failed_err))
assert_true(vim.api.nvim_win_is_valid(unchanged_win), "unchanged failed :wq must keep popup")
assert_true(vim.api.nvim_win_is_valid(source_win), "unchanged failed :wq must keep source")
vim.cmd("q!")

-- An edit made before the deferred close is a new draft, and must not be discarded.
require("track.meta").edit()
local draft_buf = vim.api.nvim_get_current_buf()
local draft_win = vim.api.nvim_get_current_win()
vim.api.nvim_buf_set_lines(draft_buf, 0, -1, false, { "description: saved once" })
vim.cmd.write()
vim.api.nvim_buf_set_lines(draft_buf, 0, -1, false, { "description: newer draft" })
vim.wait(100, function() return false end)
assert_true(vim.api.nvim_win_is_valid(draft_win), "deferred close discarded a newer draft")
assert_true(vim.bo[draft_buf].modified, "newer draft lost its modified state")
assert_true(vim.api.nvim_buf_get_lines(draft_buf, 0, 1, false)[1] == "description: newer draft", "newer draft text changed")
vim.cmd("q!")

vim.defer_fn(function()
   print("track-e2e: PASS meta")
   vim.cmd("qa!")
end, 500)
