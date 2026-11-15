local host_vim = vim
package.path = './lua/?.lua;' .. package.path
local jobs, scheduled, payloads = {}, {}, {}
vim = {
   schedule = function(cb) scheduled[#scheduled + 1] = cb end,
   json = { decode = function(s) if not payloads[s] then error('bad JSON') end return payloads[s] end },
   system = function(argv, options, cb)
      local job = {argv = argv, options = options, callback = cb}
      function job:kill() self.killed = true end
      jobs[#jobs + 1] = job
      return job
   end,
}
package.loaded['track.client'] = {bin = function() return '/bin/track' end}
local timeline = dofile('lua/track/timeline.lua').new('/vault/note/123.md')
local function finish(job, code, data)
   job.callback({code = code, signal = 0, stdout = data, stderr = 'failed'})
   while #scheduled > 0 do table.remove(scheduled, 1)() end
end
local called = 0
local function callback() called = called + 1 end
payloads.good = {schema_version = 1, items = {{depth = 1, state = 'TODO', text = 'example'}}, history = {}}
timeline.refresh(callback)
assert(jobs[1].argv[2] == 'timeline' and jobs[1].argv[4] == '/vault/note/123.md')
assert(jobs[1].options.timeout == 10000)
timeline.refresh(callback)
assert(jobs[1].killed)
finish(jobs[1], 0, 'good')
assert(called == 0 and timeline.snapshot == nil)
finish(jobs[2], 0, 'good')
assert(called == 1 and timeline.rows()[1] == '  [TODO] example')
local count = #jobs
timeline.rows()
assert(#jobs == count)
timeline.refresh(callback)
finish(jobs[3], 1, '')
assert(timeline.error and timeline.snapshot and not timeline.loading)
timeline.refresh(callback)
finish(jobs[4], 0, 'invalid')
assert(timeline.error == 'Invalid timeline response')
timeline.refresh(callback)
timeline.cancel()
finish(jobs[5], 0, 'good')
assert(called == 3)
print('timeline adapter tests passed')

vim = host_vim
