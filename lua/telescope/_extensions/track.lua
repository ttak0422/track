local telescope = require("telescope")
local track = require("track.telescope")

return telescope.register_extension({
   exports = {
      search_title = track.search_title,
      search_body = track.search_body,
      search_templates = track.search_templates,
   },
})
