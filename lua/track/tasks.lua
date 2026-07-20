-- track.nvim task helpers: engine-backed mutations of the task line under the cursor.

local client = require("track.client")

local M = {}

-- cycle advances the task on the cursor line to the next state in the vault's state-set order,
-- wrapping at the end (`track task cycle`) — completion stamps, the sidecar log, and progress
-- cookies all apply. The buffer is written first so the engine rewrites what the user sees, then
-- reloaded to pick up the engine's edit; the render autocmds repaint the decorations from there.
function M.cycle()
   local buf = vim.api.nvim_get_current_buf()
   local path = vim.api.nvim_buf_get_name(buf)
   if path == "" then
      vim.notify("track: buffer has no file", vim.log.levels.WARN)
      return
   end
   if vim.bo[buf].modified then
      vim.cmd.write()
   end
   local line = vim.api.nvim_win_get_cursor(0)[1]
   local res, err = client.run_json({ "task", "cycle", "--path", path, "--line", tostring(line) })
   if not res then
      vim.notify("track: " .. tostring(err), vim.log.levels.ERROR)
      return
   end
   -- The buffer was just written, so a forced reload cannot lose anything; the cursor stays put.
   vim.cmd("silent edit!")
   vim.notify(("track: %s → %s"):format(res.from, res.state), vim.log.levels.INFO)
end

return M
