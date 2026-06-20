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
assert_true(vim.fn.isdirectory(vault .. "/note") == 0, "initial vault should not have note dir")
assert_true(vim.fn.isdirectory(vault .. "/journal") == 0, "initial vault should not have journal dir")

vim.cmd.enew()
local seed = vault .. "/note/100.md"
vim.api.nvim_buf_set_name(0, seed)
vim.api.nvim_buf_set_lines(0, 0, -1, false, { "# Seed", "", "[x](<" })
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

local action_vault = vim.fn.tempname()
local action_cache = vim.fn.tempname()
local config = require("track.config")
config.options.vault_dir = action_vault
config.options.cache_dir = action_cache
vim.env.TRACK_VAULT = action_vault
vim.env.TRACK_CACHE_DIR = action_cache
local client = require("track.client")
local function run_json(args)
   local data, err = client.run_json(args)
   assert_true(data ~= nil, table.concat(args, " ") .. ": " .. tostring(err))
   return data
end
assert_true(vim.fn.isdirectory(action_vault .. "/note") == 0, "fresh action vault should not have note dir")
assert_true(vim.fn.isdirectory(action_vault .. "/journal") == 0, "fresh action vault should not have journal dir")

require("track.action").run("note?title={{date}} E2E")
local today = os.date("%Y%m%d")
assert_true(vim.fn.isdirectory(action_vault .. "/note") == 1, "note action should create note dir")
assert_true(vim.api.nvim_buf_get_name(0):match("/note/%d+%.md$") ~= nil, "note action did not open a note file")
-- With no explicit template, creation applies the builtin "default" template (# {{ title }}).
local note_lines = vim.api.nvim_buf_get_lines(0, 0, -1, false)
assert_true(
   #note_lines == 1 and note_lines[1] == "# " .. today .. " E2E",
   "note action should apply the builtin default template: " .. vim.inspect(note_lines)
)
local note_resolved = run_json({ "resolve", "--term", today .. " E2E" })
assert_true(note_resolved.found == true, "note action should write sidecar title: " .. vim.inspect(note_resolved))

assert_true(vim.fn.isdirectory(action_vault .. "/journal") == 0, "journal dir should not exist before journal action")
require("track.action").run("journal?offset=0")
assert_true(vim.fn.isdirectory(action_vault .. "/journal") == 1, "journal action should create journal dir")
assert_true(vim.api.nvim_buf_get_name(0):sub(-#("/journal/" .. today .. ".md")) == "/journal/" .. today .. ".md", "journal action did not open today's journal")
-- With no explicit template, creation applies the builtin "journal" template (# {{ title }} + {{ date }}).
local journal_lines = vim.api.nvim_buf_get_lines(0, 0, -1, false)
local iso_date = os.date("%Y-%m-%d")
assert_true(
   #journal_lines == 3
      and journal_lines[1] == "# " .. today
      and journal_lines[2] == ""
      and journal_lines[3] == iso_date,
   "journal action should apply the builtin journal template: " .. vim.inspect(journal_lines)
)
local journal_resolved = run_json({ "resolve", "--term", today })
assert_true(journal_resolved.found == true, "journal action should write sidecar title: " .. vim.inspect(journal_resolved))

print("track-e2e: PASS nvim action links")
vim.cmd("qa!")
