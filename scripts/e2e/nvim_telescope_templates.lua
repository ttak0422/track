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

vim.fn.mkdir(vault .. "/template", "p")
vim.fn.writefile({
   "<!-- track-template",
   "name: daily",
   "-->",
   "# {{ title }}",
}, vault .. "/template/100.template.md")

local exports = require("telescope").extensions.track
assert_true(type(exports.search_templates) == "function", "search_templates export is missing")
assert_true(vim.tbl_contains(vim.fn.getcompletion("Track temp", "cmdline"), "templates"), ":Track templates completion is missing")

local ok, err = pcall(exports.search_templates, { query = "daily" })
assert_true(ok, "search_templates failed: " .. tostring(err))

vim.defer_fn(function()
   print("track-e2e: PASS telescope templates")
   vim.cmd("qa!")
end, 500)
