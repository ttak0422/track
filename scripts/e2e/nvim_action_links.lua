local function fail(message)
   print("track-e2e: FAIL: " .. message)
   vim.cmd("cquit 1")
end

local function assert_true(ok, message)
   if not ok then
      fail(message)
   end
end

local function completion_item(items, label)
   for _, item in ipairs(items or {}) do
      if item.label == label then
         return item
      end
   end
end

local function edit_text(item)
   local edit = item and item.textEdit
   return edit and edit.newText or nil
end

local vault = vim.env.TRACK_VAULT
assert_true(vault and vault ~= "", "TRACK_VAULT is required")

vim.fn.mkdir(vault .. "/note", "p")
vim.fn.mkdir(vault .. "/journal", "p")

local seed = vault .. "/note/100.md"
vim.fn.writefile({ "# Seed", "", "[x](<" }, seed)
vim.cmd.edit(vim.fn.fnameescape(seed))
vim.bo.filetype = "markdown"

local bufnr = vim.api.nvim_get_current_buf()
local attached = vim.wait(5000, function()
   return #vim.lsp.get_clients({ bufnr = bufnr, name = "track-lsp" }) > 0
end, 50)
assert_true(attached, "track-lsp did not attach")

local completion_result
local completion_error
vim.lsp.buf_request(bufnr, "textDocument/completion", {
   textDocument = { uri = vim.uri_from_bufnr(bufnr) },
   position = { line = 2, character = #"[x](<" },
}, function(err, result)
   completion_error = err
   completion_result = result
end)

local completed = vim.wait(5000, function()
   return completion_result ~= nil or completion_error ~= nil
end, 50)
assert_true(completed, "completion request timed out")
assert_true(completion_error == nil, "completion error: " .. vim.inspect(completion_error))

local items = completion_result.items or completion_result
local note = completion_item(items, "note")
local journal = completion_item(items, "journal")
assert_true(note ~= nil, "note action completion is missing")
assert_true(journal ~= nil, "journal action completion is missing")
assert_true(edit_text(note) == "note?title={{date}} $0>)", "unexpected note completion: " .. vim.inspect(note))
assert_true(edit_text(journal) == "journal?offset=0>)", "unexpected journal completion: " .. vim.inspect(journal))
assert_true(note.insertTextFormat == 2, "note completion should use snippet format")
assert_true(journal.insertTextFormat == 2, "journal completion should use snippet format")

local parsed = require("track.action").parse_action("note?title={{date}}/{{journal}}")
assert_true(parsed ~= nil, "action parse failed")
local date_value, journal_value = parsed.params.title:match("^([^/]+)/([^/]+)$")
assert_true(date_value == journal_value, "{{date}} and {{journal}} should match: " .. tostring(parsed.params.title))
assert_true(date_value:match("^%d%d%d%d%d%d%d%d$") ~= nil, "date placeholder should be yyyyMMdd: " .. tostring(date_value))

require("track.action").run("note?title={{date}} E2E")
local today = os.date("%Y%m%d")
assert_true(vim.api.nvim_buf_get_name(0):match("/note/%d+%.md$") ~= nil, "note action did not open a note file")
local note_title = vim.api.nvim_buf_get_lines(0, 0, 1, false)[1]
assert_true(note_title == "# " .. today .. " E2E", "note action created unexpected title: " .. tostring(note_title))

require("track.action").run("journal?offset=0")
assert_true(vim.api.nvim_buf_get_name(0):sub(-#("/journal/" .. today .. ".md")) == "/journal/" .. today .. ".md", "journal action did not open today's journal")
local journal_title = vim.api.nvim_buf_get_lines(0, 0, 1, false)[1]
assert_true(journal_title == "# " .. today, "journal action created unexpected title: " .. tostring(journal_title))

print("track-e2e: PASS nvim action links")
vim.cmd("qa")
