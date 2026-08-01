-- track.nvim task helpers: engine-backed mutations of the task line under the cursor.

local client = require("track.client")

local M = {}

local function buffer_etag(buf)
   local lines = vim.api.nvim_buf_get_lines(buf, 0, -1, false)
   local format = vim.bo[buf].fileformat
   local newline = format == "dos" and "\r\n" or format == "mac" and "\r" or "\n"
   local content = table.concat(lines, newline)
   if vim.bo[buf].eol then
      content = content .. newline
   end
   if vim.bo[buf].bomb then
      content = "\239\187\191" .. content
   end
   return vim.fn.sha256(content):sub(1, 32)
end

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
   local etag = buffer_etag(buf)
   local res, err = client.run_json({
      "task",
      "cycle",
      "--path",
      path,
      "--line",
      tostring(line),
      "--etag",
      etag,
   })
   if not res then
      if tostring(err):find("note changed on disk", 1, true) then
         vim.notify(
            "track: note changed on disk; buffer kept unchanged. Run :edit to reload, then retry.",
            vim.log.levels.WARN
         )
      else
         vim.notify("track: " .. tostring(err), vim.log.levels.ERROR)
      end
      return
   end
   -- The buffer was just written, so a forced reload cannot lose anything; the cursor stays put.
   vim.cmd("silent edit!")
   vim.notify(("track: %s → %s"):format(res.from, res.state), vim.log.levels.INFO)
end

return M
