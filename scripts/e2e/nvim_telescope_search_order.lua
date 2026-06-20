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

-- Both notes match the query "track" in their body, but only "track manual" also matches it in its
-- title, so a fuzzy sorter would score it best and float it to the top. We give that note the OLDEST
-- mtime and the unrelated "Newest" title the freshest, so the CLI's mtime-DESC ranking and a fuzzy
-- re-rank disagree:
--   CLI (mtime DESC):  Newest, track manual
--   fuzzy re-rank:     track manual, Newest   <- the bug
-- The picker must honor the CLI order; this is the regression guard for using an identity sorter
-- instead of generic_sorter on the search pickers.
local query = "track"
local specs = {
   { title = "Newest", body = "mentions track once", mtime = os.time() + 20 },
   { title = "track manual", body = "mentions track once", mtime = os.time() },
}
local order = { "Newest", "track manual" }

for _, spec in ipairs(specs) do
   local note, err = client.run_json({ "open", "--title", spec.title, "--body", spec.body })
   assert_true(note ~= nil, "create " .. spec.title .. " failed: " .. tostring(err))
   assert_true(vim.uv.fs_utime(note.path, spec.mtime, spec.mtime), "fs_utime failed for " .. spec.title)
end

local reindexed, rerr = client.run_json({ "reindex" })
assert_true(reindexed ~= nil, "reindex failed: " .. tostring(rerr))

-- The CLI is the source of truth for ordering; confirm it ranks by mtime DESC as expected.
local cli, cerr = client.run_json({ "search", "--scope", "body", "--query", query, "--limit", "50" })
assert_true(cli ~= nil, "cli body search failed: " .. tostring(cerr))
local cli_titles = {}
for _, r in ipairs(cli.results or {}) do
   table.insert(cli_titles, r.title)
end
assert_true(
   #cli_titles == 2 and cli_titles[1] == order[1] and cli_titles[2] == order[2],
   "cli order is not mtime DESC: " .. vim.inspect(cli_titles)
)

-- Drive the body picker with the query as its prompt and read back the rendered order.
require("track.telescope").search_body({ query = query })
local action_state = require("telescope.actions.state")
local prompt_bufnr = vim.api.nvim_get_current_buf()
local picker = action_state.get_current_picker(prompt_bufnr)
assert_true(picker ~= nil, "no active picker after search_body")

-- The dynamic finder shells out to the CLI asynchronously; wait until both hits are populated.
local ready = vim.wait(5000, function()
   return picker.manager and picker.manager:num_results() >= 2
end, 50)
assert_true(ready, "picker did not populate 2 results")

local picker_titles = {}
for i = 1, picker.manager:num_results() do
   local entry = picker.manager:get_entry(i)
   table.insert(picker_titles, entry.value and entry.value.title)
end
assert_true(
   #picker_titles >= 2 and picker_titles[1] == order[1] and picker_titles[2] == order[2],
   "picker order does not match cli mtime order: picker=" .. vim.inspect(picker_titles) .. " cli=" .. vim.inspect(cli_titles)
)

vim.defer_fn(function()
   print("track-e2e: PASS telescope search order")
   vim.cmd("qa!")
end, 500)
