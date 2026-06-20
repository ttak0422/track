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
   local ok_sorters, sorters = pcall(require, "telescope.sorters")
   if not (ok_pickers and ok_finders and ok_conf and ok_actions and ok_state and ok_display and ok_sorters) then
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
      sorters = sorters,
   }
end

-- Display title for one result, falling back to the note id when untitled.
local function result_title(result)
   return result.title or ("#" .. tostring(result.note_id or "?"))
end

-- Fields shared by every entry regardless of scope. lnum seeds the previewer's
-- initial line and is reused by open_selection when jumping into the note.
local function base_entry(result, lnum)
   local path = result.path or ""
   return {
      value = result,
      path = path,
      filename = path,
      lnum = lnum,
   }
end

-- Build a scope-aware entry maker.
-- Title search shows just the title; body search shows the title alongside the
-- matched line, and positions the previewer/cursor on that line.
local function make_entry_maker(telescope, scope)
   if scope ~= "body" then
      return function(result)
         local entry = base_entry(result, 1)
         entry.display = result_title(result)
         entry.ordinal = entry.display
         return entry
      end
   end

   local displayer = telescope.entry_display.create({
      separator = "  ",
      items = { { width = 30 }, { remaining = true } },
   })
   return function(result)
      local line = (result.line and result.line > 0) and result.line or 1
      local entry = base_entry(result, line)
      local title = result_title(result)
      local snippet = result.snippet or ""
      entry.display = function()
         return displayer({ title, { snippet, "Comment" } })
      end
      entry.ordinal = title .. " " .. snippet
      return entry
   end
end

-- Open the note backing `selection`, jumping to its matched line when present.
local function open_note(selection)
   vim.cmd.edit(vim.fn.fnameescape(selection.value.path))
   local line = selection.value.line
   if line and line > 0 then
      pcall(vim.api.nvim_win_set_cursor, 0, { line, 0 })
      vim.cmd("normal! zz")
   end
end

local function open_selection(telescope, prompt_bufnr)
   local selection = telescope.action_state.get_selected_entry()
   telescope.actions.close(prompt_bufnr)
   if not selection or not selection.value or not selection.value.path then
      return
   end
   open_note(selection)
end

-- Handle <CR> for the search pickers: open the highlighted note, or—when the
-- search matched nothing—create a note titled with the current prompt. Note
-- creation is gated behind <CR> alone so that moving through the results
-- (e.g. <C-n>) never mints a note by accident.
local function select_or_create(telescope, prompt_bufnr)
   local selection = telescope.action_state.get_selected_entry()
   local title = vim.trim(telescope.action_state.get_current_line() or "")
   telescope.actions.close(prompt_bufnr)
   if selection and selection.value and selection.value.path then
      open_note(selection)
      return
   end
   if title == "" then
      vim.notify("track: type a title to create a note", vim.log.levels.WARN)
      return
   end
   require("track.create").create(title)
end

local function backlink_title(backlink)
   return backlink.title or ("#" .. tostring(backlink.note_id or "?"))
end

-- Build entries for the backlinks picker: the note title is the primary column and the matched line
-- is shown alongside it, so the opaque epoch filename never surfaces. value carries the path and line
-- that open_selection jumps to.
local function make_backlink_entry(telescope)
   local displayer = telescope.entry_display.create({
      separator = "  ",
      items = { { width = 30 }, { remaining = true } },
   })
   return function(backlink)
      local start = (backlink.range or {}).start or {}
      local line = (start.line or 0) + 1
      local title = backlink_title(backlink)
      local preview = backlink.preview or ""
      return {
         value = { path = backlink.path, line = line },
         path = backlink.path,
         filename = backlink.path,
         lnum = line,
         display = function()
            return displayer({ title, { preview, "Comment" } })
         end,
         ordinal = title .. " " .. preview,
      }
   end
end

local function make_template_entry(template)
   local name = template.name or ("#" .. tostring(template.id or "?"))
   local path = template.path or ""
   return {
      value = template,
      display = name,
      ordinal = name,
      path = path,
      filename = path,
      lnum = 1,
   }
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
         -- The CLI already ranks results (title exact > prefix > mtime DESC) and the
         -- dynamic finder re-runs the query on each keystroke, so the backend is the
         -- source of truth for both filtering and order. An identity sorter preserves
         -- that order; generic_sorter would re-rank by fuzzy proximity to the prompt,
         -- demoting frequently-updated notes and dropping non-fuzzy-matching hits.
         sorter = telescope.sorters.empty(),
         previewer = telescope.conf.file_previewer(picker_opts),
         attach_mappings = function(prompt_bufnr)
            -- <CR> opens the highlighted note, or creates one titled with the prompt when the
            -- search finds nothing. Creation lives on <CR> alone so list navigation never
            -- creates a note by accident.
            telescope.actions.select_default:replace(function()
               select_or_create(telescope, prompt_bufnr)
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

-- backlinks shows the notes linking to the current note in a Telescope picker, titled by note title
-- with the matched line as context. The epoch filename stays hidden in the entry value.
function M.backlinks(opts)
   opts = opts or {}
   local telescope = load_telescope()
   if not telescope then
      return
   end

   require("track.backlinks").request(function(result)
      if #result == 0 then
         vim.notify("track: no backlinks", vim.log.levels.INFO)
         return
      end
      local picker_opts = vim.tbl_extend("force", opts, {})
      telescope.pickers
         .new(picker_opts, {
            prompt_title = "Track backlinks",
            finder = telescope.finders.new_table({
               results = result,
               entry_maker = make_backlink_entry(telescope),
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
   end)
end

function M.search_templates(opts)
   opts = opts or {}
   local telescope = load_telescope()
   if not telescope then
      return
   end

   local data, err = client.run_json({ "template", "list" })
   if not data then
      vim.notify("track: " .. tostring(err), vim.log.levels.ERROR)
      return
   end
   local picker_opts = vim.tbl_extend("force", opts, {
      default_text = opts.default_text or opts.query,
   })

   telescope.pickers
      .new(picker_opts, {
         prompt_title = "Track templates",
         finder = telescope.finders.new_table({
            results = data.templates or {},
            entry_maker = make_template_entry,
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

return M
