-- Exercise explicit creation in both real Telescope pickers with a fake CLI.
local function check(ok, message)
   if not ok then error(message, 0) end
end
local tmp = vim.fn.tempname()
vim.fn.mkdir(tmp, "p")
local note = tmp .. "/note.md"
vim.fn.writefile({ "example" }, note)
local payload = vim.json.encode({ results = { { title = "早い", path = note, note_id = 1 } } })
local executable = tmp .. "/mock track"
vim.fn.writefile({ "#!/bin/sh", "printf '%s\n' '" .. payload .. "'" }, executable)
vim.fn.setfperm(executable, "rwx------")
require("track.client").bin = function() return executable end
package.loaded["track.vault"] = { current_name = function() return "" end }
local created, opened = {}, {}
package.loaded["track.create"] = { create = function(title) created[#created + 1] = title end }
require("track.editor_view").open = function(path) opened[#opened + 1] = path; return true end

local state = require("telescope.actions.state")
local prompt, picker
for _, scope in ipairs({ "title", "body" }) do
   require("track.telescope")["search_" .. scope]({})
   prompt = vim.api.nvim_get_current_buf()
   picker = state.get_current_picker(prompt)
   picker:set_prompt("早")
   check(vim.wait(5000, function()
      local entry = state.get_selected_entry()
      return picker.finder:can_select("早") and entry and picker.finder:owns(entry)
   end, 10), "fuzzy fixture was not selected")
   local function create_key(mode)
      local mapping = vim.fn.maparg("<C-o>", mode, false, true).callback
      check(type(mapping) == "function", "Ctrl-o mapping missing in " .. mode)
      mapping()
   end
   local count = #created
   picker:set_prompt("   ")
   create_key("i")
   check(vim.api.nvim_buf_is_valid(prompt) and #created == count, "blank Ctrl-o acted")
   picker:set_prompt("早")
   if scope == "body" then
      picker:set_prompt("  入力した新規タイトル  ")
   end
   create_key(scope == "title" and "i" or "n")
   check(#created == count + 1 and created[#created] == (scope == "title" and "早" or "入力した新規タイトル"),
      "Ctrl-o did not use the current " .. scope .. " prompt")
   check(#opened == 0, "Ctrl-o opened a search hit")
end
vim.fn.delete(tmp, "rf")
print("track-e2e: PASS telescope explicit creation")
vim.cmd("qa!")
