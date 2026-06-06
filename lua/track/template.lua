-- Template editing and template-backed note creation.

local client = require("track.client")

local M = {}

local function template_names(callback)
   local data, err = client.run_json({ "template", "list" })
   if not data then
      vim.notify("track: " .. tostring(err), vim.log.levels.ERROR)
      callback({})
      return
   end
   local names = {}
   for _, template in ipairs(data.templates or {}) do
      if template.name then
         names[#names + 1] = template.name
      end
   end
   table.sort(names)
   callback(names)
end

function M.complete(arg_lead)
   local data = client.run_json({ "template", "list" })
   if not data then
      return {}
   end
   local out = {}
   for _, template in ipairs(data.templates or {}) do
      local name = template.name
      if name and vim.startswith(name, arg_lead or "") then
         out[#out + 1] = name
      end
   end
   table.sort(out)
   return out
end

function M.open(name)
   name = vim.trim(name or "")
   if name == "" then
      vim.ui.input({ prompt = "Template name: " }, function(input)
         if input and vim.trim(input) ~= "" then
            M.open(input)
         end
      end)
      return
   end

   local data, err = client.run_json({ "template", "open", "--name", name })
   if not data then
      vim.notify("track: " .. tostring(err), vim.log.levels.ERROR)
      return
   end
   vim.cmd.edit(vim.fn.fnameescape(data.path))
end

function M.create_note(template, title)
   template = vim.trim(template or "")
   if template == "" then
      template_names(function(names)
         vim.ui.select(names, { prompt = "Template" }, function(choice)
            if choice then
               M.create_note(choice, title)
            end
         end)
      end)
      return
   end

   title = vim.trim(title or "")
   if title == "" then
      vim.ui.input({ prompt = "Note title: " }, function(input)
         if input and vim.trim(input) ~= "" then
            M.create_note(template, input)
         end
      end)
      return
   end

   require("track.create").create(title, template)
end

return M
