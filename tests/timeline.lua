local host_vim = vim
local host_client = package.loaded["track.client"]
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
local function finish(job, code, data, stderr, signal)
   job.callback({code = code, signal = signal or 0, stdout = data, stderr = stderr == nil and 'failed' or stderr})
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
-- Track writes structured CLI failures to stdout, not stderr.
payloads.failure = {error = 'timeline note not found'}
timeline.refresh(callback)
finish(jobs[#jobs], 1, 'failure', 'less specific stderr')
assert(timeline.error == 'Timeline read failed: timeline note not found')
assert(timeline.snapshot == payloads.good and not timeline.loading)
timeline.refresh(callback)
finish(jobs[#jobs], 1, 'not JSON', 'executable failed')
assert(timeline.error == 'Timeline read failed: executable failed')
payloads.bad_error = {error = {message = 'not a string'}}
timeline.refresh(callback)
finish(jobs[#jobs], 2, 'bad_error', '')
assert(timeline.error == 'Timeline read failed: exit code 2')
payloads.empty_error = {error = '   '}
timeline.refresh(callback)
finish(jobs[#jobs], 0, 'empty_error', '  ', 15)
assert(timeline.error == 'Timeline read failed: exit code 0, signal 15')
payloads.long_error = {error = string.rep('x', 1024)}
timeline.refresh(callback)
finish(jobs[#jobs], 1, 'long_error', '')
assert(timeline.error == 'Timeline read failed: ' .. string.rep('x', 512))
assert(timeline.snapshot == payloads.good)
print('timeline adapter tests passed')
vim = host_vim
package.loaded['track.client'] = host_client
