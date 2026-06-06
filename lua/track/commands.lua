-- User command registration for track.nvim.

local client = require("track.client")
local util = require("track.util")

local M = {}
local subcommands = {}

local function cmd(name, fn, opts)
   vim.api.nvim_create_user_command(name, fn, opts or {})
end

local function command_names()
   local names = vim.tbl_keys(subcommands)
   table.sort(names)
   return names
end

local function register(name, fn, opts)
   opts = opts or {}
   subcommands[name] = {
      fn = fn,
      nargs = opts.nargs or 0,
      range = opts.range == true,
      complete = opts.complete,
      desc = opts.desc,
   }
end

local function validate_args(name, spec, opts)
   local nargs = #opts.fargs
   local expected = spec.nargs
   if expected == "?" then
      if nargs > 1 then
         vim.notify("track: " .. name .. " expects 0 or 1 arguments", vim.log.levels.ERROR)
         return false
      end
   elseif expected == "+" then
      if nargs == 0 then
         vim.notify("track: " .. name .. " expects at least one argument", vim.log.levels.ERROR)
         return false
      end
   elseif expected ~= "*" and nargs ~= expected then
      vim.notify("track: " .. name .. " expects " .. expected .. " arguments", vim.log.levels.ERROR)
      return false
   end
   if opts.range > 0 and not spec.range then
      vim.notify("track: " .. name .. " does not accept a range", vim.log.levels.ERROR)
      return false
   end
   return true
end

local function handle_track(opts)
   local name = opts.fargs[1]
   if not name then
      vim.ui.select(command_names(), { prompt = "Track command" }, function(choice)
         if choice then
            vim.cmd.Track(choice)
         end
      end)
      return
   end
   table.remove(opts.fargs, 1)
   opts.args = table.concat(opts.fargs, " ")

   local spec = subcommands[name]
   if not spec then
      vim.notify("track: unknown command " .. name, vim.log.levels.ERROR)
      return
   end
   if validate_args(name, spec, opts) then
      spec.fn(opts)
   end
end

local function complete_track(arg_lead, cmdline, cursor_pos)
   local split = vim.split(cmdline:sub(1, cursor_pos), " ", { plain = true, trimempty = true })
   local name = split[2]
   local names = command_names()

   if cmdline:match("^['<,'>]*Track%s*$") then
      return names
   end
   if #split <= 2 and name then
      return vim.tbl_filter(function(candidate)
         return vim.startswith(candidate, name)
      end, names)
   end

   local spec = subcommands[name]
   if type(spec and spec.complete) == "function" then
      return spec.complete(arg_lead, cmdline, cursor_pos)
   elseif type(spec and spec.complete) == "string" then
      return vim.fn.getcompletion(arg_lead, spec.complete)
   end
end

function M.setup()
   register("dump", function()
      util.open_scratch("track://dump", "json", client.run({ "dump" }))
   end, { desc = "Open a diagnostic dump of track state" })

   register("open", function(opts)
      local create = require("track.create")
      if opts.range > 0 then
         create.from_visual()
      elseif #opts.fargs > 0 then
         create.create(table.concat(opts.fargs, " "))
      else
         create.prompt(vim.fn.expand("<cword>"))
      end
   end, { nargs = "*", range = true, desc = "Open or create a track note by title (selection, args, or prompt); existing titles are reused" })

   register("template", function(opts)
      require("track.template").open(table.concat(opts.fargs, " "))
   end, { nargs = "*", complete = function(arg_lead)
      return require("track.template").complete(arg_lead)
   end, desc = "Open or create a template by name" })

   register("from_template", function(opts)
      local template = opts.fargs[1] or ""
      local title = ""
      if #opts.fargs > 1 then
         local parts = {}
         for i = 2, #opts.fargs do
            parts[#parts + 1] = opts.fargs[i]
         end
         title = table.concat(parts, " ")
      end
      require("track.template").create_note(template, title)
   end, { nargs = "*", complete = function(arg_lead)
      return require("track.template").complete(arg_lead)
   end, desc = "Create a note from a template" })

   register("follow", function()
      require("track.follow").follow()
   end, { desc = "Follow the track link under the cursor" })

   register("backlinks", function()
      require("track.backlinks").show()
   end, { desc = "Show notes that link to the current note" })

   register("babel_exec", function()
      require("track.babel").exec()
   end, { desc = "Run the source block under the cursor and show its result" })

   register("babel_restore", function()
      require("track.babel").restore()
   end, { desc = "Restore stored source block results in the buffer" })

   register("babel_clear", function()
      require("track.babel").clear()
   end, { desc = "Clear rendered babel results in the buffer" })

   register("today", function()
      require("track.journal").open(0)
   end, { desc = "Open today's journal note" })

   register("yesterday", function()
      require("track.journal").open(-1)
   end, { desc = "Open yesterday's journal note" })

   register("tomorrow", function()
      require("track.journal").open(1)
   end, { desc = "Open tomorrow's journal note" })

   register("journal", function(opts)
      require("track.journal").open(tonumber(opts.args) or 0)
   end, { nargs = "?", desc = "Open the journal note at a day offset (default 0)" })

   register("keywords", function()
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

   cmd("Track", handle_track, {
      nargs = "*",
      range = true,
      complete = complete_track,
      desc = "Run a track subcommand",
   })
end

return M
