-- Metadata editor: a floating YAML buffer over the current note's editable sidecar metadata —
-- title, tags, description, cover image, and typed props — saved through `track meta --edit -` so
-- parsing and validation live in the CLI (a changed title renames the note, backlinks included).
-- Saving (:w) applies the whole document atomically; a rejected document (e.g. an image that is
-- not a vault asset, or a prop breaking the configured schema) surfaces the CLI error and keeps
-- the popup open, a successful save closes it.
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
      "# note metadata — :w validates and saves; q closes",
      "# edit title / tags / description / image / props (a changed title renames the note)",
   }
   for _, line in ipairs(vim.split(meta.doc or "", "\n", { trimempty = true })) do
      table.insert(lines, line)
   end

   local buf = vim.api.nvim_create_buf(false, false)
   vim.api.nvim_buf_set_name(buf, "track://meta/" .. tostring(meta.id))
   vim.api.nvim_buf_set_lines(buf, 0, -1, false, lines)
   vim.bo[buf].filetype = "yaml"
   vim.bo[buf].buftype = "acwrite"
   vim.bo[buf].bufhidden = "wipe"
   vim.bo[buf].modified = false

   local width = math.min(math.max(60, math.floor(vim.o.columns * 0.5)), vim.o.columns - 4)
   local height = math.min(#lines, math.max(3, vim.o.lines - 6))
   local win = vim.api.nvim_open_win(buf, true, {
      relative = "editor",
      width = width,
      height = height,
      row = math.max(1, math.floor((vim.o.lines - height) / 2) - 1),
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
         -- The buffer text is the document; header lines are YAML comments the CLI ignores. No
         -- client-side parsing — the engine validates and applies the whole document atomically.
         local doc = table.concat(vim.api.nvim_buf_get_lines(buf, 0, -1, false), "\n") .. "\n"
         local saved, save_err = client.run_json({ "meta", "--path", note_path, "--edit", "-" }, doc)
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
