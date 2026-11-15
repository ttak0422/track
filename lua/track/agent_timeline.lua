-- Explicit agent work records. Rendering reads cache; only user actions issue writes.
local M = {}
local serial = 0

local function event_id()
   serial = serial + 1
   local uv = vim.uv or vim.loop
   return "nvim-" .. vim.fn.sha256(tostring(uv.hrtime()) .. ":" .. tostring(serial)):sub(1, 24)
end

local function call(args, payload, options, callback)
   local argv = { require("track.client").bin(), "timeline-project" }
   for _, arg in ipairs(args) do argv[#argv + 1] = arg end
   if options.vault then
      argv[#argv + 1], argv[#argv + 2] = "--vault", options.vault
   end
   local settings = { text = true, timeout = 15000 }
   if options.vault_dir then settings.env = { TRACK_VAULT = options.vault_dir } end
   if payload then settings.stdin = vim.json.encode(payload) end
   return vim.system(argv, settings, function(result)
      vim.schedule(function()
         local ok, data = pcall(vim.json.decode, result.stdout or "")
         if result.code ~= 0 or result.signal ~= 0 or not ok or type(data) ~= "table" or data.error then
            local err = ok and type(data) == "table" and data.error or result.stderr
            callback(tostring(err or "Timeline request failed"):sub(1, 1024))
         else callback(nil, data) end
      end)
   end)
end

function M.list(options, callback)
   return call({ "list" }, nil, options or {}, callback)
end

function M.new(project_id, options)
   assert(type(project_id) == "string" and project_id:match("^[A-Za-z0-9][A-Za-z0-9_-]*$") and #project_id <= 96, "invalid project id")
   options = options or {}
   local view = { project_id = project_id, snapshot = nil, error = nil, loading = false, writing = false }
   local generation, read_job = 0, nil

   function view.cancel()
      generation = generation + 1
      if read_job then read_job:kill(15) end
      read_job, view.loading = nil, false
      -- A submitted write is not cancelled: its result may already be committed.
   end

   function view.refresh(callback)
      if view.writing then
         if callback then callback("A timeline write is pending; refresh after its receipt") end
         return
      end
      view.cancel()
      view.loading = true
      local request = generation
      local ok, job = pcall(call, { "get", "--id", project_id }, nil, options, function(err, data)
         if request ~= generation then return end
         read_job, view.loading, view.error = nil, false, err
         if data then view.snapshot = data end
         if vim.api then vim.api.nvim_exec_autocmds("User", { pattern = "TrackTimelineChanged" }) end
         if callback then callback(err, data) end
      end)
      if ok then read_job = job else
         view.loading, view.error = false, tostring(job)
         if callback then callback(view.error) end
      end
   end

   function view.request(kind, payload)
      return { source = "nvim", event_id = event_id(), expected_revision = view.snapshot and view.snapshot.revision or 0,
         kind = kind, payload = payload }
   end

   function view.apply(request, callback)
      if view.writing then
         if callback then callback("A timeline write is already pending") end
         return
      end
      view.cancel()
      local request_generation = generation
      view.writing = true
      local ok, err = pcall(call, { "apply", "--id", project_id }, request, options, function(failure, data)
         view.writing = false
         -- Cancelling a view only drops display ownership, not the caller's write receipt.
         if request_generation == generation then
            view.error = failure
            if data then view.snapshot = data end
         end
         if vim.api then vim.api.nvim_exec_autocmds("User", { pattern = "TrackTimelineChanged" }) end
         if callback then callback(failure, data) end
      end)
      if not ok then
         view.writing, view.error = false, tostring(err)
         if callback then callback(view.error) end
      end
   end

   function view.rows()
      local rows = {}
      local snapshot = view.snapshot
      if not snapshot then return {view.error and "Timeline unavailable" or "Timeline not loaded"} end
      local progress = snapshot.progress
      rows[1] = snapshot.title:gsub("[%c]", " ") .. string.format("  %.0f%%", (progress.fraction or 0) * 100)
      if view.error then rows[#rows + 1] = "[stale: refresh failed]" end
      local by_parent = {}
      for _, task in ipairs(snapshot.tasks) do
         local key = task.parent_id or ""
         by_parent[key] = by_parent[key] or {}
         table.insert(by_parent[key], task)
      end
      local function add(parent, depth)
         for _, task in ipairs(by_parent[parent] or {}) do
            rows[#rows + 1] = string.rep("  ", math.min(depth, 32)) .. "[" .. task.status .. "] "
               .. task.title:gsub("[%c]", " ")
            add(task.id, depth + 1)
         end
      end
      add("", 0)
      return rows
   end

   -- A drop-in komado Line specification; providers never execute a command.
   function view.komado_component()
      local Line = require("komado.dsl").Line
      return { update = { "User", pattern = "TrackTimelineChanged" },
         require("komado.utils").mapped_list(function() return view.rows() end, function(row)
            return Line({ provider = row })
         end) }
   end

   return view
end

return M
