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
pcall(vim.cmd.write)
assert_true(vim.api.nvim_win_is_valid(win), "popup should stay open on a validation error")

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
vim.cmd.write()
assert_true(not vim.api.nvim_win_is_valid(win), "popup should close after a successful save")

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

vim.defer_fn(function()
   print("track-e2e: PASS meta")
   vim.cmd("qa!")
end, 500)
