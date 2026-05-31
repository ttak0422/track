require("track").setup({})
-- Telescope's default horizontal layout hides the previewer when the window is
-- narrower than preview_cutoff (120). Force it on so the test instance always
-- shows the note preview regardless of terminal width.
require("telescope").setup({
   defaults = {
      layout_config = { preview_cutoff = 0 },
   },
})
require("telescope").load_extension("track")

-- Minimal nvim-cmp wiring so [[ completion can be exercised in the test instance.
-- track advertises completion via cmp-nvim-lsp capabilities; here we drive it through the nvim_lsp source.
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
