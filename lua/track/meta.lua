-- Page-metadata editor: a floating YAML buffer over the current note, saved through `track meta`
-- so validation lives in the CLI. Saving (:w) runs the check; a rejected value (e.g. an image that
-- is not a vault asset) surfaces the CLI error and keeps the popup open, a successful save closes it.
local client = require("track.client")

local M = {}

-- Edit the current buffer's note metadata in a popup. The buffer must be a vault note file; the CLI
-- resolves it via --path and errors otherwise.
function M.edit()
   local note_path = vim.api.nvim_buf_get_name(0)
   if note_path == "" then
      vim.notify("track meta: current buffer has no file", vim.log.levels.ERROR)
      return
   end

   local meta, err = client.run_json({ "meta", "--path", note_path })
   if not meta then
      vim.notify("track meta: " .. err, vim.log.levels.ERROR)
      return
   end

   local lines = {
      "# page metadata — :w で検査して保存、q で閉じる",
      "# title / tags は :Track rename / :Track update で変更",
      "description: " .. (meta.description or ""),
      "image: " .. (meta.image or ""),
   }

   local buf = vim.api.nvim_create_buf(false, false)
   vim.api.nvim_buf_set_name(buf, "track://meta/" .. tostring(meta.id))
   vim.api.nvim_buf_set_lines(buf, 0, -1, false, lines)
   vim.bo[buf].filetype = "yaml"
   vim.bo[buf].buftype = "acwrite"
   vim.bo[buf].bufhidden = "wipe"
   vim.bo[buf].modified = false

   local width = math.min(math.max(60, math.floor(vim.o.columns * 0.5)), vim.o.columns - 4)
   local win = vim.api.nvim_open_win(buf, true, {
      relative = "editor",
      width = width,
      height = #lines,
      row = math.max(1, math.floor((vim.o.lines - #lines) / 2) - 1),
      col = math.floor((vim.o.columns - width) / 2),
      style = "minimal",
      border = "rounded",
      title = " note meta ",
      title_pos = "center",
   })

   local function close()
      if vim.api.nvim_win_is_valid(win) then
         vim.api.nvim_win_close(win, true)
      end
   end
   vim.keymap.set("n", "q", close, { buffer = buf, desc = "Close the meta popup" })

   vim.api.nvim_create_autocmd("BufWriteCmd", {
      buffer = buf,
      callback = function()
         local description, image = "", ""
         for _, line in ipairs(vim.api.nvim_buf_get_lines(buf, 0, -1, false)) do
            local d = line:match("^description:%s*(.*)$")
            if d then
               description = d
            end
            local i = line:match("^image:%s*(.*)$")
            if i then
               image = i
            end
         end
         local saved, save_err =
            client.run_json({ "meta", "--path", note_path, "--description", description, "--image", image })
         if not saved then
            -- Validation failed: report and keep the popup (and its modified state) for another try.
            vim.notify("track meta: " .. save_err, vim.log.levels.ERROR)
            return
         end
         vim.bo[buf].modified = false
         vim.notify("track meta: saved", vim.log.levels.INFO)
         close()
      end,
   })
end

return M
