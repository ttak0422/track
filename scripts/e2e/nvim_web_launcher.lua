-- Regression: :Track web must open the browser on the note being read even when the configured
-- vault_dir is a symlink to the real vault (TRACK_VAULT points at one here, and the plugin's
-- config does the same). The launcher compared the buffer's canonical vault against the raw
-- configured path, so a link never matched and the browser opened the landing page instead.
-- TRACK_VAULT is expected to be a symlink: CI sets it to a link over a real temporary vault.

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
local created, cerr = client.run_json({ "open", "--title", "Web launcher target", "--body", "launch me" })
assert_true(created ~= nil, "create note failed: " .. tostring(cerr))

vim.cmd.edit(vim.fn.fnameescape(created.path))
local note_id = tonumber(created.path:match("note/(%d+)%.md$")) or tonumber(created.path:match("journal/(%d+)%.md$"))
assert_true(note_id ~= nil, "cannot read the note id from the created path: " .. tostring(created.path))

-- Capture the URL the launcher hands to the browser; headless runs have no opener to show it to.
local opened
local orig_open = vim.ui.open
vim.ui.open = function(url)
   opened = url
end

require("track.web").open({ "127.0.0.1:18765" })
vim.wait(20000, function()
   return opened ~= nil
end, 100)
vim.ui.open = orig_open

assert_true(opened ~= nil, ":Track web never opened a browser URL")
assert_true(
   opened == "http://127.0.0.1:18765/notes/" .. note_id,
   "browser should open the note being read, got: " .. tostring(opened)
)

print("track-e2e: PASS nvim web launcher")
vim.cmd("qa!")
