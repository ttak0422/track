package.path = './lua/?.lua;' .. package.path
local scheduled, jobs, notices = {}, {}, {}
local payloads = {}
vim = {
   trim = function(s) return (s:gsub('^%s+', ''):gsub('%s+$', '')) end,
   schedule = function(cb) scheduled[#scheduled + 1] = cb end,
   islist = function(t)
      if type(t) ~= 'table' or getmetatable(t) then return false end
      local n = #t
      for k in pairs(t) do if type(k) ~= 'number' or k < 1 or k > n or k % 1 ~= 0 then return false end end
      return true
   end,
   json = { decode = function(s) if payloads[s] == nil then error('bad json') end return payloads[s] end },
   notify = function(s) notices[#notices + 1] = s end,
   log = { levels = { ERROR = 3 } },
   system = function(argv, options, callback)
      local job = { argv = argv, callback = callback, killed = false }
      function job:kill() self.killed = true end
      jobs[#jobs + 1] = job
      return job
   end,
}
package.loaded['track.client'] = { bin = function() return '/path to/track' end }
local pending = {}
package.loaded['track.editor_view'] = { preferred_async = function(path, callback)
   local request = { path = path, callback = callback }
   pending[#pending + 1] = request
   return function() request.cancelled = true end
end }
local finder_mod = require('track.search_finder')
local function flush()
   while #scheduled > 0 do table.remove(scheduled, 1)() end
end
local function exit(job, stdout, code, stderr)
   job.callback({stdout = stdout, stderr = stderr or '', code = code or 0, signal = 0})
   flush()
end
local emitted, completions = {}, 0
local finder = finder_mod.new({scope = 'body', limit = 42, entry_maker = function(result, path)
   return { value = result, filename = path }
end})
local function run(prompt)
   finder(prompt, function(e) emitted[#emitted + 1] = e end, function() completions = completions + 1 end)
end
run(' A ; $(bad) ')
assert(jobs[1].argv[1] == '/path to/track' and jobs[1].argv[6] == 'A ; $(bad)' and jobs[1].argv[8] == '42')
assert(not finder:can_select('A ; $(bad)') and #emitted == 0)
run('new')
assert(jobs[1].killed and #jobs == 2)
payloads.old = {results = {{path='/old'}}}
exit(jobs[1], 'old')
assert(#pending == 0 and completions == 0, 'old search must not complete or emit')
payloads.new = {results = {{path='/1'}, {path='/2'}, {path='/3'}, {path='/4'}, {path='/5'}}}
exit(jobs[2], 'new')
assert(#pending == 4 and not finder:can_select('new'), 'alias concurrency must be bounded')
pending[2].callback('/alias2'); assert(#pending == 5)
pending[5].callback('/alias5'); pending[4].callback('/alias4'); pending[3].callback('/alias3')
assert(#emitted == 0)
pending[1].callback('/alias1')
assert(#emitted == 5 and emitted[1].filename == '/alias1' and emitted[2].filename == '/alias2')
assert(emitted[5].index == 5 and completions == 1 and finder:can_select(' new ') and finder.result_count == 5)
assert(finder:owns(emitted[1]) and not finder:owns({}), 'only current entries belong to finder')
assert(not finder:can_select('newer'), 'debounced prompt changes must invalidate Enter')
run('another'); assert(not finder:owns(emitted[1]), 'new prompt must invalidate old selection'); payloads.more = {results={{path='/x'}}}; exit(jobs[3], 'more')
assert(#pending == 6)
run('')
assert(pending[6].cancelled and finder:can_select('') and completions == 2)
pending[6].callback('/late'); assert(#emitted == 5 and completions == 2)
run('bad'); exit(jobs[4], 'broken', 0)
assert(finder.status == 'error' and not finder:can_select('bad') and completions == 3)
run('failure'); exit(jobs[5], '', 2, 'failure detail')
assert(finder.status == 'error' and notices[#notices]:match('failure detail'))
run('closing'); finder:close(); finder:close()
assert(jobs[6].killed); exit(jobs[6], 'new')
assert(not finder:can_select('closing') and completions == 4)
assert(finder:reopen('closing'), 'interrupted cached picker must refresh')
run('resumed'); payloads.resumed = {results = {}}
exit(jobs[7], 'resumed')
assert(finder:can_select('resumed'))
finder:close()
assert(not finder:reopen('resumed') and finder:can_select('resumed'), 'completed cached picker should remain usable')



finder:close()
assert(finder:reopen('typed but not searched'), 'a changed cached prompt must refresh')

-- Synchronous identity resolutions do not recurse or stall, even with many hits.
package.loaded['track.editor_view'].preferred_async = function(path, cb) cb(path); return function() end end
local many, complete_many = 0, 0
local synchronous = finder_mod.new({scope='title', entry_maker=function(result, path) return {filename=path} end})
synchronous('lots', function() many=many+1 end, function() complete_many=complete_many+1 end)
payloads.lots = {results = {}}
for i=1,2000 do payloads.lots.results[i]={path='/n/'..i} end
exit(jobs[8], 'lots')
assert(many == 2000 and complete_many == 1 and synchronous:can_select('lots'))

-- An alias failure must not be mistaken for a successful empty search.
package.loaded['track.editor_view'].preferred_async = function(_, cb) cb(nil, 'alias failed'); return function() end end
local failures, failure_complete = 0, 0
local fail_finder = finder_mod.new({scope='title',entry_maker=function() failures=failures+1 end})
fail_finder('x', function() failures=failures+1 end, function() failure_complete=failure_complete+1 end)
payloads.one = {results={{path='/x'}}}; exit(jobs[9], 'one')
assert(failures == 0 and failure_complete == 1 and fail_finder.status == 'error' and not fail_finder:can_select('x'))


local invalid = finder_mod.new({scope='title',entry_maker=function() error('invalid result must not be emitted') end})
local invalid_done = 0
invalid('bad entry', function() error('invalid result must not be emitted') end, function() invalid_done=invalid_done+1 end)
payloads.invalid = {results={{title='missing path'}}}; exit(jobs[10], 'invalid')
assert(invalid_done == 1 and invalid.status == 'error' and invalid.result_count == nil)
invalid('bad shape', function() error('invalid result must not be emitted') end, function() invalid_done=invalid_done+1 end)
payloads.invalidshape = {results={wrong='shape'}}; exit(jobs[11], 'invalidshape')
assert(invalid_done == 2 and invalid.status == 'error' and not invalid:can_select('bad shape'))
invalid('missing results', function() error('missing results must not be emitted') end, function() invalid_done=invalid_done+1 end)
payloads.missing = {}; exit(jobs[#jobs], 'missing')
assert(invalid_done == 3 and invalid.status == 'error' and not invalid:can_select('missing results'))
invalid('object results', function() error('object results must not be emitted') end, function() invalid_done=invalid_done+1 end)
payloads.object = {results=setmetatable({}, {})}; exit(jobs[#jobs], 'object')
assert(invalid_done == 4 and invalid.status == 'error' and not invalid:can_select('object results'))

-- Exercise the real eligibility check and CLI alias validation.
package.loaded['track.editor_view'] = nil
package.loaded['track.config'] = {options = {vault_dir = '/vault'}}
vim.env = { TRACK_EDITOR_VIEW = '/view' }
vim.uv = { fs_realpath = function(path) return path end, fs_stat = function(path) if path == '/vault/.track' then return {type='directory'} end end }
vim.fn = {fnamemodify = function(path) return path end}
local editor = require('track.editor_view')
local resolved
editor.preferred_async('/outside/file.md', function(path) resolved = path end)
assert(resolved == '/outside/file.md' and #jobs == 13)
editor.preferred_async('/vault/note/123.md', function(path, err) resolved = path or err end)
assert(#jobs == 14 and jobs[14].argv[2] == 'editor-path')
payloads.alias = {path='/vault/note/123.md', editor_path='/view/title.md'}
exit(jobs[14], 'alias'); assert(resolved == '/view/title.md')
vim.env.TRACK_EDITOR_VIEW = ''
editor.preferred_async('/vault/note/456.md', function(path) resolved = path end)
assert(resolved == '/vault/note/456.md' and #jobs == 14)
print('async search finder tests passed')
