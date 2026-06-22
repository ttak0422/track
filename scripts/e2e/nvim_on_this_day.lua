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

local journal, jerr = client.run_json({ "journal" })
assert_true(journal ~= nil, "create journal failed: " .. tostring(jerr))
local note, nerr = client.run_json({ "new", "--title", "Worked today", "--id", "100" })
assert_true(note ~= nil, "create note failed: " .. tostring(nerr))

vim.cmd.edit(vim.fn.fnameescape(journal.path))

local on_this_day = require("track.on_this_day")
assert_true(on_this_day.date_from_path(journal.path) ~= nil, "daily journal date was not detected")
local summary_path = journal.path:gsub("%d%d%.md$", ".md")
assert_true(on_this_day.date_from_path(summary_path) == nil, "summary journal should not match")

local notes
local date
on_this_day.request(function(result, result_date)
   notes = result
   date = result_date
end)
assert_true(type(notes) == "table", "agenda request did not return notes")
assert_true(type(date) == "string" and date ~= "", "agenda request did not return a date")

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
