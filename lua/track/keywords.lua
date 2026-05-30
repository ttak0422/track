-- Session cache of the auto-link keyword dictionary, fetched from the CLI.

local client = require("track.client")

local M = {}

local cache = nil

-- all returns the list of keyword entries ({term, note_id, path, kind}), fetching and caching them on first use.
function M.all()
   if cache then
      return cache
   end
   local data, err = client.run_json({ "keywords" })
   if not data then
      vim.notify("track: " .. tostring(err), vim.log.levels.ERROR)
      cache = {}
      return cache
   end
   cache = data.keywords or {}
   return cache
end

-- invalidate drops the cache so the next all() refetches.
-- Call after creating or reindexing notes.
function M.invalidate()
   cache = nil
end

return M
