-- The only boundary between canonical Track identities and machine-local editor names.
-- The CLI owns the manifest and validates every alias. Never infer identity from a title filename.
local M = {}
local uv = vim.uv or vim.loop
local client = require("track.client")
local states = {} -- One session-wide guard, shared by every per-vault LSP client.
local installed = false

local function absolute(path)
   return vim.fn.fnamemodify(path, ":p"):gsub("/+$", "")
end

function M.enabled()
   return vim.env.TRACK_EDITOR_VIEW ~= nil and vim.env.TRACK_EDITOR_VIEW ~= ""
end

function M.canonical_path(path)
   if not path or path == "" then
      return ""
   end
   return uv.fs_realpath(path) or absolute(path)
end

local function in_view(path)
   if not M.enabled() then
      return false
   end
   local root = absolute(vim.env.TRACK_EDITOR_VIEW)
   path = absolute(path)
   return path == root or path:sub(1, #root + 1) == root .. "/"
end

local function track_path(path)
   if path == "" then
      return false
   end
   if in_view(path) then
      return true
   end
   local canonical = M.canonical_path(path)
   local root, kind = canonical:match("^(.*)/([^/]+)/%d+%.md$")
   if not root or (kind ~= "note" and kind ~= "journal") then
      return false
   end
   local stat = uv.fs_stat(root .. "/.track")
   local configured = require("track.config").options.vault_dir
   return (stat and stat.type == "directory") or (configured and M.canonical_path(root) == M.canonical_path(configured))
end

function M.info(path, check, source_path)
   local args = { "editor-path", "--path", path }
   if check then
      args[#args + 1] = "--check"
   end
   if source_path then
      vim.list_extend(args, { "--source-path", source_path })
   end
   local info, err = client.run_json(args)
   if not info then
      return nil, err
   end
   if type(info.path) ~= "string" or type(info.editor_path) ~= "string" then
      return nil, "editor-path returned an incomplete identity"
   end
   return info
end

function M.preferred(path)
   if not M.enabled() or not track_path(path) then
      return path
   end
   local info, err = M.info(path)
   if not info then
      return nil, err
   end
   return info.editor_path, nil, info
end

local function conflict(buf, reason)
   local message = "track editor view: " .. tostring(reason)
      .. ". Write blocked; preserve this buffer and any replacement file, compare them with the canonical note, and recover manually. No file was copied back or removed."
   states[buf] = states[buf] or {}
   states[buf].conflict = message
   if vim.api.nvim_buf_is_valid(buf) then
      vim.b[buf].track_editor_conflict = message
   end
   return nil, message
end

function M.attach(buf)
   if not vim.api.nvim_buf_is_valid(buf) or vim.bo[buf].buftype ~= "" then
      return true
   end
   local path = vim.api.nvim_buf_get_name(buf)
   if not M.enabled() or not track_path(path) then
      return true
   end
   if states[buf] then
      return not states[buf].conflict, states[buf].conflict
   end
   local info, err = M.info(path, true)
   if not info then
      return conflict(buf, err)
   end
   states[buf] = { path = info.path, name = absolute(path) }
   -- yes writes through a symlink. auto/no and either break* flag can replace its inode.
   vim.bo[buf].backupcopy = "yes"
   vim.b[buf].track_canonical_path = info.path
   return true
end

-- validate is also used before engine-backed mutations that do not run BufWritePre.
function M.validate(buf)
   buf = buf or vim.api.nvim_get_current_buf()
   local ok, err = M.attach(buf)
   if not ok then
      return nil, err
   end
   local state = states[buf]
   if not state then
      return true
   end
   if state.conflict then
      return nil, state.conflict
   end
   local name = vim.api.nvim_buf_get_name(buf)
   local info, problem = M.info(name, true)
   if not info then
      return conflict(buf, problem)
   end
   if info.path ~= state.path then
      return conflict(buf, "buffer identity changed from " .. state.path .. " to " .. info.path)
   end
   if not uv.fs_stat(state.path) then
      return conflict(buf, "canonical note is missing: " .. state.path)
   end
   if vim.bo[buf].backupcopy ~= "yes" then
      return nil, "track editor view: write requires setlocal backupcopy=yes (no breaksymlink or breakhardlink)"
   end
   for _, other in ipairs(vim.api.nvim_list_bufs()) do
      if other ~= buf and vim.api.nvim_buf_is_loaded(other) and vim.bo[other].buftype == "" then
         local other_name = vim.api.nvim_buf_get_name(other)
         local other_path = states[other] and states[other].path or M.canonical_path(other_name)
         if other_path == state.path then
            return nil, string.format(
               "track editor view: duplicate buffers %d and %d refer to %s; write blocked. Preserve edits and close the duplicate before saving.",
               buf, other, state.path
            )
         end
      end
   end
   return true
end

local function checked(buf)
   local ok, err = M.validate(buf)
   if not ok then
      error(err, 0)
   end
end

function M.setup()
   if installed then
      return
   end
   installed = true
   local group = vim.api.nvim_create_augroup("track_editor_view", { clear = true })
   vim.api.nvim_create_autocmd({ "BufReadPost", "BufNewFile", "BufEnter" }, {
      group = group,
      callback = function(ev)
         local ok, err = M.validate(ev.buf)
         if not ok and not vim.b[ev.buf].track_editor_conflict_reported then
            vim.b[ev.buf].track_editor_conflict_reported = true
            vim.notify(err, vim.log.levels.ERROR)
         end
      end,
   })
   vim.api.nvim_create_autocmd("BufWritePre", {
      group = group,
      callback = function(ev)
         checked(ev.buf)
      end,
   })
   vim.api.nvim_create_autocmd({ "FileWritePre", "FileAppendPre" }, {
      group = group,
      callback = function(ev)
         -- Partial writes / :write another-note bypass BufWritePre and cannot establish buffer
         -- ownership. Recovery copies to an unrelated path remain available.
         if M.enabled() and track_path(ev.file) then
            error("track editor view: partial, append, or alternate-target writes to Track notes are unsupported; open the note and use normal :write", 0)
         end
      end,
   })
   vim.api.nvim_create_autocmd("BufWritePost", {
      group = group,
      callback = function(ev)
         -- A formatter may run after our pre-hook and replace an alias with a regular file.
         -- The replacement stays untouched; make the recovery conflict visible and sticky.
         local function verify()
            if not vim.api.nvim_buf_is_valid(ev.buf) then
               return
            end
            local ok, err = M.validate(ev.buf)
            if not ok then
               vim.bo[ev.buf].modified = true
               vim.notify(err, vim.log.levels.ERROR)
            end
         end
         verify()
         -- Also run after later BufWritePost formatters; this is detection, not support for
         -- arbitrary tools that rewrite aliases. Never repair or copy their output automatically.
         vim.schedule(verify)
      end,
   })
   vim.api.nvim_create_autocmd("BufWipeout", {
      group = group,
      callback = function(ev)
         states[ev.buf] = nil
      end,
   })
   for _, buf in ipairs(vim.api.nvim_list_bufs()) do
      if vim.api.nvim_buf_is_loaded(buf) then
         M.attach(buf)
      end
   end
end

-- Openings from Track all go through here. Reuse identity, not a visible basename or one LSP client.
-- Arbitrary Markdown attachments still open normally; the alias API is only for Track notes.
function M.open(path, opts)
   opts = opts or {}
   M.setup()
   local target = path
   local canonical = M.canonical_path(path)
   if M.enabled() and track_path(path) then
      local info, err = M.info(path)
      if not info then
         vim.notify("track editor view: " .. tostring(err), vim.log.levels.ERROR)
         return nil, err
      end
      canonical, target = info.path, info.editor_path
   end
   local found
   for _, buf in ipairs(vim.api.nvim_list_bufs()) do
      if vim.api.nvim_buf_is_loaded(buf) and vim.bo[buf].buftype == "" then
         local identity = states[buf] and states[buf].path or M.canonical_path(vim.api.nvim_buf_get_name(buf))
         if identity == canonical then
            if found then
               local err = "duplicate buffers refer to " .. canonical .. "; preserve edits and close the duplicate"
               vim.notify("track editor view: " .. err, vim.log.levels.ERROR)
               return nil, err
            end
            found = buf
         end
      end
   end
   if found then
      vim.cmd((opts.keepalt and "keepalt " or "") .. "buffer " .. found)
   else
      vim.cmd((opts.keepalt and "keepalt " or "") .. "edit " .. vim.fn.fnameescape(target))
   end
   return vim.api.nvim_get_current_buf()
end

function M.current_source()
   local path = vim.api.nvim_buf_get_name(0)
   return track_path(path) and path or nil
end

function M.copy_link(path, source_path)
   path = path or vim.api.nvim_buf_get_name(0)
   source_path = source_path or M.current_source()
   local info, err = M.info(path, false, source_path)
   if not info or type(info.wikilink) ~= "string" or info.wikilink == "" then
      vim.notify("track: " .. tostring(err or "no wikilink returned"), vim.log.levels.ERROR)
      return nil
   end
   vim.fn.setreg('"', info.wikilink)
   if vim.fn.has("clipboard") == 1 then
      vim.fn.setreg("+", info.wikilink)
   end
   vim.notify("track: copied " .. info.wikilink, vim.log.levels.INFO)
   return info.wikilink
end

-- Generic file finders must enumerate the view, not the canonical vault, and follow file symlinks.
function M.generic_files(opts)
   if not M.enabled() then
      return nil, "set TRACK_EDITOR_VIEW to an absolute machine-local directory first"
   end
   local data, err = client.run_json({ "editor-view" })
   if not data then
      return nil, err
   end
   local ignore = vim.deepcopy((opts or {}).file_ignore_patterns or {})
   ignore[#ignore + 1] = "manifest%.json$"
   return vim.tbl_extend("force", opts or {}, { cwd = data.root, follow = true, file_ignore_patterns = ignore })
end

return M
