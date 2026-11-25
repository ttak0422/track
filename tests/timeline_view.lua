local host_vim = vim
package.path = './lua/?.lua;' .. package.path
local buffers, options, mappings, writes, count, receipt = {}, {}, {}, {}, 0, nil
local snapshot = {title='Demo',revision=1,progress={fraction=0,remaining_minutes=0},tasks={},reports={},discussions={},controls={},events={},mode='serial'}
local request = {source='nvim',event_id='e1',expected_revision=1,kind='task.upsert',payload={}}
local model = {snapshot=snapshot,project_id="demo"}
local encoded, picker, disposed, watched
local function copy(value)
  if type(value) ~= 'table' then return value end
  local result = {}; for k,v in pairs(value) do result[k]=copy(v) end; return result
end
function model.request(kind,payload) request={source="nvim",event_id="e1",expected_revision=model.snapshot.revision,kind=kind,payload=payload};return request end
function model.refresh(cb) cb(nil,snapshot) end
function model.cancel() end
function model.watch(interval,cb) watched=interval end
function model.dispose() disposed=true end
function model.apply(_,cb) receipt=cb end
package.loaded['track.agent_timeline']={new=function() return model end}
vim = {
  deepcopy=copy,
  cmd=function() end,
  notify=function() end,
  log={levels={ERROR=3}},
  json={encode=function(value) if value.kind then encoded=copy(value) end;return value.marker or 'request' end,decode=function() return request end},
  bo=setmetatable({}, {__index=function(_,key) options[key]=options[key] or {};return options[key] end}),
  keymap={set=function(_,key,cb,opt) mappings[opt.buffer]=mappings[opt.buffer] or {};mappings[opt.buffer][key]=cb end},
  ui={select=function(items,opt,cb) if picker then picker(items,opt,cb) else cb(items[1]) end end},
  api={
    nvim_create_buf=function() count=count+1;buffers[count]={lines={},tick=0,valid=true};return count end,
    nvim_win_set_buf=function() end,
    nvim_buf_set_name=function() end,
    nvim_buf_set_lines=function(buf,_,_,_,value) buffers[buf].lines=value;buffers[buf].tick=buffers[buf].tick+1 end,
    nvim_buf_get_lines=function(buf) return buffers[buf].lines end,
    nvim_buf_get_changedtick=function(buf) return buffers[buf].tick end,
    nvim_buf_is_valid=function(buf) return buffers[buf] and buffers[buf].valid end,
    nvim_buf_delete=function(buf) buffers[buf].valid=false;if buffers[buf].wipe then buffers[buf].wipe() end end,
    nvim_create_autocmd=function(name,opt) if name=='BufWriteCmd' then writes[opt.buffer]=opt.callback elseif name=='BufWipeout' then buffers[opt.buffer].wipe=opt.callback end end,
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
-- Existing tasks retain every field, including fields not edited in this UI.
local task = {id="child",parent_id="parent",title="Child",detail="Details",status="blocked",owner="worker",
  priority=0,weight=2,done_minutes=4,remaining_minutes=9,mode="parallel"}
local parent = {id="parent",title="Parent",status="active",weight=1,done_minutes=0,remaining_minutes=0,mode="serial"}
snapshot.tasks={task,parent} -- Deliberately out of tree order.
snapshot.task_progress={child={fraction=0.25}}
snapshot.reports={{id="report",author="worker",task_id="child",summary="Summary",period_start="start",period_end="end",event_sequences={1}}}
snapshot.events={{sequence=1,at="now",source="worker",event_id="created",kind="task.upsert",payload={marker="event payload visible"}}}
mappings[1].r()
local rendered=table.concat(buffers[1].lines,"\n")
assert(rendered:find("States are recorded, not process liveness",1,true))
assert(rendered:find("a > task.upsert to edit the full task",1,true))
assert(rendered:find("[active] Parent",1,true)<rendered:find("[blocked] Child",1,true))
assert(rendered:find("\n  [blocked] Child (child) | 25%",1,true))
assert(rendered:find("priority=P0",1,true) and rendered:find("task:child",1,true))
assert(rendered:find("start - end",1,true), "report period must remain visible")
for _, hidden in ipairs({ "Recorded events", "Payload:", "event payload visible", " events: 1" }) do
  assert(not rendered:find(hidden,1,true), "raw journal detail must stay hidden: " .. hidden)
end
local original=copy(task)
picker=function(items,opt,cb)
  if opt.prompt=='Append an explicit record' then cb('task.upsert'); return end
  assert(opt.prompt=='Create or edit task' and #items==3)
  -- Another refresh can arrive while vim.ui.select is open.
  snapshot.revision=2;task.title='Changed externally';task.remaining_minutes=99
  cb(items[2])
end
mappings[1].a()
assert(encoded.expected_revision==1, 'picker silently rebased the edit')
for key,value in pairs(original) do assert(encoded.payload[key]==value, 'lost task field: '..key) end
assert(encoded.payload~=task)
assert(watched==5000, 'timeline monitoring not enabled')
mappings[1].q()
assert(disposed, 'closing timeline did not dispose monitoring')
print('timeline view ownership tests passed')
vim=host_vim
