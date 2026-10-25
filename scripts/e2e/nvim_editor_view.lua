-- CLI + LSP + Telescope integration. Requires the normal test-nvim runtime and freshly built
-- track + track-lsp. Owns and cleans an isolated vault/config/cache/view; no user vault is touched.
-- Every asynchronous assertion has a bounded timeout.
local test_root = vim.fn.tempname()
vim.fn.mkdir(test_root .. "/vault/.track", "p")
vim.env.TRACK_VAULT = test_root .. "/vault"
vim.env.TRACK_EDITOR_VIEW = test_root .. "/editor"
vim.env.TRACK_CONFIG = test_root .. "/machine.yml"
vim.fn.writefile({ "link_format: id-v1" }, vim.env.TRACK_VAULT .. "/.track/config.yml")
vim.fn.writefile({ "cache_dir: " .. test_root .. "/cache" }, vim.env.TRACK_CONFIG)
require("track.config").setup({ vault_dir = vim.env.TRACK_VAULT, watch_interval_ms = 0 })
vim.o.hidden = true
local client = require("track.client")
local editor = require("track.editor_view")
local uv = vim.uv or vim.loop
local function check(value, message)
   assert(value, message)
   return value
end
local function call(args, input)
   local result, err = client.run_json(args, input)
   return check(result, table.concat(args, " ") .. ": " .. tostring(err))
end
local function lsp_for(buf)
   check(vim.wait(5000, function()
      local clients = vim.lsp.get_clients({ name = "track-lsp", bufnr = buf })
      return clients[1] and clients[1].initialized
   end, 20), "track-lsp did not initialize")
   return vim.lsp.get_clients({ name = "track-lsp", bufnr = buf })[1]
end
local function request(buf, method, extra)
   local params = vim.tbl_extend("force", { textDocument = { uri = vim.uri_from_bufnr(buf) } }, extra or {})
   local response, err = lsp_for(buf):request_sync(method, params, 5000, buf)
   check(response and not response.err, "LSP " .. method .. ": " .. vim.inspect(response or err))
   return response.result
