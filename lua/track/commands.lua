-- User command registration for track.nvim.

local client = require("track.client")
local keywords = require("track.keywords")
local util = require("track.util")

local M = {}

local function cmd(name, fn, opts)
   vim.api.nvim_create_user_command(name, fn, opts or {})
end

function M.setup()
   cmd("TrackDump", function()
      util.open_scratch("track://dump", "json", client.run({ "dump" }))
   end, { desc = "Open a diagnostic dump of track state" })

   cmd("TrackFollow", function()
      require("track.follow").follow()
   end, { desc = "Follow the track link under the cursor" })

   cmd("TrackKeywords", function()
      local entries = keywords.all()
      local lines = {}
      for _, k in ipairs(entries) do
         lines[#lines + 1] = string.format("%s\t->\t%s\t(%s)", k.term, k.path, k.kind)
      end
      if #lines == 0 then
         lines = { "(no keywords)" }
      end
      util.open_scratch("track://keywords", "text", table.concat(lines, "\n"))
   end, { desc = "List the track auto-link keyword dictionary" })
end

return M
