-- Thin, asynchronous frontend for the CLI's checked archive writer.
local client = require("track.client")
local M = {}
local pending = false

local function failure(message)
   vim.notify("track backup: " .. message, vim.log.levels.ERROR)
   return nil, message
end

local function parse(args)
   local opts, seen = {}, {}
   local i = 1
   while i <= #args do
      local flag = args[i]
      if seen[flag] then return nil, "duplicate option: " .. flag end
      seen[flag] = true
      if flag == "--all-vaults" or flag == "--overwrite" then
         opts[flag] = true
      elseif flag == "--out" or flag == "--vault" or flag == "--timeout" then
         i = i + 1
         local value = args[i]
         if type(value) ~= "string" or value == "" or value:sub(1, 2) == "--" then
            return nil, flag .. " requires a value"
         end
         opts[flag] = value
      else
         return nil, "unknown option: " .. tostring(flag)
      end
      i = i + 1
   end
   if opts["--vault"] and opts["--all-vaults"] then
      return nil, "--vault and --all-vaults cannot be combined"
   end
   return opts
end

local function start(opts)
   if pending then return failure("another backup is already in progress") end
   local out = opts["--out"]
   if type(out) ~= "string" or not out:match("%.tar%.gz$") or out:find("%z") then
      return failure("--out must name a .tar.gz file")
   end
   -- Expand only a leading home directory, not environment variables, wildcards or Ex tokens.
   if out:sub(1, 2) == "~/" then out = vim.env.HOME .. out:sub(2) end
   out = vim.fn.fnamemodify(out, ":p")
   local ok, binary = pcall(client.bin)
   if not ok then return failure(tostring(binary)) end
   local argv = { binary, "backup", "--out", out }
   for _, flag in ipairs({ "--vault", "--all-vaults", "--overwrite", "--timeout" }) do
      if opts[flag] then
         argv[#argv + 1] = flag
         if type(opts[flag]) == "string" then argv[#argv + 1] = opts[flag] end
      end
   end
   pending = true
   -- Let the CLI own its deadline and staging cleanup. An editor-side timeout could kill the
   -- parent at commit and hide a committed-but-durability-uncertain result.
   local launched, process = pcall(vim.system, argv, { text = true }, function(result)
      vim.schedule(function()
         pending = false
         local decoded, data = pcall(vim.json.decode, result.stdout or "")
         local valid = decoded and type(data) == "table"
         if result.code ~= 0 or result.signal ~= 0 or not valid or data.error
            or data.complete ~= true or type(data.path) ~= "string" then
            local details = {}
            if valid and data.error then details[#details + 1] = tostring(data.error) end
            -- Keep both streams, including post-commit/cleanup diagnostics. Never promise rollback.
            if result.stdout and result.stdout ~= "" then details[#details + 1] = result.stdout end
            if result.stderr and result.stderr ~= "" then details[#details + 1] = result.stderr end
            if #details == 0 then details[1] = "no valid completion response" end
            failure("did not complete successfully (exit " .. tostring(result.code) .. "): " .. table.concat(details, "\n"))
            return
         end
         vim.notify("track backup complete: " .. data.path, vim.log.levels.INFO)
         if result.stderr and result.stderr ~= "" then
            vim.notify("track backup: " .. result.stderr, vim.log.levels.WARN)
         end
      end)
   end)
   if not launched then
      pending = false
      return failure("could not start: " .. tostring(process))
   end
   vim.notify("track backup started: " .. out .. " (saved files only; buffers are not saved)", vim.log.levels.INFO)
   return process
end

-- Scriptable argv API, identical to :Track backup. No output prompts for a destination.
-- Returns the process for explicit output, or nil/error on invalid input/start failure.
function M.run(args)
   local opts, err = parse(args or {})
   if not opts then return failure(err) end
   if pending then return failure("another backup is already in progress") end
   if opts["--out"] then return start(opts) end
   vim.ui.input({ prompt = "Backup saved files to (.tar.gz; outside vault): ", completion = "file" }, function(path)
      if path == nil or path == "" then return end
      opts["--out"] = path
      start(opts)
   end)
end

function M.complete(lead, cmdline, cursor)
   local before = cmdline:sub(1, cursor - #lead)
   if before:match("%-%-out%s+$") then
      return vim.tbl_map(vim.fn.fnameescape, vim.fn.getcompletion(lead, "file"))
   end
   if before:match("%-%-vault%s+$") or before:match("%-%-timeout%s+$") then return {} end
   return vim.tbl_filter(function(flag) return vim.startswith(flag, lead) end,
      { "--out", "--vault", "--all-vaults", "--overwrite", "--timeout" })
end

return M
