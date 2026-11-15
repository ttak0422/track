local host_vim = vim
package.path = './lua/?.lua;' .. package.path
local buffers, options, mappings, writes, count, receipt = {}, {}, {}, {}, 0, nil
local snapshot = {title='Demo',revision=1,progress={fraction=0,remaining_minutes=0},tasks={},reports={},discussions={},controls={},events={},mode='serial'}
local request = {source='nvim',event_id='e1',expected_revision=1,kind='task.upsert',payload={}}
local model = {snapshot=snapshot,project_id="demo"}
function model.request() return request end
function model.refresh(cb) cb(nil,snapshot) end
function model.cancel() end
function model.apply(_,cb) receipt=cb end
package.loaded['track.agent_timeline']={new=function() return model end}
vim = {
  cmd=function() end,
  notify=function() end,
  log={levels={ERROR=3}},
  json={encode=function() return 'request' end,decode=function() return request end},
  bo=setmetatable({}, {__index=function(_,key) options[key]=options[key] or {};return options[key] end}),
  keymap={set=function(_,key,cb,opt) mappings[opt.buffer]=mappings[opt.buffer] or {};mappings[opt.buffer][key]=cb end},
  ui={select=function(items,_,cb) cb(items[1]) end},
  api={
    nvim_create_buf=function() count=count+1;buffers[count]={lines={},tick=0,valid=true};return count end,
    nvim_win_set_buf=function() end,
    nvim_buf_set_name=function() end,
    nvim_buf_set_lines=function(buf,_,_,_,value) buffers[buf].lines=value;buffers[buf].tick=buffers[buf].tick+1 end,
    nvim_buf_get_lines=function(buf) return buffers[buf].lines end,
    nvim_buf_get_changedtick=function(buf) return buffers[buf].tick end,
    nvim_buf_is_valid=function(buf) return buffers[buf] and buffers[buf].valid end,
    nvim_buf_delete=function(buf) buffers[buf].valid=false end,
    nvim_create_autocmd=function(name,opt) if name=='BufWriteCmd' then writes[opt.buffer]=opt.callback end end,
  },
}
dofile('lua/track/timeline_view.lua').open('demo',{})
mappings[1].a()
writes[2]()
assert(options[2].modifiable==false)
-- Even an external plugin changing a locked buffer must not lose those new edits.
buffers[2].tick=buffers[2].tick+1;options[2].modified=true
receipt(nil)
assert(buffers[2].valid and options[2].modified and options[2].modifiable)
mappings[1].a()
writes[3]()
receipt(nil)
assert(not buffers[3].valid)
-- Closing the mutation view while a write is pending must not reopen anything.
mappings[1].a()
writes[4]()
buffers[4].valid=false
receipt(nil)
assert(count==4)
print('timeline view ownership tests passed')
vim=host_vim
