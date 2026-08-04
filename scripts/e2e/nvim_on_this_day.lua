local function fail(message)
   print("track-e2e: FAIL: " .. message)
   vim.cmd("cquit 1")
end

local function assert_true(ok, message)
   if not ok then
      fail(message)
   end
end

local client = require("track.client")

local note, nerr = client.run_json({ "new", "--title", "Worked today", "--id", "100" })
assert_true(note ~= nil, "create note failed: " .. tostring(nerr))

local expected_journal_path = vim.env.TRACK_VAULT .. "/journal/" .. os.date("%Y%m%d") .. ".md"
assert_true(vim.fn.delete(expected_journal_path) == 0, "could not remove setup journal")
assert_true(vim.fn.filereadable(expected_journal_path) == 0, "test journal already exists")
vim.cmd.edit(vim.fn.fnameescape(note.path))

local on_this_day = require("track.on_this_day")

local notes
local date
on_this_day.request(function(result, result_date)
   notes = result
   date = result_date
end)
assert_true(type(notes) == "table", "agenda request did not return notes")
assert_true(type(date) == "string" and date ~= "", "agenda request did not return a date")

local journal, jerr = client.run_json({ "journal" })
assert_true(journal ~= nil, "on_this_day did not create journal: " .. tostring(jerr))
assert_true(
   vim.fn.resolve(journal.path) == vim.fn.resolve(expected_journal_path),
   "unexpected generated journal path: " .. journal.path
)
assert_true(on_this_day.date_from_path(journal.path) ~= nil, "daily journal date was not detected")
local summary_path = journal.path:gsub("%d%d%.md$", ".md")
assert_true(on_this_day.date_from_path(summary_path) == nil, "summary journal should not match")

local found
for _, item in ipairs(notes) do
   if item.title == "Worked today" and item.path == note.path then
      found = true
   end
end
assert_true(found, "On this day did not include the active note: " .. vim.inspect(notes))

on_this_day.show()
local qf = vim.fn.getqflist({ title = 1, items = 1 })
assert_true(qf.title == "track on this day: " .. date, "unexpected quickfix title: " .. tostring(qf.title))
assert_true(#qf.items >= 1, "quickfix list is empty")

local exports = require("telescope").extensions.track
assert_true(type(exports.on_this_day) == "function", "on_this_day Telescope export is missing")

print("track-e2e: PASS on this day")
vim.cmd("qa!")
