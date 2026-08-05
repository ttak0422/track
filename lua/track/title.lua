local client = require("track.client")
local config = require("track.config")

local M = {}

local ns = vim.api.nvim_create_namespace("track_title")
local titles = {}

local function fetch_title(path)
   local meta, err = client.run_json({ "meta", "--path", path })
   if not meta then
      return nil
   end
   return meta.title
end

function M.render(buf)
   if not vim.api.nvim_buf_is_valid(buf) then
      return
   end
   vim.api.nvim_buf_clear_namespace(buf, ns, 0, -1)
   local title = titles[buf]
   if not title or title == "" then
      return
   end
   vim.api.nvim_buf_set_extmark(buf, ns, 0, 0, {
      virt_lines = { { { title, config.options.title_hl } } },
      virt_lines_above = true,
      priority = 90,
   })
end

function M.attach(buf)
   local path = vim.api.nvim_buf_get_name(buf)
   if path == "" then
      return
   end
   local title = fetch_title(path)
   if not title then
      return
   end
   titles[buf] = title
   M.render(buf)
end

function M.copy_title()
   local buf = vim.api.nvim_get_current_buf()
   local title = titles[buf]
   if not title then
      vim.notify("track: no title cached for this buffer", vim.log.levels.INFO)
      return
   end
   vim.fn.setreg("+", title)
   vim.notify("track: copied title: " .. title, vim.log.levels.INFO)
end

function M.setup()
   vim.api.nvim_set_hl(0, "TrackTitle", { default = true, bold = true })
   vim.api.nvim_set_hl(0, "TrackTitleBg", { default = true, link = "Normal" })

   local group = vim.api.nvim_create_augroup(config.options.augroup .. "_title", { clear = true })

   vim.api.nvim_create_autocmd("BufWritePost", {
      group = group,
      pattern = "*.md",
      callback = function(ev)
         if not titles[ev.buf] then
            return
         end
         local title = fetch_title(vim.api.nvim_buf_get_name(ev.buf))
         if title and title ~= titles[ev.buf] then
            titles[ev.buf] = title
            M.render(ev.buf)
         end
      end,
   })

   vim.api.nvim_create_autocmd("BufWipeout", {
      group = group,
      callback = function(ev)
         titles[ev.buf] = nil
      end,
   })
end

return M
