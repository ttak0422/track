-- Track actions embedded in Markdown links.

local M = {}

local function url_decode(value)
   value = tostring(value or ""):gsub("+", " ")
   return (value:gsub("%%(%x%x)", function(hex)
      return string.char(tonumber(hex, 16))
   end))
end

local function query_params(query)
   local params = {}
   for part in string.gmatch(query or "", "([^&]+)") do
      local key, value = part:match("^([^=]*)=(.*)$")
      if key then
         params[url_decode(key)] = url_decode(value)
      else
         params[url_decode(part)] = ""
      end
   end
   return params
end

local function normalize_target(target)
   target = vim.trim(tostring(target or ""))
   if target:sub(1, 1) == "<" and target:sub(-1) == ">" then
      target = vim.trim(target:sub(2, -2))
   end
   return target
end

local function day_from_params(params)
   local offset = tonumber(params.offset or "")
   if params.date == "today" or params.date == nil or params.date == "" then
      return os.time() + ((offset or 0) * 86400)
   end
   if params.date == "yesterday" then
      return os.time() - 86400
   end
   if params.date == "tomorrow" then
      return os.time() + 86400
   end
   local y, m, d = tostring(params.date):match("^(%d%d%d%d)%-(%d%d)%-(%d%d)$")
   if y then
      return os.time({ year = tonumber(y), month = tonumber(m), day = tonumber(d), hour = 12 })
   end
   return os.time() + ((offset or 0) * 86400)
end

local function expand_params(params)
   local day = day_from_params(params)
   local values = {
      date = os.date("%Y-%m-%d", day),
      journal = os.date("%Y%m%d", day),
   }
   local expanded = {}
   for key, value in pairs(params) do
      expanded[key] = tostring(value):gsub("{{%s*([%w_]+)%s*}}", function(name)
         return values[name] or ""
      end)
   end
   return expanded
end

function M.parse_track_uri(uri)
   uri = normalize_target(uri)
   if type(uri) ~= "string" or not vim.startswith(uri, "track://") then
      return nil
   end
   local rest = uri:sub(#"track://" + 1)
   local before_fragment = rest:match("^([^#]*)")
   local head, query = before_fragment:match("^([^?]*)%??(.*)$")
   local action = vim.trim((head or ""):gsub("^/+", ""):gsub("/+$", ""))
   if action == "" then
      return nil
   end
   return {
      action = action,
      params = expand_params(query_params(query)),
   }
end

function M.markdown_link_at_cursor(line, col)
   if type(line) ~= "string" then
      return nil
   end
   col = tonumber(col) or 1
   local search_from = 1
   while true do
      local start_pos, end_pos, label, target = line:find("%[([^%]]*)%]%(([^%)]+)%)", search_from)
      if not start_pos then
         return nil
      end
      if col >= start_pos and col <= end_pos then
         return {
            label = label,
            target = normalize_target(target),
            start_col = start_pos,
            end_col = end_pos,
         }
      end
      search_from = end_pos + 1
   end
end

local function run_open(params)
   local title = vim.trim(params.title or "")
   if title == "" then
      vim.notify("track: track://open requires title", vim.log.levels.ERROR)
      return
   end
   require("track.create").create(title, params.template)
end

local function run_journal(params)
   local offset = tonumber(params.offset or "0") or 0
   if params.date == "yesterday" then
      offset = -1
   elseif params.date == "tomorrow" then
      offset = 1
   end
   require("track.journal").open(offset, params.template)
end

function M.run(uri)
   local parsed = M.parse_track_uri(uri)
   if not parsed then
      return false
   end

   local action = parsed.action
   if action == "open" or action == "new" or action == "note" then
      run_open(parsed.params)
   elseif action == "journal" then
      run_journal(parsed.params)
   elseif action == "today" then
      parsed.params.offset = parsed.params.offset or "0"
      run_journal(parsed.params)
   else
      vim.notify("track: unknown action " .. action, vim.log.levels.ERROR)
   end
   return true
end

function M.run_markdown_link_at_cursor()
   local line = vim.api.nvim_get_current_line()
   local col = vim.fn.col(".")
   local link = M.markdown_link_at_cursor(line, col)
   if not link then
      return false
   end
   return M.run(link.target)
end

return M
