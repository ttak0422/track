-- Neovim-native agent timeline view. No terminal/TUI or automatic agent invocation.
local model = require("track.agent_timeline")
local M = {}

local function clean(value) return tostring(value or ""):gsub("[%c]", " ") end

local function lines(snapshot)
   local result = { "# " .. clean(snapshot.title), "",
      string.format("Revision %d | %.0f%% | %.1f min remaining", snapshot.revision,
         snapshot.progress.fraction * 100, snapshot.progress.remaining_minutes),
      "r refresh   a append/update record   q close", "", "## Tasks" }
   for _, task in ipairs(snapshot.tasks) do
      result[#result + 1] = string.format("[%s] %s (%s)%s", task.status, clean(task.title), task.id,
         task.parent_id and " <- " .. task.parent_id or "")
      result[#result + 1] = string.format("  owner=%s weight=%g effort=%g/%g min mode=%s", task.owner or "", task.weight, task.done_minutes, task.remaining_minutes, task.mode)
      if task.detail then result[#result + 1] = "  " .. clean(task.detail) end
   end
   result[#result + 1] = ""
   result[#result + 1] = "## Reports"
   for _, report in ipairs(snapshot.reports) do
      result[#result + 1] = report.id .. " | " .. clean(report.author) .. " | " .. clean(report.summary)
      result[#result + 1] = "  " .. report.period_start .. " - " .. report.period_end .. " events: " .. table.concat(report.event_sequences, ", ")
      if report.uncertainty then result[#result + 1] = "Uncertainty: " .. clean(report.uncertainty) end
      if report.user_action then result[#result + 1] = "User action: " .. clean(report.user_action) end
   end
   result[#result + 1] = ""
   result[#result + 1] = "## Discussion"
   for _, entry in ipairs(snapshot.discussions) do
      result[#result + 1] = clean(entry.author) .. " -> " .. entry.target_type .. ":" .. entry.target_id
         .. ": " .. clean(entry.text)
   end
   result[#result + 1] = ""
   result[#result + 1] = "## Cooperative controls (receipts, not process state)"
   for _, control in ipairs(snapshot.controls) do
      result[#result + 1] = control.id .. " " .. control.action .. " -> " .. control.owner .. " [" .. control.status .. "] " .. clean(control.result)
   end
   result[#result + 1] = ""
   result[#result + 1] = "## Recorded events"
   for _, event in ipairs(snapshot.events) do
      result[#result + 1] = string.format("%d %s %s %s:%s", event.sequence, event.at, event.kind, event.source, event.event_id)
   end
   return result
end

local function edit_record(view, kind, payload, on_saved)
   local request = view.request(kind, payload)
   local buf = vim.api.nvim_create_buf(false, true)
   vim.cmd("botright split")
   vim.api.nvim_win_set_buf(0, buf)
   vim.bo[buf].buftype, vim.bo[buf].bufhidden, vim.bo[buf].swapfile = "acwrite", "wipe", false
   vim.bo[buf].filetype = "json"
   vim.api.nvim_buf_set_name(buf, "track-timeline-record://" .. view.project_id .. "/" .. request.event_id)
   vim.api.nvim_buf_set_lines(buf, 0, -1, false, { vim.json.encode(request) })
   vim.bo[buf].modified = false
   local sending = false
   vim.api.nvim_create_autocmd("BufWriteCmd", { buffer = buf, callback = function()
      if sending then return end
      local ok, input = pcall(vim.json.decode, table.concat(vim.api.nvim_buf_get_lines(buf, 0, -1, false), "\n"))
      if not ok or type(input) ~= "table" then
         vim.notify("Timeline record must be a JSON object", vim.log.levels.ERROR)
         return
      end
      local submitted_tick = vim.api.nvim_buf_get_changedtick(buf)
      sending = true
      vim.bo[buf].modifiable = false
      view.apply(input, function(err)
         sending = false
         if vim.api.nvim_buf_is_valid(buf) then vim.bo[buf].modifiable = true end
         if err then
            vim.notify(err .. "\nNo automatic retry. Refresh before resolving a revision conflict; keep the same event ID when retrying an uncertain write.", vim.log.levels.ERROR)
            return
         end
         local owns_view = vim.api.nvim_buf_is_valid(buf)
         if owns_view and vim.api.nvim_buf_get_changedtick(buf) ~= submitted_tick then
            vim.notify("The submitted record was saved; newer draft edits were retained. Use a new event ID for another action.")
            return
         end
         if owns_view then
            vim.bo[buf].modified = false
            vim.api.nvim_buf_delete(buf, { force = false })
         end
         if owns_view and on_saved then on_saved() end
      end)
   end })
   vim.notify("Edit the explicit record and :write to submit; :bd! cancels without sending.")
end

local kinds = { "task.upsert", "project.update", "report.add", "discussion.add", "control.request", "control.update" }
local function payload_for(view, kind)
   local now = os.date("!%Y-%m-%dT%H:%M:%SZ")
   local id = view.request("", {}).event_id
   if kind == "task.upsert" then return { id = id, title = "New task", status = "pending", owner = "nvim", weight = 1, done_minutes = 0, remaining_minutes = 0, mode = "serial" } end
   if kind == "project.update" then return { title = view.snapshot.title, description = view.snapshot.description, note_id = view.snapshot.note_id, mode = view.snapshot.mode } end
   if kind == "report.add" then return { id = id, author = "nvim", period_start = now, period_end = now, summary = "", event_sequences = { view.snapshot.revision } } end
   if kind == "discussion.add" then return { id = id, target_type = "project", target_id = view.project_id, author = "nvim", text = "" } end
   if kind == "control.request" then return { id = id, task_id = "", owner = "", action = "pause", status = "requested" } end
   return { id = "", status = "acknowledged", result = "" }
end

function M.open(project_id, options)
   options = options or { vault_dir = require("track.config").options.vault_dir }
   if not project_id or project_id == "" then
      local ok, err = pcall(model.list, options, function(failure, data)
         if failure then vim.notify(failure, vim.log.levels.ERROR); return end
         local choices = { { project_id = "", title = "+ Create project" } }
         for _, project in ipairs(data.projects) do choices[#choices + 1] = project end
         vim.ui.select(choices, { prompt = "Agent timeline", format_item = function(item) return item.title end }, function(choice)
            if not choice then return end
            if choice.project_id ~= "" then M.open(choice.project_id, options); return end
            vim.ui.input({ prompt = "Project ID (letters, digits, -, _): " }, function(id)
               if not id or not id:match("^[%w_-]+$") then return end
               local view = model.new(id, options)
               edit_record(view, "project.create", { title = id, mode = "serial" }, function() M.open(id, options) end)
            end)
         end)
      end)
      if not ok then vim.notify(tostring(err), vim.log.levels.ERROR) end
      return
   end
   local view = model.new(project_id, options)
   local buf = vim.api.nvim_create_buf(false, true)
   vim.cmd("botright vsplit")
   vim.api.nvim_win_set_buf(0, buf)
   vim.bo[buf].bufhidden, vim.bo[buf].swapfile, vim.bo[buf].filetype = "wipe", false, "markdown"
   vim.api.nvim_buf_set_name(buf, "track-timeline://" .. project_id .. "/" .. buf)
   local function render(err)
      if not vim.api.nvim_buf_is_valid(buf) then return end
      local output = view.snapshot and lines(view.snapshot) or { "Loading timeline..." }
      if err then table.insert(output, 1, "[unavailable or stale] " .. clean(err)) end
      vim.bo[buf].modifiable = true
      vim.api.nvim_buf_set_lines(buf, 0, -1, false, output)
      vim.bo[buf].modifiable, vim.bo[buf].modified = false, false
   end
   vim.keymap.set("n", "r", function() view.refresh(render) end, { buffer = buf })
   vim.keymap.set("n", "q", function() vim.api.nvim_buf_delete(buf, { force = false }) end, { buffer = buf })
   vim.keymap.set("n", "a", function()
      if not view.snapshot then return end
      vim.ui.select(kinds, { prompt = "Append an explicit record" }, function(kind)
         if kind then edit_record(view, kind, payload_for(view, kind), function() render() end) end
      end)
   end, { buffer = buf })
   vim.api.nvim_create_autocmd("BufWipeout", { buffer = buf, once = true, callback = view.cancel })
   render()
   view.refresh(render)
end

return M
