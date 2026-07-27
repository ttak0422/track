-- Regression: typing a Japanese note title inside [[ must keep completion alive. nvim-cmp's own gate
-- checks the last *byte* before the cursor against the server's ASCII trigger characters and falls
-- back to an ASCII-only keyword pattern, so the first multibyte character used to reset the session
-- and nothing ever re-opened the popup while the title was typed or deleted. The plugin now re-opens
-- completion manually on multibyte input inside an unclosed [[ (and leaves the alias part after "|"
-- alone, where the server offers nothing).
--
-- Typed keys only drain in the main loop — vim.wait() does not consume typeahead, and feedkeys("x!")
-- blocks in insert mode waiting for the next character — so the scenario runs as a timer-driven step
-- chain after this file returns: each step feeds keys via nvim_input and polls its assertion.

local function fail(message)
   print("track-e2e: FAIL: " .. message)
   vim.cmd("cquit 1")
end

local function assert_true(ok, message)
   if not ok then
      fail(message)
   end
end

local cmp = require("cmp")
local uv = vim.uv or vim.loop

local steps = {}
local step_index = 0
local function next_step()
   step_index = step_index + 1
   local fn = steps[step_index]
   if not fn then
      print("track-e2e: PASS nvim link completion retrigger")
      vim.cmd("qa!")
      return
   end
   local ok, err = pcall(fn, next_step)
   if not ok then
      fail("step " .. step_index .. " error: " .. tostring(err))
   end
end

-- Polls cond every 50ms until it holds, then continues the chain; fails the run on timeout.
local function wait_until(cond, message, cont)
   local deadline = uv.now() + 8000
   local function poll()
      local ok, res = pcall(cond)
      if ok and res then
         return cont()
      end
      if uv.now() > deadline then
         return fail(message)
      end
      vim.defer_fn(poll, 50)
   end
   poll()
end

local function has_japanese_entry()
   for _, entry in ipairs(cmp.get_entries()) do
      if entry.completion_item.label == "日本語ノート" then
         return true
      end
   end
   return false
end

-- Feeds one key batch and lets the main loop settle before continuing, so each batch yields its own
-- TextChangedI with a changed cmp context. One combined batch would not: cmp's InsertEnter handler is
-- deferred a tick, consumes the whole batch's context first, and the trailing TextChangedI then reads
-- as "unchanged" — scripted input only, interactive typing never batches like that.
local function key_step(keys, settle_ms)
   table.insert(steps, function(cont)
      vim.api.nvim_input(keys)
      vim.defer_fn(cont, settle_ms or 200)
   end)
end

-- Opening [[ triggers completion through cmp's native trigger-character path.
key_step("o")
key_step("[")
table.insert(steps, function(cont)
   vim.api.nvim_input("[")
   wait_until(cmp.visible, "completion did not open on [[", cont)
end)

-- A Japanese character used to kill the session for good; the re-trigger must bring the popup back
-- with the Japanese title as a candidate.
table.insert(steps, function(cont)
   vim.api.nvim_input("日")
   wait_until(function()
      return cmp.visible() and has_japanese_entry()
   end, "completion did not re-open after a Japanese character (regression)", cont)
end)

-- Over-typing to a non-matching prefix empties the popup; deleting back must revive it.
key_step("日", 400)
table.insert(steps, function(cont)
   vim.api.nvim_input("<BS>")
   wait_until(function()
      return cmp.visible() and has_japanese_entry()
   end, "completion did not re-open after deleting back to a matching prefix (regression)", cont)
end)

-- Past the alias pipe the server offers nothing, so the re-trigger must leave the popup closed.
table.insert(steps, function(cont)
   vim.api.nvim_input("<C-e>|日")
   vim.defer_fn(function()
      assert_true(not cmp.visible(), "completion must stay closed after the alias pipe")
      cont()
   end, 1000)
end)

local vault = vim.env.TRACK_VAULT
assert_true(vault and vault ~= "", "TRACK_VAULT is required")

local client = require("track.client")

-- The Japanese title is the completion candidate; Seed is the note being edited.
local target, terr = client.run_json({ "open", "--title", "日本語ノート" })
assert_true(target ~= nil, "create 日本語ノート failed: " .. tostring(terr))
local seed, serr = client.run_json({ "open", "--title", "Seed" })
assert_true(seed ~= nil, "create Seed failed: " .. tostring(serr))

vim.cmd("edit " .. vim.fn.fnameescape(seed.path))
vim.bo.filetype = "markdown"
local buf = vim.api.nvim_get_current_buf()
local attached = vim.wait(5000, function()
   return #vim.lsp.get_clients({ bufnr = buf, name = "track-lsp" }) > 0
end, 50)
assert_true(attached, "track-lsp did not attach")

-- Watchdog: a step chain that stalls without failing must not hang the harness forever.
vim.defer_fn(function()
   fail("scenario did not finish within 60s (stalled at step " .. step_index .. ")")
end, 60000)

next_step()
