local log_path = vim.env.TRACK_FOLLOW_TRACE or "/tmp/track-follow-trace.log"
local ns = vim.api.nvim_create_namespace("track_follow_trace")

local function append(line)
   local fd = io.open(log_path, "a")
   if not fd then
      return
   end
   fd:write(os.date("%H:%M:%S"), " ", line, "\n")
   fd:close()
end

local function current_state(label)
   local win = vim.api.nvim_get_current_win()
   local buf = vim.api.nvim_get_current_buf()
   local cursor = vim.api.nvim_win_get_cursor(win)
   local map = vim.fn.maparg("<CR>", "n", false, true)
   append(string.format(
      "%s mode=%s win=%s buf=%s ft=%s file=%s row=%s col=%s map_expr=%s map_rhs=%s map_desc=%s",
      label,
      vim.api.nvim_get_mode().mode,
      win,
      buf,
      vim.bo[buf].filetype,
      vim.api.nvim_buf_get_name(buf),
      cursor[1],
      cursor[2],
      vim.inspect(map.expr),
      vim.inspect(map.rhs),
      vim.inspect(map.desc)
   ))
end

append("=== track follow trace start ===")
current_state("start")

vim.on_key(function(key)
   if key == "\r" or key == "\n" then
      current_state("on_key:<CR>")
   end
end, ns)

local group = vim.api.nvim_create_augroup("track_follow_trace", { clear = true })
for _, event in ipairs({ "BufEnter", "BufWritePost", "CmdlineEnter", "CmdlineLeave", "ModeChanged", "WinEnter" }) do
   vim.api.nvim_create_autocmd(event, {
      group = group,
      callback = function(ev)
         current_state("autocmd:" .. event .. ":" .. tostring(ev.match))
      end,
   })
end

local ok, follow = pcall(require, "track.follow")
if ok then
   if not follow._trace_original_smart_action then
      follow._trace_original_smart_action = follow.smart_action
      follow.smart_action = function(...)
         current_state("smart_action:before")
         local result = follow._trace_original_smart_action(...)
         append("smart_action:return " .. vim.inspect(result))
         return result
      end
   end

   if not follow._trace_original_follow then
      follow._trace_original_follow = follow.follow
      follow.follow = function(...)
         current_state("follow:before")
         local result = follow._trace_original_follow(...)
         vim.defer_fn(function()
            current_state("follow:after_defer")
         end, 300)
         return result
      end
   end
end

vim.notify("track follow trace enabled: " .. log_path, vim.log.levels.INFO)
