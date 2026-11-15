local host_vim = vim
package.path = './lua/?.lua;' .. package.path
local jobs, scheduled, payloads, emitted = {}, {}, {}, 0
vim = {
   schedule = function(cb) scheduled[#scheduled + 1] = cb end,
   uv = {hrtime = function() return 123 end},
   fn = {sha256 = function(s) return s end},
   api = {nvim_exec_autocmds = function() emitted = emitted + 1 end},
   json = {decode = function(s) if not payloads[s] then error('bad JSON') end return payloads[s] end,
      encode = function(data) return data end},
   system = function(argv, options, cb)
      local job = {argv = argv, options = options, callback = cb}
      function job:kill() self.killed = true end
      jobs[#jobs + 1] = job
      return job
   end,
}
package.loaded['track.client'] = {bin = function() return '/bin/track' end}
local m = dofile('lua/track/agent_timeline.lua')
local view = m.new('demo', {vault = 'work'})
local function finish(job, code, data)
   job.callback({code = code, signal = 0, stdout = data, stderr = 'failed'})
   while #scheduled > 0 do table.remove(scheduled, 1)() end
end
payloads.good = {schema_version = 1, project_id = 'demo', title = 'Demo', revision = 3,
   progress = {fraction = 0.5}, tasks = {{id = 'parent', title = 'First', status = 'active'},
   {id = 'child', parent_id = 'parent', title = 'Second', status = 'pending'}}}
view.refresh()
assert(jobs[1].argv[2] == 'timeline-project' and jobs[1].argv[3] == 'get')
assert(jobs[1].argv[6] == '--vault' and jobs[1].argv[7] == 'work')
view.refresh()
finish(jobs[1], 0, 'good')
assert(view.snapshot == nil)
finish(jobs[2], 0, 'good')
assert(view.snapshot.revision == 3 and view.rows()[3] == '  [pending] Second')
local req = view.request('task.upsert', {id = 'task'})
assert(req.expected_revision == 3 and req.source == 'nvim')
assert(req.event_id ~= view.request('task.upsert', {}).event_id)
local receipt, errors = 0, 0
view.apply(req, function(err) if err then errors = errors + 1 else receipt = receipt + 1 end end)
local write = jobs[3]
local blocked_refresh = false
view.refresh(function(err) blocked_refresh = err ~= nil end)
assert(blocked_refresh and #jobs == 3)
assert(write.options.stdin == req)
view.apply(req, function(err) assert(err); errors = errors + 1 end)
assert(#jobs == 3)
view.cancel()
assert(not write.killed)
finish(write, 0, 'good')
assert(receipt == 1 and errors == 1 and not view.writing)
view.apply(req, function(err) assert(err); errors = errors + 1 end)
finish(jobs[4], 1, '')
assert(errors == 2 and view.snapshot and view.error)
local n = #jobs
view.rows()
assert(#jobs == n)
local spec
package.loaded['komado.dsl'] = {Line = function(x) return x end}
package.loaded['komado.utils'] = {mapped_list = function(source, item) spec = {source = source, item = item}; return spec end}
local component = view.komado_component()
assert(component.update[1] == 'User' and component.update.pattern == 'TrackTimelineChanged')
assert(spec.item(spec.source()[1]).provider == 'Demo  50%')
assert(#jobs == n)
local timers = {}
vim.uv.new_timer = function()
   local timer = {}
   function timer:start(_, _, cb) self.callback = cb end
   function timer:stop() self.stopped = true end
   function timer:close() self.closed = true end
   timers[#timers + 1] = timer
   return timer
end
view.watch(1000)
local before_watch = #jobs
timers[1].callback()
while #scheduled > 0 do table.remove(scheduled, 1)() end
assert(#jobs == before_watch + 1)
-- Outstanding reads do not overlap.
timers[1].callback()
while #scheduled > 0 do table.remove(scheduled, 1)() end
assert(#jobs == before_watch + 1)
finish(jobs[#jobs], 0, 'good')
-- A queued tick from a disposed UI cannot issue a request.
timers[1].callback()
view.dispose()
while #scheduled > 0 do table.remove(scheduled, 1)() end
assert(#jobs == before_watch + 1 and timers[1].stopped and timers[1].closed)
view.watch(1000)
assert(#timers == 2)
view.apply(view.request('project.update', {title='Demo'}))
local pending_write = #jobs
timers[2].callback()
while #scheduled > 0 do table.remove(scheduled, 1)() end
assert(#jobs == pending_write)
finish(jobs[#jobs], 0, 'good')
view.dispose()
view.snapshot.reports = {{summary='Summary',task_id='parent'}}
view.snapshot.discussions = {{author='worker',target_id='parent',text='Message'}}
view.snapshot.controls = {{task_id='parent',action='pause',status='requested'}}
local rich = table.concat(view.rows(), '\n')
assert(rich:find('Report [parent]: Summary',1,true) and rich:find('worker -> parent: Message',1,true) and rich:find('parent pause: requested',1,true))
print('agent timeline adapter tests passed')

vim = host_vim
