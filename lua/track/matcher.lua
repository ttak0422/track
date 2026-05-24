-- Longest-match, non-overlapping keyword scanner. Mirrors the Go engine's
-- match package so highlighting agrees with the indexed link graph. CJK has no
-- word boundaries, so this is pure substring matching by design.
--
-- This module is intentionally free of any `vim` dependency so it can be unit
-- tested with a bare Lua interpreter.

local M = {}

local function utf8_seqlen(byte)
   if byte < 0x80 then
      return 1
   elseif byte < 0xC0 then
      return 1 -- stray continuation byte; advance one to stay safe
   elseif byte < 0xE0 then
      return 2
   elseif byte < 0xF0 then
      return 3
   else
      return 4
   end
end

-- build constructs a matcher from a keyword list (entries with .term,
-- .note_id, .path). Terms are bucketed by first byte and sorted longest-first
-- within each bucket; duplicate terms keep the first entry.
function M.build(keywords)
   local by_first = {}
   local seen = {}
   for _, k in ipairs(keywords) do
      local term = k.term
      if term and term ~= "" and not seen[term] then
         seen[term] = true
         local fb = string.byte(term, 1)
         local bucket = by_first[fb]
         if not bucket then
            bucket = {}
            by_first[fb] = bucket
         end
         bucket[#bucket + 1] = {
            term = term,
            len = #term,
            note_id = k.note_id,
            path = k.path,
         }
      end
   end
   for _, bucket in pairs(by_first) do
      table.sort(bucket, function(a, b)
         return a.len > b.len
      end)
   end
   return setmetatable({ by_first = by_first }, { __index = M })
end

-- line scans a single line and returns matches as a list of
-- { s_col, e_col, term, note_id, path } using 0-based byte columns with an
-- exclusive end (ready for nvim_buf_set_extmark).
function M:line(text)
   local matches = {}
   local n = #text
   local i = 1 -- 1-based byte index into the Lua string
   while i <= n do
      local fb = string.byte(text, i)
      local bucket = self.by_first[fb]
      local hit = nil
      if bucket then
         for _, e in ipairs(bucket) do
            if e.len <= n - i + 1 and string.sub(text, i, i + e.len - 1) == e.term then
               hit = e
               break
            end
         end
      end
      if hit then
         matches[#matches + 1] = {
            s_col = i - 1,
            e_col = i - 1 + hit.len,
            term = hit.term,
            note_id = hit.note_id,
            path = hit.path,
         }
         i = i + hit.len
      else
         i = i + utf8_seqlen(fb)
      end
   end
   return matches
end

return M
