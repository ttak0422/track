-- Local note graph rendering.

local client = require("track.client")

local M = {}

local function title(node)
   return node.title or ("#" .. tostring(node.note_id or "?"))
end

local function display_width(text)
   return vim.fn.strdisplaywidth(text or "")
end

local function pad(text, width)
   text = text or ""
   return text .. string.rep(" ", math.max(0, width - display_width(text)))
end

local function box(node, active)
   local text = " " .. title(node) .. " "
   local width = display_width(text)
   if active then
      return {
         "┏" .. string.rep("━", width) .. "┓",
         "┃" .. text .. "┃",
         "┗" .. string.rep("━", width) .. "┛",
      }
   end
   return {
      "┌" .. string.rep("─", width) .. "┐",
      "│" .. text .. "│",
      "└" .. string.rep("─", width) .. "┘",
   }
end

local function blank(width)
   return string.rep(" ", width)
end

local function box_width(lines)
   return display_width(lines[1] or "")
end

local function node_by_id(nodes)
   local out = {}
   for _, node in ipairs(nodes or {}) do
      out[node.note_id] = node
   end
   return out
end

local function graph_parts(graph)
   local nodes = node_by_id(graph.nodes)
   local center = nodes[graph.center_id]
   if not center then
      return nil, {}, {}
   end
   local incoming, outgoing = {}, {}
   local seen_in, seen_out = {}, {}
   for _, edge in ipairs(graph.edges or {}) do
      if edge.target_id == graph.center_id and edge.source_id ~= graph.center_id and nodes[edge.source_id] and not seen_in[edge.source_id] then
         incoming[#incoming + 1] = nodes[edge.source_id]
         seen_in[edge.source_id] = true
      end
      if edge.source_id == graph.center_id and edge.target_id ~= graph.center_id and nodes[edge.target_id] and not seen_out[edge.target_id] then
         outgoing[#outgoing + 1] = nodes[edge.target_id]
         seen_out[edge.target_id] = true
      end
   end
   return center, incoming, outgoing
end

local function render(graph)
   local center, incoming, outgoing = graph_parts(graph)
   if not center then
      return { "(current note is not indexed; run :Track reindex)" }, {}
   end

   local in_boxes, out_boxes = {}, {}
   local in_width, out_width = 0, 0
   for _, node in ipairs(incoming) do
      local b = box(node, false)
      in_boxes[#in_boxes + 1] = { node = node, lines = b }
      in_width = math.max(in_width, box_width(b))
   end
   for _, node in ipairs(outgoing) do
      local b = box(node, false)
      out_boxes[#out_boxes + 1] = { node = node, lines = b }
      out_width = math.max(out_width, box_width(b))
   end

   local center_box = box(center, true)
   local center_width = box_width(center_box)
   local rows = math.max(#in_boxes, #out_boxes, 1)
   local center_slot = math.floor((rows + 1) / 2)
   local lines, line_to_nodes = {}, {}
   for i = 1, rows do
      local left = in_boxes[i]
      local right = out_boxes[i]
      local mid = i == center_slot
      for j = 1, 3 do
         local l = left and pad(left.lines[j], in_width) or blank(in_width)
         local c = mid and center_box[j] or blank(center_width)
         local r = right and pad(right.lines[j], out_width) or blank(out_width)
         local left_edge = (left and mid and j == 2) and "──" or "  "
         local right_edge = (right and mid and j == 2) and "──" or "  "
         local row = pad(l, in_width) .. left_edge .. c .. right_edge .. r
         lines[#lines + 1] = row:gsub("%s+$", "")
         local line_no = #lines
         line_to_nodes[line_no] = {}
         if left then
            line_to_nodes[line_no][#line_to_nodes[line_no] + 1] = {
               start_col = 1,
               end_col = box_width(left.lines),
               path = left.node.path,
            }
         end
         if mid then
            line_to_nodes[line_no][#line_to_nodes[line_no] + 1] = {
               start_col = in_width + 3,
               end_col = in_width + 2 + center_width,
               path = center.path,
            }
         end
         if right then
            line_to_nodes[line_no][#line_to_nodes[line_no] + 1] = {
               start_col = in_width + center_width + 5,
               end_col = in_width + center_width + 4 + box_width(right.lines),
               path = right.node.path,
            }
         end
      end
      if i < rows then
         lines[#lines + 1] = ""
      end
   end
   if #incoming == 0 and #outgoing == 0 then
      lines[#lines + 1] = ""
      lines[#lines + 1] = "(no linked notes)"
   end
   return lines, line_to_nodes
end

function M.show()
   local path = vim.api.nvim_buf_get_name(0)
   if path == "" then
      vim.notify("track: buffer has no file", vim.log.levels.WARN)
      return
   end
   local data, err = client.run_json({ "graph", "--path", path })
   if not data then
      vim.notify("track: " .. tostring(err), vim.log.levels.ERROR)
      return
   end
   local lines, line_to_nodes = render(data.graph or {})
   local buf = vim.api.nvim_create_buf(true, true)
   vim.api.nvim_buf_set_name(buf, "track://graph")
   vim.api.nvim_set_option_value("bufhidden", "wipe", { buf = buf })
   vim.api.nvim_set_option_value("swapfile", false, { buf = buf })
   vim.api.nvim_set_option_value("filetype", "text", { buf = buf })
   vim.api.nvim_buf_set_lines(buf, 0, -1, false, lines)
   vim.api.nvim_buf_set_var(buf, "track_graph_nodes", line_to_nodes)
   vim.keymap.set("n", "<CR>", function()
      local nodes = vim.b.track_graph_nodes or {}
      local line = vim.api.nvim_win_get_cursor(0)[1]
      local col = vim.fn.virtcol(".")
      for _, node in ipairs(nodes[line] or {}) do
         if node.start_col <= col and col <= node.end_col and node.path and node.path ~= "" then
            vim.cmd.edit(vim.fn.fnameescape(node.path))
            return
         end
      end
   end, { buffer = buf, desc = "track: open graph node" })
   vim.api.nvim_set_current_buf(buf)
end

return M
