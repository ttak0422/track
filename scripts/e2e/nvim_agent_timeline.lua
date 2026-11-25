-- Real Neovim -> vim.system -> Track JSON integration. Run from the repository root:
-- TRACK_TEST_BIN=/absolute/path/to/track nvim --headless -u NONE -i NONE -n -l scripts/e2e/nvim_agent_timeline.lua
-- Every write is confined to a newly created temporary vault, never the caller's vault.
vim.opt.rtp:prepend(vim.fn.getcwd())
local root = vim.fn.tempname()
vim.fn.mkdir(root .. "/vault/note", "p")
vim.fn.mkdir(root .. "/home", "p")
vim.env.TRACK_VAULT = root .. "/vault"
vim.env.TRACK_CACHE_DIR = root .. "/cache"
vim.env.TRACK_CONFIG = root .. "/missing-config.yml"
vim.env.HOME = root .. "/home"
vim.env.XDG_CONFIG_HOME = root .. "/config"
vim.env.XDG_CACHE_HOME = root .. "/cache"
vim.o.swapfile = false

local function await(start)
   local finished, failure, result = false, nil, nil
   start(function(err, data)
      failure, result, finished = err, data, true
   end)
   assert(vim.wait(20000, function() return finished end, 10), "timeline callback timed out")
   return failure, result
end

local function run()
   assert(vim.system, "Neovim with vim.system is required")
   local config = require("track.config")
   config.setup({ bin = vim.env.TRACK_TEST_BIN or "track", vault_dir = root .. "/vault" })
   local binary = require("track.client").bin()
   assert(vim.fn.executable(binary) == 1, "real Track binary is required")
   if vim.env.TRACK_TEST_BIN then
      assert(vim.fn.resolve(vim.fn.fnamemodify(binary, ":p")) == vim.fn.resolve(vim.fn.fnamemodify(vim.env.TRACK_TEST_BIN, ":p")),
         "bundled Track binary overrides TRACK_TEST_BIN; build the intended binary at that location")
   end
   local initialized = vim.system({ binary, "init" }, { text = true, timeout = 15000 }):wait()
   assert(initialized.code == 0, initialized.stderr)
   local model = require("track.agent_timeline")
   local view = model.new("e2e-project", { vault_dir = root .. "/vault" })
   local function apply(request)
      local err, data = await(function(done) view.apply(request, done) end)
      assert(not err, err)
      assert(data and data.project_id == "e2e-project", "missing mutation receipt")
      return data
   end
   local created = apply(view.request("project.create", { title = "Integration project", mode = "serial" }))
   assert(created.revision == 1, "project creation revision")
   local task = { id = "first-task", title = "Real process task", status = "active", owner = "nvim", weight = 1,
      done_minutes = 2, remaining_minutes = 3, mode = "serial" }
   local request = view.request("task.upsert", task)
   assert(apply(request).revision == 2, "task creation revision")
   assert(apply(request).revision == 2, "identical retry must not append an event")
   local err, snapshot = await(function(done) view.refresh(done) end)
   assert(not err and snapshot.revision == 2 and #snapshot.tasks == 1, err or "refresh did not read saved task")
   assert(view.rows()[2]:find("[active] Real process task", 1, true), "model task rendering")

   -- Another real CLI writer advances the ledger after this view constructs a request.
   local stale = view.request("project.update", { title = "Must not overwrite", mode = "serial" })
   local external = { source = "e2e-external", event_id = "advance", expected_revision = 2, kind = "project.update",
      payload = { title = "Externally updated project", mode = "parallel" } }
   local process = vim.system({ binary, "timeline-project", "apply", "--id", "e2e-project" },
      { text = true, stdin = vim.json.encode(external), timeout = 15000 }):wait()
   assert(process.code == 0 and process.signal == 0, process.stderr)
   assert(vim.json.decode(process.stdout).revision == 3, "external JSON mutation failed")
   err = await(function(done) view.apply(stale, done) end)
   assert(err and err:find("conflict", 1, true), "stale write must return revision conflict")
   assert(view.snapshot.revision == 2 and view.error, "failed write replaced the cached snapshot")
   err, snapshot = await(function(done) view.refresh(done) end)
   assert(not err and snapshot.revision == 3 and snapshot.title == "Externally updated project", "refresh after conflict")
   assert(#snapshot.events == 3, "refused write or retry added an event")
   local list_error, listed = await(function(done) model.list({ vault_dir = root .. "/vault" }, done) end)
   assert(not list_error and #listed.projects == 1 and listed.projects[1].project_id == "e2e-project", "project listing")

   require("track.commands").setup()
   local windows_before = #vim.api.nvim_list_wins()
   vim.cmd("Track timeline e2e-project")
   local buf = vim.api.nvim_get_current_buf()
   assert(vim.api.nvim_buf_get_name(buf):find("track-timeline://e2e-project/", 1, true), "command did not open timeline")
   assert(vim.wait(20000, function()
      return table.concat(vim.api.nvim_buf_get_lines(buf, 0, -1, false), "\n"):find("Revision 3", 1, true) ~= nil
   end, 10), "timeline command did not render CLI snapshot")
   local rendered = table.concat(vim.api.nvim_buf_get_lines(buf, 0, -1, false), "\n")
   for _, expected in ipairs({ "Externally updated project", "[active] Real process task", "## Reports", "## Discussion" }) do
      assert(rendered:find(expected, 1, true), "missing timeline content: " .. expected)
   end
   for _, hidden in ipairs({ "## Recorded events", "Payload:", " events: " }) do
      assert(not rendered:find(hidden, 1, true), "unexpected raw journal detail: " .. hidden)
   end
   assert(not vim.bo[buf].modifiable and not vim.bo[buf].modified, "timeline should be read-only")
   -- The open native view observes an independent writer without manual refresh.
   external.event_id, external.expected_revision = "monitor", 3
   external.payload.title = "Observed external progress"
   local advanced = vim.system({ binary, "timeline-project", "apply", "--id", "e2e-project" },
      { text = true, stdin = vim.json.encode(external), timeout = 15000 }):wait()
   assert(advanced.code == 0, advanced.stderr)
   assert(vim.wait(8000, function()
      return table.concat(vim.api.nvim_buf_get_lines(buf, 0, -1, false), "\n"):find("Observed external progress", 1, true) ~= nil
   end, 20), "open timeline did not monitor the external writer")
   -- Execute the real buffer-local q mapping, including BufWipeout cancellation.
   vim.api.nvim_feedkeys("q", "xt", false)
   assert(vim.wait(1000, function() return not vim.api.nvim_buf_is_valid(buf) end, 10), "q did not wipe timeline")
   assert(#vim.api.nvim_list_wins() == windows_before, "timeline close left its split open")
   view.cancel()
end

local ok, failure = xpcall(run, debug.traceback)
vim.fn.delete(root, "rf")
if not ok then
   print("track-e2e: FAIL nvim agent timeline: " .. tostring(failure))
   vim.cmd("cquit 1")
else
   print("track-e2e: PASS nvim agent timeline real CLI, revision conflict, render and close")
   vim.cmd("qa!")
end
