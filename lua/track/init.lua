-- track.nvim - Neovim frontend for the `track` note CLI.

local config = require("track.config")
local client = require("track.client")

local M = {}

function M.dump()
   return client.run({ "dump" })
end

function M.setup(opts)
   config.setup(opts)
   if config.options.lsp then
      require("track.lsp").setup()
   else
      require("track.autolink").setup()
   end
   require("track.commands").setup()
end

return M
