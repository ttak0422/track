-- An asynchronous Telescope finder for the CLI's whole-document JSON search.
local client = require("track.client")
local M = {}

local function diagnostic(result, message)
   local stderr = (result.stderr or ""):sub(1, 512):gsub("%s+$", "")
   local status = string.format(" (exit %s, signal %s)", result.code, result.signal)
   return "track: " .. message .. status .. (stderr ~= "" and ": " .. stderr or "")
end

-- Runs a JSON CLI command without waiting. Cancellation suppresses the callback,
-- even when a completion has already been scheduled on Neovim's main loop.
function M.run_json_async(args, callback)
   local cancelled, process = false, nil
   local function deliver(data, err)
      vim.schedule(function()
         if not cancelled then callback(data, err) end
      end)
   end
   local ok_bin, bin = pcall(client.bin)
   if not ok_bin then
      deliver(nil, "track: could not run CLI: " .. tostring(bin):sub(1, 512))
   else
      local command = { bin }
      for _, arg in ipairs(args) do command[#command + 1] = arg end
      local ok, result = pcall(vim.system, command, { text = true }, function(output)
         local decoded_ok, decoded = pcall(vim.json.decode, output.stdout or "")
         if decoded_ok and type(decoded) == "table" and decoded.error then
            deliver(nil, decoded.error)
         elseif output.code ~= 0 or output.signal ~= 0 then
            deliver(nil, diagnostic(output, "CLI failed"))
         elseif not decoded_ok then
            deliver(nil, diagnostic(output, "invalid JSON output"))
         else
            deliver(decoded)
         end
      end)
      if ok then process = result else deliver(nil, "track: could not run CLI: " .. tostring(result):sub(1, 512)) end
   end
   return function()
      if cancelled then return end
      cancelled = true
      if process then pcall(function() process:kill(15) end) end
   end
end

function M.new(opts)
   local self = { status = "idle", prompt = nil, result_count = nil }
   local generation, closed, cancel_search = 0, false, nil
   local cancels, owned = {}, {}
   local function cancel_all()
      if cancel_search then cancel_search(); cancel_search = nil end
      for _, cancel in pairs(cancels) do if cancel then cancel() end end
      cancels = {}
   end
   function self:close()
      if closed then return end
      closed = true
      generation = generation + 1
      cancel_all()
   end
   -- Telescope may reuse a cached picker after calling close(). Keep completed
   -- cached entries usable; an interrupted search must be run again on resume.
   function self:reopen(prompt)
      local refresh = closed and (self.status ~= "ok" or self.prompt ~= vim.trim(prompt or ""))
      closed = false
      return refresh
   end
   function self:can_select(prompt)
      return not closed and self.status == "ok" and self.prompt == vim.trim(prompt or "")
   end
   function self:owns(entry)
      return entry ~= nil and owned[entry] == true
   end
   local function find(_, prompt, process_result, process_complete)
      closed = false
      generation = generation + 1
      owned = {}
      local current = generation
      cancel_all()
      local query = vim.trim(prompt or "")
      self.prompt, self.status, self.result_count = query, "pending", nil
      local function live() return not closed and current == generation end
      local function finish_error(err)
         if not live() then return end
         self.status = "error"
         vim.notify("track: " .. tostring(err), vim.log.levels.ERROR)
         process_complete()
      end
      if query == "" then
         self.status, self.result_count = "ok", 0
         process_complete()
         return
      end
      cancel_search = M.run_json_async({ "search", "--scope", opts.scope, "--query", query, "--limit", tostring(opts.limit or 100) }, function(data, err)
         if not live() then return end
         cancel_search = nil
         if not data then finish_error(err); return end
         if type(data) ~= "table" or not vim.islist(data.results) then
            finish_error("invalid search results")
            return
         end
         local results = data.results
         local count, seen = #results, 0
         for key, result in pairs(results) do
            seen = seen + 1
            if type(key) ~= "number" or key < 1 or key % 1 ~= 0 or key > count
               or type(result) ~= "table" or type(result.path) ~= "string" or result.path == "" then
               finish_error("invalid search result")
               return
            end
         end
         if seen ~= count then finish_error("invalid search results"); return end
         local started, running, finished = 0, 0, 0
         local paths, failed, pumping = {}, nil, false
         local pump
         local function done()
            if not live() then return end
            if failed then finish_error(failed); return end
            for i, result in ipairs(results) do
               local ok, entry = pcall(opts.entry_maker, result, paths[i])
               if not ok then
                  finish_error("invalid search entry: " .. tostring(entry))
                  return
               end
               if entry then
                  entry.index = i
                  owned[entry] = true
               end
               if process_result(entry) then return end
            end
            self.status, self.result_count = "ok", count
            process_complete()
         end
         pump = function()
            if not live() or pumping then return end
            pumping = true
            while running < 4 and started < count and live() do
               started = started + 1
               local i = started
               running = running + 1
               local completed = false
               local function resolved(path, resolve_err)
                  if completed or not live() then return end
                  completed = true
                  cancels[i] = nil
                  running, finished = running - 1, finished + 1
                  if not path and not failed then failed = resolve_err or "could not resolve editor path" end
                  paths[i] = path
                  if finished == count then done() elseif not pumping then pump() end
               end
               local ok, cancel = pcall(function()
                  return require("track.editor_view").preferred_async(results[i].path or "", resolved)
               end)
               if not ok then resolved(nil, tostring(cancel))
               elseif not completed then cancels[i] = cancel end
            end
            pumping = false
            if count == 0 then done() end
         end
         pump()
      end)
   end
   return setmetatable(self, { __call = find })
end

return M
