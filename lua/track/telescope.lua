-- Telescope-backed note search.

local client = require("track.client")

local M = {}

local function load_telescope()
   local ok_pickers, pickers = pcall(require, "telescope.pickers")
   local ok_finders, finders = pcall(require, "telescope.finders")
   local ok_conf, conf_mod = pcall(require, "telescope.config")
   local ok_actions, actions = pcall(require, "telescope.actions")
   local ok_state, action_state = pcall(require, "telescope.actions.state")
   local ok_display, entry_display = pcall(require, "telescope.pickers.entry_display")
   if not (ok_pickers and ok_finders and ok_conf and ok_actions and ok_state and ok_display) then
      vim.notify("track: telescope.nvim is required for this command", vim.log.levels.ERROR)
      return nil
   end
   return {
      pickers = pickers,
      finders = finders,
      conf = conf_mod.values,
      actions = actions,
      action_state = action_state,
      entry_display = entry_display,
   }
end

-- Build a scope-aware entry maker.
-- Title search shows just the title; body search shows the title alongside the
-- matched line, and positions the previewer/cursor on that line.
local function make_entry_maker(telescope, scope)
   if scope ~= "body" then
      return function(result)
         local title = result.title or ("#" .. tostring(result.note_id or "?"))
         return {
            value = result,
            display = title,
            ordinal = title,
            path = result.path or "",
            filename = result.path or "",
            lnum = 1,
         }
      end
   end

   local displayer = telescope.entry_display.create({
      separator = "  ",
      items = { { width = 30 }, { remaining = true } },
   })
   return function(result)
      local title = result.title or ("#" .. tostring(result.note_id or "?"))
      local snippet = result.snippet or ""
      local lnum = (result.line and result.line > 0) and result.line or 1
      return {
         value = result,
         display = function(entry)
            return displayer({ entry.value.title or title, { snippet, "Comment" } })
         end,
         ordinal = title .. " " .. snippet,
         path = result.path or "",
         filename = result.path or "",
         lnum = lnum,
      }
   end
end

local function open_selection(telescope, prompt_bufnr)
   local selection = telescope.action_state.get_selected_entry()
   telescope.actions.close(prompt_bufnr)
   if not selection or not selection.value or not selection.value.path then
      return
   end
   vim.cmd.edit(vim.fn.fnameescape(selection.value.path))
   local line = selection.value.line
   if line and line > 0 then
      pcall(vim.api.nvim_win_set_cursor, 0, { line, 0 })
      vim.cmd("normal! zz")
   end
end

local function pick(scope, opts)
   opts = opts or {}
   local telescope = load_telescope()
   if not telescope then
      return
   end

   local limit = tostring(opts.limit or 100)
   local picker_opts = vim.tbl_extend("force", opts, {
      default_text = opts.default_text or opts.query,
   })

   telescope.pickers
      .new(picker_opts, {
         prompt_title = "Track " .. scope .. " search",
         finder = telescope.finders.new_dynamic({
            fn = function(prompt)
               local query = vim.trim(prompt or "")
               if query == "" then
                  return {}
               end
               local data, err = client.run_json({ "search", "--scope", scope, "--query", query, "--limit", limit })
               if not data then
                  vim.notify("track: " .. tostring(err), vim.log.levels.ERROR)
                  return {}
               end
               return data.results or {}
            end,
            entry_maker = make_entry_maker(telescope, scope),
         }),
         sorter = telescope.conf.generic_sorter(picker_opts),
         previewer = telescope.conf.file_previewer(picker_opts),
         attach_mappings = function(prompt_bufnr)
            telescope.actions.select_default:replace(function()
               open_selection(telescope, prompt_bufnr)
            end)
            return true
         end,
      })
      :find()
end

function M.search_title(opts)
   pick("title", opts)
end

function M.search_body(opts)
   pick("body", opts)
end

return M
