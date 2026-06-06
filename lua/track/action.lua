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

local function angle_target(target)
   target = vim.trim(tostring(target or ""))
   if target:sub(1, 1) == "<" and target:sub(-1) == ">" then
      return vim.trim(target:sub(2, -2))
   end
end

local track_actions = {
   journal = true,
   note = true,
}

local function day_from_params(params)
   local offset = tonumber(params.offset or "") or 0
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

function M.parse_action(spec)
   if type(spec) ~= "string" then
      return nil
   end
   local before_fragment = spec:match("^([^#]*)")
   local head, query = before_fragment:match("^([^?]*)%??(.*)$")
   local action = vim.trim((head or ""):gsub("^/+", ""):gsub("/+$", ""))
   if not track_actions[action] then
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
            target = target,
            action_target = angle_target(target),
            start_col = start_pos,
            end_col = end_pos,
         }
      end
      search_from = end_pos + 1
   end
end

local function run_note(params)
   local title = vim.trim(params.title or "")
   if title == "" then
      vim.notify("track: note action requires title", vim.log.levels.ERROR)
      return
   end
   require("track.create").create(title, params.template)
end

local function run_journal(params)
   if params.offset == nil or params.offset == "" then
      vim.notify("track: journal action requires offset", vim.log.levels.ERROR)
      return
   end
   local offset = tonumber(params.offset)
   if not offset then
      vim.notify("track: journal action offset must be a number", vim.log.levels.ERROR)
      return
   end
   require("track.journal").open(offset, params.template)
end

function M.run(uri)
   local parsed = M.parse_action(uri)
   if not parsed then
      return false
   end

   local action = parsed.action
   if action == "note" then
      run_note(parsed.params)
   elseif action == "journal" then
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
   if not link.action_target then
      return false
   end
   return M.run(link.action_target)
end

return M
