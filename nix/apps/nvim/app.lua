-- Real-use launcher config: track resolves its vault from the default config file
-- (config.yml at the platform user config path), not a test override.
require("track").setup({})
require("telescope").load_extension("track")

-- nvim-cmp wiring so [[ completion works out of the box. track advertises completion
-- via cmp-nvim-lsp capabilities; drive it through the nvim_lsp source.
local cmp = require("cmp")
cmp.setup({
   sources = {
      { name = "nvim_lsp" },
   },
   mapping = cmp.mapping.preset.insert({
      ["<C-Space>"] = cmp.mapping.complete(),
      ["<C-n>"] = cmp.mapping.select_next_item(),
      ["<C-p>"] = cmp.mapping.select_prev_item(),
      ["<C-y>"] = cmp.mapping.confirm({ select = true }),
   }),
})
