-- Vault activity watcher.
--
-- Polls the day's agenda — the notes created *or* updated on a date — and reports whatever appeared
-- since the last poll. That one list covers both halves of "something changed", so nothing here has
-- to watch the filesystem or diff bodies. The web workspace answers the same question the same way
-- (web/src/vaultActivity.ts), which is why both report a note once per day rather than once per edit.

local client = require("track.client")
local config = require("track.config")
local util = require("track.util")

local uv = vim.uv or vim.loop

local M = {}

local timer
local seen = {}
local day = ""

local function today()
   return os.date("%Y-%m-%d")
end

-- mark records a note id as already known. Notes this Neovim wrote go through here: a save is not
-- news to the editor that made it.
function M.mark(id)
   if id ~= nil and id ~= "" then
      seen[tostring(id)] = true
   end
end

local function fresh_titles(notes)
   local titles = {}
   for _, note in ipairs(notes) do
      local id = tostring(note.note_id)
      if not seen[id] then
         seen[id] = true
         table.insert(titles, (note.title ~= nil and note.title ~= "") and note.title or id)
      end
   end
   return titles
end

local function announce(titles)
   local message = titles[1]
   if #titles > 1 then
      message = string.format("%s (+%d more)", message, #titles - 1)
   end
   vim.notify("track: updated " .. message, vim.log.levels.INFO)
end

local function poll()
   local now = today()
   -- A new day primes rather than reports: yesterday's notes are not news either.
   local priming = now ~= day
   if priming then
      day, seen = now, {}
   end

   local out = {}
   vim.fn.jobstart({ client.bin(), "agenda", "--date", now }, {
      env = { TRACK_VAULT = config.options.vault_dir },
      stdout_buffered = true,
      on_stdout = function(_, data)
         out = data or {}
      end,
      on_exit = function(_, code)
         if code ~= 0 then
            return
         end
         local ok, decoded = pcall(vim.json.decode, table.concat(out, "\n"))
         if not ok or type(decoded) ~= "table" or type(decoded.notes) ~= "table" then
            return
         end
         local titles = fresh_titles(decoded.notes)
         if not priming and #titles > 0 then
            announce(titles)
         end
      end,
   })
end

function M.setup()
   local interval = config.options.watch_interval_ms
   if type(interval) ~= "number" or interval <= 0 then
      return
   end

   vim.api.nvim_create_autocmd("BufWritePost", {
      group = vim.api.nvim_create_augroup(config.options.augroup .. "-watch", { clear = true }),
      callback = function(args)
         -- A note's file is named for its id (<vault>/note/<id>.md), so the buffer names what to skip.
         if util.vault_of(args.buf) then
            M.mark(vim.fn.fnamemodify(args.file, ":t:r"))
         end
      end,
   })

   timer = uv.new_timer()
   timer:start(interval, interval, vim.schedule_wrap(poll))
end

return M
