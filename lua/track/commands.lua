-- User command registration for track.nvim.

local client = require("track.client")
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

   cmd("TrackBacklinks", function()
      require("track.backlinks").show()
   end, { desc = "Show notes that link to the current note" })

   cmd("TrackBabelExec", function()
      require("track.babel").exec()
   end, { desc = "Run the source block under the cursor and show its result" })

   cmd("TrackBabelRestore", function()
      require("track.babel").restore()
   end, { desc = "Restore stored source block results in the buffer" })

   cmd("TrackBabelClear", function()
      require("track.babel").clear()
   end, { desc = "Clear rendered babel results in the buffer" })

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
      local data, err = client.run_json({ "keywords" })
      if not data then
         vim.notify("track: " .. tostring(err), vim.log.levels.ERROR)
         return
      end
      local lines = {}
      for _, k in ipairs(data.keywords or {}) do
         lines[#lines + 1] = string.format("%s\t->\t%s\t(%s)", k.term, k.path, k.kind)
      end
      if #lines == 0 then
         lines = { "(no keywords)" }
      end
      util.open_scratch("track://keywords", "text", table.concat(lines, "\n"))
   end, { desc = "List the track link keyword dictionary" })
end

return M
