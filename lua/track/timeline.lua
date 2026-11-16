-- Asynchronous timeline snapshots for Neovim views, including sidebar providers.
-- No polling or process launch happens while rendering cached rows.
local M = {}

function M.new(path)
   assert(type(path) == "string" and path ~= "", "timeline needs a note path")
   local view = { snapshot = nil, error = nil, loading = false }
   local generation, process = 0, nil

   function view.cancel()
      generation = generation + 1
      if process then process:kill(15) end
      process = nil
      view.loading = false
   end

   function view.refresh(callback)
      view.cancel()
      local request = generation
      view.loading = true
      local function finish(err, snapshot)
         vim.schedule(function()
            if request ~= generation then return end
            process = nil
            view.loading, view.error = false, err
            -- Keep the last good snapshot on failure; error marks it stale.
            if snapshot then view.snapshot = snapshot end
            if callback then callback(err, snapshot) end
         end)
      end
      local ok, launched = pcall(function()
         return vim.system({ require("track.client").bin(), "timeline", "--path", path },
            { text = true, timeout = 10000 }, function(result)
               local valid, data = pcall(vim.json.decode, result.stdout or "")
               if result.code ~= 0 or result.signal ~= 0 then
                  local reason = valid and type(data) == "table" and data.error or nil
                  if type(reason) ~= "string" or not reason:find("%S") then
                     reason = result.stderr
                  end
                  if type(reason) ~= "string" or not reason:find("%S") then
                     reason = "exit code " .. tostring(result.code)
                     if result.signal and result.signal ~= 0 then
                        reason = reason .. ", signal " .. tostring(result.signal)
                     end
                  end
                  finish("Timeline read failed: " .. reason:sub(1, 512))
                  return
               end
               if not valid or type(data) ~= "table" or data.schema_version ~= 1
                  or type(data.items) ~= "table" or type(data.history) ~= "table" then
                  finish("Invalid timeline response")
                  return
               end
               finish(nil, data)
            end)
      end)
      if ok then process = launched else finish(tostring(launched):sub(1, 512)) end
   end

   -- Plain text is safe for komado providers; the consumer owns highlights and mappings.
   function view.rows()
      local rows = {}
      for _, item in ipairs(view.snapshot and view.snapshot.items or {}) do
         local text = tostring(item.text or ""):gsub("[%c]", " ")
         rows[#rows + 1] = string.rep("  ", math.min(item.depth or 0, 32))
            .. "[" .. tostring(item.state) .. "] " .. text
      end
      return rows
   end

   return view
end

return M
