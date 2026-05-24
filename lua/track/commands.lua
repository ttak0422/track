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

   cmd("TrackNew", function(opts)
      local create = require("track.create")
      if opts.range > 0 then
         create.from_visual()
      elseif #opts.fargs > 0 then
         create.create(table.concat(opts.fargs, " "))
      else
         create.prompt(vim.fn.expand("<cword>"))
      end
   end, { nargs = "*", range = true, desc = "Create a track note (selection, args, or prompt)" })

   cmd("TrackFollow", function()
      require("track.follow").follow()
   end, { desc = "Follow the track link under the cursor" })

   cmd("TrackToday", function()
      require("track.journal").open(0)
   end, { desc = "Open today's journal note" })

   cmd("TrackYesterday", function()
      require("track.journal").open(-1)
   end, { desc = "Open yesterday's journal note" })

   cmd("TrackTomorrow", function()
      require("track.journal").open(1)
   end, { desc = "Open tomorrow's journal note" })

   cmd("TrackJournal", function(opts)
      require("track.journal").open(tonumber(opts.args) or 0)
   end, { nargs = "?", desc = "Open the journal note at a day offset (default 0)" })

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