end
local function test()
   check(editor.enabled(), "TRACK_EDITOR_VIEW is required")
   call({ "init" })
   local target = call({ "new", "--id", "9200001", "--title", "Readable target 50% 日本語", "--body", "## Disk heading\n\nbody" })
   local target_info = check(editor.info(target.path))
   check(target_info.editor_path ~= target.path, "CLI did not return an alias")
   check(target_info.editor_path:find("50%", 1, true), "percent title missing from filename")
   check(target_info.wikilink:find("id:9200001", 1, true), "vault must use id-v1")
   local source = call({ "new", "--id", "9200002", "--title", "Readable source", "--body", "[[id:9200001|Readable target]]" })
   local third = call({ "new", "--id", "9200004", "--title", "Third source 30%", "--body", "[[id:9200001|Readable target]]" })

   -- Canonical-first openings retain that single buffer/URI, rather than making an alias copy.
   vim.cmd.edit(vim.fn.fnameescape(target.path))
   local canonical_buf = vim.api.nvim_get_current_buf()
   lsp_for(canonical_buf)
   check(editor.open(target_info.editor_path) == canonical_buf, "canonical-first buffer not reused")
   check(vim.api.nvim_buf_get_name(canonical_buf) == target.path, "canonical owner unexpectedly renamed")
   local source_buf = check(editor.open(source.path))
   local def = request(source_buf, "textDocument/definition", { position = { line = 0, character = 5 } })
   check(def and (def.uri or def[1] and def[1].uri) == vim.uri_from_fname(target.path), "definition did not retain canonical owner URI: " .. vim.inspect(def))
   vim.api.nvim_buf_delete(canonical_buf, { force = true })
   vim.wait(50)

   -- Alias-first openings retain the alias URI and expose unsaved content through canonical identity.
   local target_buf = check(editor.open(target.path))
   check(vim.api.nvim_buf_get_name(target_buf) == target_info.editor_path, "alias-first buffer not readable")
   lsp_for(target_buf)
   vim.api.nvim_buf_set_lines(target_buf, 0, -1, false, { "## Unsaved heading", "", "unsaved body" })
   -- Flush the target buffer's debounced didChange before requesting from another buffer.
   vim.wait(250)
   vim.api.nvim_set_current_buf(source_buf)
   def = request(source_buf, "textDocument/definition", { position = { line = 0, character = 5 } })
   check(def and (def.uri or def[1] and def[1].uri) == vim.uri_from_fname(target_info.editor_path), "definition did not retain alias owner URI: " .. vim.inspect(def))
   vim.api.nvim_buf_set_lines(source_buf, 0, -1, false, { "[[id:9200001##" })
   local completions = request(source_buf, "textDocument/completion", { position = { line = 0, character = #"[[id:9200001##" } })
   local found = false
   for _, item in ipairs(completions.items or completions) do
      if item.label == "Unsaved heading" then
         found = true
         local text = item.textEdit and item.textEdit.newText or item.insertText
         check(text and text:find("id:9200001", 1, true), "heading completion did not insert ID link")
      end
   end
   check(found, "unsaved alias heading missing from completion: " .. vim.inspect(completions))
   vim.api.nvim_buf_set_lines(source_buf, 0, -1, false, { "[[id:9200001|Readable target]]" })
   vim.cmd.write()
   vim.api.nvim_set_current_buf(target_buf)
   vim.cmd.write()
   check(uv.fs_lstat(target_info.editor_path).type == "link", "normal save replaced alias")
   check(vim.fn.readfile(target.path)[1] == "## Unsaved heading", "save did not reach canonical master")
   check(require("track.util").canonical_path(target_buf) == target.path, "canonical helper lost identity")
   check(editor.copy_link() == target_info.wikilink, "copy-link diverged from CLI format")
   local third_buf = check(editor.open(third.path))
   lsp_for(third_buf)
   local references = request(target_buf, "textDocument/references", { position = { line = 0, character = 0 }, context = { includeDeclaration = false } })
   local reference_paths = {}
   for _, ref in ipairs(references) do reference_paths[ref.uri] = true end
   check(reference_paths[vim.uri_from_bufnr(source_buf)], "references did not retain first source alias URI: " .. vim.inspect(references))
   check(reference_paths[vim.uri_from_bufnr(third_buf)], "references did not retain third-note alias URI: " .. vim.inspect(references))
   vim.api.nvim_set_current_buf(target_buf)

   -- Aliases stay pinned indefinitely, including after title changes. Metadata titles refresh.
   call({ "meta", "--path", target_info.editor_path, "--edit", "-" }, "title: Readable renamed 75% 日本語\n")
   local renamed = check(editor.info(target.path))
   check(renamed.editor_path == target_info.editor_path, "pinned alias moved after title change")
   check(uv.fs_lstat(target_info.editor_path).type == "link", "old alias was removed")
   check(editor.open(renamed.editor_path) == target_buf, "renamed alias duplicated buffer")
   check(editor.validate(target_buf), "old pinned alias stopped validating")
   require("track.title").attach(target_buf)
   local marks = vim.api.nvim_buf_get_extmarks(target_buf, vim.api.nvim_create_namespace("track_title"), 0, -1, { details = true })
   check(#marks > 0 and vim.inspect(marks):find("Readable renamed", 1, true), "metadata title did not refresh via alias")

   -- Indexed pickers carry actual readable filenames, while their values keep canonical paths.
   require("track.telescope").search_title({ query = "Readable renamed" })
   local action_state = require("telescope.actions.state")
   local prompt = vim.api.nvim_get_current_buf()
   local picker = action_state.get_current_picker(prompt)
   check(vim.wait(5000, function() return picker.manager and picker.manager:num_results() > 0 end, 20), "title picker did not populate")
   local entry = picker.manager:get_entry(1)
   check(entry.filename == renamed.editor_path and entry.path == renamed.editor_path, "picker filename is cosmetic/canonical: " .. vim.inspect(entry))
   check(entry.value.path == target.path, "picker changed canonical automation path")
   require("telescope.actions").close(prompt)

   -- Generic find_files enumerates the view with symlink-following, not just display transforms.
   require("track.telescope").files()
   prompt = vim.api.nvim_get_current_buf()
   picker = action_state.get_current_picker(prompt)
   check(vim.wait(5000, function() return picker.manager and picker.manager:num_results() > 0 end, 20), "generic file picker omitted aliases")
   local readable = false
   for i = 1, picker.manager:num_results() do
      entry = picker.manager:get_entry(i)
      local path = entry.path or entry.filename or entry.value
      if tostring(path):find("Readable target", 1, true) then readable = true end
      check(not tostring(path):find("manifest.json", 1, true), "manifest leaked into generic picker")
   end
   check(readable, "generic picker did not enumerate title alias")
   require("telescope.actions").close(prompt)

   -- Completion creates a real note, then replaces only its just-inserted title target with an ID.
   vim.api.nvim_set_current_buf(third_buf)
   local new_title = "Created from completion"
   vim.api.nvim_buf_set_lines(third_buf, 0, -1, false, { "[[" .. new_title })
   local create_items = request(third_buf, "textDocument/completion", { position = { line = 0, character = #new_title + 2 } })
   local create_item
   for _, item in ipairs(create_items.items or create_items) do
      if item.command and item.command.command == "track.createNote" then create_item = item end
   end
   check(create_item, "create-note completion missing")
   vim.lsp.util.apply_text_edits({ create_item.textEdit }, third_buf, "utf-8")
   local source_client = lsp_for(third_buf)
   vim.lsp.commands["track.createNote"](create_item.command, { bufnr = third_buf, client_id = source_client.id })
   check(vim.wait(5000, function()
      return vim.api.nvim_buf_get_lines(third_buf, 0, 1, false)[1]:find("[[id:", 1, true) ~= nil
   end, 20), "successful create did not insert an ID target")
   check(vim.api.nvim_buf_get_lines(third_buf, 0, 1, false)[1]:find("|Created from completion]]", 1, true), "create completion lost readable label")

   -- A late create result may not overwrite intervening user edits.
   local stale_title = "Created after source edit"
   vim.api.nvim_buf_set_lines(third_buf, 0, -1, false, { "[[" .. stale_title .. "]]" })
   local old_notify = vim.notify
   local warned = false
   vim.notify = function(message, ...)
      if tostring(message):find("source changed", 1, true) then warned = true end
      old_notify(message, ...)
   end
   vim.lsp.commands["track.createNote"]({ arguments = { {
      title = stale_title, uri = vim.uri_from_bufnr(third_buf), expected_text = stale_title,
      replacement_kind = "target",
      replacement_range = { start = { line = 0, character = 2 }, ["end"] = { line = 0, character = #stale_title + 2 } },
   } } }, { bufnr = third_buf, client_id = source_client.id })
   vim.api.nvim_buf_set_lines(third_buf, 0, -1, false, { "newer user draft" })
   local got_warning = vim.wait(5000, function() return warned end, 20)
   vim.notify = old_notify
   check(got_warning, "late create result did not report changed source")
   check(vim.api.nvim_buf_get_lines(third_buf, 0, 1, false)[1] == "newer user draft", "late result overwrote user edits")

   local journal = call({ "journal", "--offset", "-2" })
   local journal_buf = check(editor.open(journal.path))
   check(require("track.on_this_day").date_from_path(require("track.util").canonical_path(journal_buf)) ~= nil, "readable daily journal lost its date")

   -- Copy into another vault uses the SOURCE link mode, and qualifies the target vault.
   local vault = vim.env.TRACK_VAULT
   local other_vault = vim.fn.fnamemodify(vault, ":h") .. "/other-vault"
   check(vim.env.TRACK_CONFIG and vim.env.TRACK_CONFIG ~= "", "isolated TRACK_CONFIG required")
   vim.fn.writefile({ "cache_dir: " .. test_root .. "/cache", "default_vault: main", "vaults:", "  main: " .. vault, "  other: " .. other_vault }, vim.env.TRACK_CONFIG)
   call({ "--vault", "other", "init" })
   vim.fn.writefile({ "link_format: title" }, other_vault .. "/.track/config.yml")
   local other = call({ "--vault", "other", "new", "--id", "9200001", "--title", "Other target 10%", "--body", "legacy vault" })
   local cross_id = check(editor.copy_link(other.path, source.path))
   check(cross_id:find("[[other:id:9200001", 1, true), "ID source did not produce qualified ID link: " .. cross_id)
   local cross_legacy = check(editor.copy_link(target.path, other.path))
   check(cross_legacy == "[[main:Readable renamed 75% 日本語]]", "legacy source mode was ignored: " .. cross_legacy)

   -- Check the actual manifest service fails closed without repairing an external replacement.
   local damaged = call({ "new", "--id", "9200003", "--title", "Replaced alias", "--body", "master" })
   local damaged_info = check(editor.info(damaged.path))
   local damaged_buf = check(editor.open(damaged.path))
   assert(uv.fs_unlink(damaged_info.editor_path))
   vim.fn.writefile({ "external formatter output" }, damaged_info.editor_path)
   vim.api.nvim_buf_set_lines(damaged_buf, 0, -1, false, { "local draft" })
   local ok = pcall(vim.cmd, "write!")
   check(not ok, "replaced alias write was not blocked")
   check(vim.fn.readfile(damaged.path)[1] == "master", "guard copied replacement to master")
   check(vim.fn.readfile(damaged_info.editor_path)[1] == "external formatter output", "guard changed replacement")
   check(vim.b[damaged_buf].track_editor_conflict, "recovery conflict is not visible")
end
local ok, err = xpcall(test, debug.traceback)
for _, lsp in ipairs(vim.lsp.get_clients({ name = "track-lsp" })) do lsp:stop(true) end
for _, buf in ipairs(vim.api.nvim_list_bufs()) do pcall(vim.api.nvim_buf_delete, buf, { force = true }) end
vim.wait(30)
vim.fn.delete(test_root, "rf")
if not ok then
   print("track-e2e: FAIL readable editor view: " .. tostring(err))
   vim.cmd("cquit 1")
end
print("track-e2e: PASS readable editor view CLI/LSP/Telescope")
vim.cmd("qa!")
