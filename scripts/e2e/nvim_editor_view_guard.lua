-- Standalone headless regression: nvim --headless -u NONE -i NONE -l scripts/e2e/nvim_editor_view_guard.lua
-- Real Neovim buffers, symlinks and :write; only the CLI manifest service is stubbed.
vim.opt.rtp:prepend(vim.fn.getcwd())
vim.o.hidden = true
local uv = vim.uv or vim.loop
local root = vim.fn.tempname()
local vault = root .. "/vault"
local view = root .. "/view"
vim.fn.mkdir(vault .. "/.track", "p")
vim.fn.mkdir(vault .. "/note", "p")
vim.fn.mkdir(view, "p")
vim.env.TRACK_EDITOR_VIEW = view
local rows = {}
local messages = {}
vim.notify = function(message)
   messages[#messages + 1] = message
end
local function eq(actual, expected, message)
   assert(actual == expected, (message or "mismatch") .. ": expected " .. vim.inspect(expected) .. ", got " .. vim.inspect(actual))
end
local function note(id, title, other_vault)
   local base = other_vault or vault
   vim.fn.mkdir(base .. "/.track", "p")
   vim.fn.mkdir(base .. "/note", "p")
   local path = base .. "/note/" .. id .. ".md"
   local alias = view .. "/" .. title .. " -- " .. id .. ".md"
   vim.fn.writefile({ "original " .. id }, path)
   assert(uv.fs_symlink(path, alias))
   local row = { path = path, editor_path = alias, id = tonumber(id), title = title, file_kind = "note", vault_dir = base, wikilink = "[[id:" .. id .. "|" .. title .. "]]" }
   rows[path], rows[alias] = row, row
   return row
end
package.loaded["track.client"] = {
   run_json = function(args)
      if args[1] == "editor-view" then
         return { root = view, entries = {} }
      end
      eq(args[1], "editor-path")
      local path = args[3]
      local row = rows[path]
      if not row or not uv.fs_stat(row.path) then
         return nil, "missing canonical note"
      end
      if path ~= row.path then
         local stat = uv.fs_lstat(path)
         if not stat or stat.type ~= "link" or uv.fs_realpath(path) ~= row.path then
            return nil, "alias replaced or broken"
         end
      end
      return row
   end,
}
require("track.config").options.vault_dir = vault
local editor = require("track.editor_view")
editor.setup()
local function wipe()
   for _, buf in ipairs(vim.api.nvim_list_bufs()) do
      pcall(vim.api.nvim_buf_delete, buf, { force = true })
   end
end
local function set_body(text)
   vim.api.nvim_buf_set_lines(0, 0, -1, false, { text })
end
local function test()
   local first = note("101", "Readable 日本語")
   local buf = assert(editor.open(first.path))
   eq(vim.api.nvim_buf_get_name(buf), first.editor_path, "real buffer name is readable")
   eq(vim.bo[buf].backupcopy, "yes")
   eq(editor.open(first.editor_path), buf, "alias open reuses buffer")
   eq(editor.open(first.path), buf, "canonical open reuses buffer")
   eq(require("track.util").canonical_path(buf), first.path)
   eq(require("track.util").vault_of(buf), vault .. "/")
   eq(editor.copy_link(), first.wikilink, "copy uses CLI link")
   eq(vim.fn.getreg('"'), first.wikilink)
   local picker = assert(editor.generic_files({ cwd = "/wrong", follow = false }))
   eq(picker.cwd, view)
   eq(picker.follow, true)
   set_body("saved through alias")
   vim.cmd.write()
   eq(vim.fn.readfile(first.path)[1], "saved through alias")
   eq(uv.fs_lstat(first.editor_path).type, "link", "ordinary write preserves symlink")

   for _, option in ipairs({ "auto", "no", "yes,breaksymlink", "yes,breakhardlink" }) do
      vim.bo[buf].backupcopy = option
      set_body("must not save")
      local ok, err = pcall(vim.cmd.write)
      eq(ok, false, option .. " must fail")
      assert(tostring(err):find("backupcopy=yes", 1, true))
      eq(vim.fn.readfile(first.path)[1], "saved through alias")
   end
   vim.bo[buf].backupcopy = "yes"
   vim.api.nvim_buf_set_lines(buf, 0, -1, false, { "partial", "must not save" })
   eq(pcall(vim.cmd, "1write!"), false, "partial alias write bypasses normal save guard")
   eq(vim.fn.readfile(first.path)[1], "saved through alias")
   -- A buffer manually created under the other spelling must not compete with the first.
   -- Neovim normally deduplicates same-inode paths itself. Simulate independently opened
   -- buffers while a sync briefly removed the target, then restore it before checking.
   assert(uv.fs_rename(first.path, first.path .. ".hold"))
   local duplicate = vim.api.nvim_create_buf(true, false)
   vim.api.nvim_buf_set_name(duplicate, first.path)
   assert(uv.fs_rename(first.path .. ".hold", first.path))
   vim.api.nvim_buf_set_lines(duplicate, 0, -1, false, { "second draft" })
   local safe, reason = editor.validate(buf)
   eq(safe, nil, "duplicate identity blocks write")
   assert(reason:find("duplicate buffers", 1, true))
   eq(pcall(vim.cmd.write), false)
   vim.api.nvim_buf_delete(duplicate, { force = true })
   assert(editor.validate(buf))
   wipe()

   -- Identical IDs in different vaults are independent identities, even in one session.
   local a = note("102", "First vault")
   local b = note("102", "Second vault", root .. "/vault-two")
   local a_buf = assert(editor.open(a.path))
   local b_buf = assert(editor.open(b.path))
   assert(a_buf ~= b_buf)
   assert(editor.validate(a_buf))
   assert(editor.validate(b_buf))
   wipe()

   local broken = note("103", "Missing master")
   local broken_buf = assert(editor.open(broken.path))
   assert(uv.fs_unlink(broken.path))
   set_body("never resurrect")
   eq(pcall(vim.cmd.write), false, "missing master blocks write")
   eq(uv.fs_stat(broken.path), nil, "master not recreated")
   eq(uv.fs_lstat(broken.editor_path).type, "link")
   assert(vim.b[broken_buf].track_editor_conflict)
   wipe()

   local replaced = note("104", "Formatter conflict")
   local replaced_buf = assert(editor.open(replaced.path))
   -- Register after the guard, deliberately exercising a formatter replacing the link after pre-check.
   vim.api.nvim_create_autocmd("BufWritePre", {
      buffer = replaced_buf, once = true,
      callback = function()
         assert(uv.fs_unlink(replaced.editor_path))
         vim.fn.writefile({ "formatter replacement" }, replaced.editor_path)
      end,
   })
   set_body("buffer draft")
   pcall(vim.cmd, "write!")
   vim.wait(20)
   eq(vim.fn.readfile(replaced.path)[1], "original 104", "no copy-back to master")
   eq(uv.fs_lstat(replaced.editor_path).type, "file", "replacement retained")
   assert(vim.b[replaced_buf].track_editor_conflict, "replacement is a visible conflict")
   eq(vim.bo[replaced_buf].modified, true, "buffer preserved for recovery")
   eq(pcall(vim.cmd.write), false, "subsequent write remains blocked")
   wipe()

   local after = note("105", "Late formatter")
   local after_buf = assert(editor.open(after.path))
   vim.api.nvim_create_autocmd("BufWritePost", {
      buffer = after_buf, once = true,
      callback = function()
         assert(uv.fs_unlink(after.editor_path))
         vim.fn.writefile({ "late formatter replacement" }, after.editor_path)
      end,
   })
   set_body("saved before formatter")
   vim.cmd.write()
   vim.wait(20)
   eq(vim.fn.readfile(after.path)[1], "saved before formatter")
   eq(vim.fn.readfile(after.editor_path)[1], "late formatter replacement")
   assert(vim.b[after_buf].track_editor_conflict, "late post-hook detected")
   wipe()

   vim.env.TRACK_EDITOR_VIEW = nil
   eq(editor.preferred(first.path), first.path, "disabled preserves legacy path")
   eq(vim.api.nvim_buf_get_name(assert(editor.open(first.path))), first.path)
end
local ok, err = xpcall(test, debug.traceback)
wipe()
vim.fn.delete(root, "rf")
if not ok then
   io.stderr:write(tostring(err) .. "\n")
   vim.cmd("cquit 1")
end
print("track-e2e: PASS readable editor view guard")
vim.cmd("qa!")
