-- Real child process I/O; no Track vault/config/database is consulted.
vim.opt.rtp:prepend(vim.fn.getcwd())
local root = assert(vim.env.TRACK_TEST_TMP)
vim.fn.mkdir(root, "p")
local helper = root .. "/fake-track"
vim.fn.writefile({
   "#!/bin/sh",
   'case "$1" in',
   'warning) printf "%s\\n" "synthetic warning" >&2; printf "%s\\n" \'{"results":[1],"read_state":{"partial":true}}\';;',
   'error) printf "%s\\n" \'{"error":"synthetic application error"}\'; exit 1;;',
   'bad) printf "%s\\n" "{";;',
   'empty) exit 1;;',
   'failed) printf "%s\\n" \'{"results":[1]}\'; exit 7;;',
   'input) cat;;',
   'esac',
},helper)
assert(vim.uv.fs_chmod(helper,493))
package.preload["track.config"] = function() return { options = { bin = helper } } end
local client = require("track.client")
local notifications=0
vim.notify=function() notifications=notifications+1 end
local result,err=client.run_json({"warning"})
assert(result and result.results[1]==1 and result.read_state.partial and not err)
assert(client.run({"input"},"synthetic stdin\n")=="synthetic stdin\n")
local _,message=client.run_json({"error"});assert(message=="synthetic application error")
_,message=client.run_json({"bad"});assert(message:find("invalid JSON output",1,true))
for _,mode in ipairs({"empty","failed"}) do
   _,message=client.run_json({mode});assert(message:find("CLI failed",1,true))
end
assert(notifications==0,"successful diagnostics caused an intrusive notification")
print("JSON transport: warning, error, invalid output, exit status, stdin and partial payload passed")
vim.cmd("qa!")
