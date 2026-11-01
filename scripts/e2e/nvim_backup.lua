-- Isolated frontend regression: no real Track process or user vault is accessed.
vim.opt.runtimepath:prepend(vim.fn.getcwd())
local fixture = vim.fn.tempname()
vim.fn.mkdir(fixture, 'p')
vim.env.TRACK_CONFIG = fixture .. '/absent-config.yml'
vim.env.TRACK_VAULT = fixture .. '/unused-vault'
local original_system, original_input, original_notify = vim.system, vim.ui.input, vim.notify
local calls, notices = {}, {}
local client = require('track.client')
local original_bin = client.bin
client.bin = function() return '/mock bin/track' end
vim.notify = function(message, level) notices[#notices + 1] = { message, level } end
vim.system = function(argv, opts, done)
   calls[#calls + 1] = { argv = argv, opts = opts, done = done }
   return { pid = 123 }
end
local function check(value, message) assert(value, message) end
local function contains(value)
   for _, notice in ipairs(notices) do
      if notice[1]:find(value, 1, true) then return true end
   end
   return false
end
local backup = require('track.backup')
require('track.commands').setup()
local buffer, window = vim.api.nvim_get_current_buf(), vim.api.nvim_get_current_win()
vim.api.nvim_buf_set_lines(buffer, 0, -1, false, { 'unsaved draft' })
local tick = vim.api.nvim_buf_get_changedtick(buffer)
local function unchanged()
   check(vim.api.nvim_get_current_buf() == buffer, 'buffer switched')
   check(vim.api.nvim_get_current_win() == window, 'window switched')
   check(vim.api.nvim_buf_get_changedtick(buffer) == tick and vim.bo[buffer].modified, 'draft changed/saved')
end
local function finish(result)
   calls[#calls].done(result)
   vim.wait(100, function() return false end, 10)
end
local function success(path)
   return { code = 0, signal = 0, stdout = vim.json.encode({ complete = true, path = path }), stderr = '' }
end
check(vim.tbl_contains(vim.fn.getcompletion('Track back', 'cmdline'), 'backup'), 'command undiscoverable')
check(vim.tbl_contains(vim.fn.getcompletion('Track backup ', 'cmdline'), '--out'), 'flag completion missing')
vim.ui.input = function(opts, callback)
   check(opts.completion == 'file', 'prompt lacks file completion')
   callback(nil)
end
vim.cmd('Track backup')
check(#calls == 0, 'cancel started backup')
vim.ui.input = function(_, callback) callback('') end
backup.run()
check(#calls == 0, 'empty prompt started backup')
for _, args in ipairs({
   { '--out' }, { '--out', 'wrong.zip' }, { '--unknown' },
   { '--out', 'x.tar.gz', '--overwrite', '--overwrite' },
   { '--vault', 'work', '--all-vaults' }, { '--timeout' },
}) do
   backup.run(args)
   check(#calls == 0, 'invalid arguments started backup')
end
vim.cmd([[Track backup --out /tmp/My\ vault.tar.gz --vault work --timeout 10m]])
check(#calls == 1, 'command did not launch')
check(vim.deep_equal(calls[1].argv, { '/mock bin/track', 'backup', '--out', '/tmp/My vault.tar.gz', '--vault', 'work', '--timeout', '10m' }), 'argv/spaces incorrect')
check(calls[1].opts.text and calls[1].opts.timeout == nil, 'CLI must own timeout')
check(contains('started') and not contains('complete:'), 'premature success')
backup.run({ '--out', '/tmp/second.tar.gz' })
check(#calls == 1, 'duplicate concurrent process')
unchanged()
finish(success('/tmp/My vault.tar.gz'))
check(contains('complete: /tmp/My vault.tar.gz'), 'missing completion')
unchanged()
notices = {}
backup.run({ '--out', '/tmp/literal;$HOME.tar.gz', '--all-vaults', '--overwrite' })
check(vim.deep_equal(calls[2].argv, { '/mock bin/track', 'backup', '--out', '/tmp/literal;$HOME.tar.gz', '--all-vaults', '--overwrite' }), 'literal argv/flags changed')
finish({ code = 1, signal = 0, stdout = vim.json.encode({ error = 'backup committed but directory sync failed; durability is uncertain', commit_state = 'committed', durability = 'uncertain' }), stderr = 'cleanup diagnostic' })
check(contains('durability is uncertain') and contains('commit_state') and contains('cleanup diagnostic'), 'lost failure diagnostics')
check(not contains('complete:'), 'failure claimed success')
for _, result in ipairs({
   { code = 0, signal = 0, stdout = 'not json', stderr = '' },
   { code = 0, signal = 0, stdout = '{}', stderr = '' },
   { code = 1, signal = 0, stdout = success('/tmp/a.tar.gz').stdout, stderr = '' },
   { code = 0, signal = 9, stdout = success('/tmp/a.tar.gz').stdout, stderr = '' },
}) do
   notices = {}
   backup.run({ '--out', '/tmp/a.tar.gz' })
   finish(result)
   check(contains('did not complete successfully') and not contains('complete:'), 'invalid completion accepted')
end
notices = {}
client.bin = function() error('binary missing') end
backup.run({ '--out', '/tmp/a.tar.gz' })
check(contains('binary missing'), 'missing binary not reported')
client.bin = function() return '/mock bin/track' end
vim.system = function() error('spawn refused') end
backup.run({ '--out', '/tmp/a.tar.gz' })
check(contains('spawn refused'), 'spawn failure not reported')
-- A stale prompt must not start a second process while another invocation runs.
vim.system = function(argv, opts, done)
   calls[#calls + 1] = { argv = argv, opts = opts, done = done }
   return { pid = 123 }
end
local prompted
vim.ui.input = function(_, callback) prompted = callback end
backup.run()
backup.run({ '--out', '/tmp/in-flight.tar.gz' })
local count = #calls
prompted('/tmp/stale prompt.tar.gz')
check(#calls == count, 'stale prompt started concurrent backup')
finish(success('/tmp/in-flight.tar.gz'))
-- A literal prompt path needs no Ex or shell escaping, and the default selects the inherited
-- active vault rather than deriving a vault from the modified current buffer.
vim.ui.input = function(_, callback) callback('/tmp/prompt space;$HOME.tar.gz') end
backup.run()
check(calls[#calls].argv[4] == '/tmp/prompt space;$HOME.tar.gz', 'prompt path expanded or split')
check(#calls[#calls].argv == 4 and calls[#calls].opts.env == nil, 'default vault overridden')
finish(success('/tmp/prompt space;$HOME.tar.gz'))
-- A failed spawn must release the guard. Run a real asynchronous mock executable with literal
-- arguments (no Track invocation), proving event-loop responsiveness and callback scheduling.
local script = fixture .. '/mock track'
vim.env.TRACK_BACKUP_EXPECT_VAULT = vim.env.TRACK_VAULT
vim.fn.writefile({ '#!/bin/sh', 'test "$TRACK_VAULT" = "$TRACK_BACKUP_EXPECT_VAULT" || exit 7', 'sleep 0.1', 'printf \'{"complete":true,"path":"%s"}\\n\' "$3"' }, script)
vim.fn.setfperm(script, 'rwx------')
client.bin = function() return script end
vim.system = original_system
notices = {}
local output = fixture .. '/a space.tar.gz'
local process = backup.run({ '--out', output })
check(process ~= nil and not contains('complete:'), 'real process blocked caller')
local responsive = false
vim.schedule(function() responsive = true end)
check(vim.wait(5000, function() return contains('complete: ' .. output) end, 10), 'real async mock never completed')
check(responsive, 'event loop blocked')
unchanged()
vim.fn.delete(fixture, 'rf')
client.bin, vim.system, vim.ui.input, vim.notify = original_bin, original_system, original_input, original_notify
print('track-e2e: PASS nvim backup')
vim.cmd('qa!')
