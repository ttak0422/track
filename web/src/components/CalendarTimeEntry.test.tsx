import { fireEvent, render } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import type { CalendarEvent } from "../calendarEvents";
import { CalendarTimeEntry } from "./CalendarTimeEntry";
import { timedSegments } from "./eventTimeLayout";
const row:CalendarEvent={version:1,id:"one",revision:1,title:"Review",all_day:false,start:"2026-10-29T09:00:00Z",end:"2026-10-29T10:00:00Z",time_zone:"UTC",description:"",location:"",url:"",note_id:"",status:"confirmed",deleted:false,updated_at:""};
let captured=false;
beforeEach(()=>{
  captured=false;
  Object.defineProperties(HTMLElement.prototype,{setPointerCapture:{configurable:true,value:vi.fn(()=>{captured=true;})},hasPointerCapture:{configurable:true,value:()=>captured},releasePointerCapture:{configurable:true,value:vi.fn(()=>{captured=false;})}});
});
function pointer(target:HTMLElement,type:string,x:number,y:number,extra:Record<string,unknown>={}){
  const event=new MouseEvent(type,{bubbles:true,cancelable:true,clientX:x,clientY:y,button:0});
  for(const [name,value] of Object.entries({pointerId:1,pointerType:"mouse",isPrimary:true,...extra})) Object.defineProperty(event,name,{value});
  fireEvent(target,event);
}
function mount(event=row){
 const onEdit=vi.fn(),onTimeChange=vi.fn(),onNotice=vi.fn();
 const segment=timedSegments([event],"2026-10-29","UTC")[0];
 const ui=render(<section className="calendar-time-day"><CalendarTimeEntry segment={segment} day="2026-10-29" zone="UTC" onEdit={onEdit} onTimeChange={onTimeChange} onNotice={onNotice}/></section>);
 vi.spyOn(ui.container.querySelector("section")!,"getBoundingClientRect").mockReturnValue({left:0,right:600,top:100,bottom:1564,width:600,height:1464,x:0,y:100,toJSON:()=>({})});
 return {...ui,onEdit,onTimeChange,onNotice};
}
it("previews a snapped move, proposes once on drop, and suppresses its synthetic click",()=>{
 const ui=mount(),button=ui.getByRole("button",{name:/^Edit Review/});
 pointer(button,"pointerdown",100,650);pointer(button,"pointermove",100,666);
 expect(ui.getByText("Proposed 09:15–10:15")).toBeVisible();expect(ui.onTimeChange).not.toHaveBeenCalled();
 pointer(button,"pointerup",100,666);fireEvent.click(button,{detail:1});
 expect(ui.onTimeChange).toHaveBeenCalledExactlyOnceWith(row,{...row,start:"2026-10-29T09:15:00Z",end:"2026-10-29T10:15:00Z"});expect(ui.onEdit).not.toHaveBeenCalled();
 expect(ui.queryByText(/Proposed/)).toBeNull();
 fireEvent.click(button,{detail:0});expect(ui.onEdit).toHaveBeenCalledWith(row);
});
it("resizes only the end through the handle",()=>{
 const ui=mount(),handle=ui.getByRole("button",{name:/^Change time/});
 pointer(handle,"pointerdown",590,690);pointer(handle,"pointermove",590,705);pointer(handle,"pointerup",590,705);
 expect(ui.onTimeChange).toHaveBeenCalledExactlyOnceWith(row,{...row,end:"2026-10-29T10:15:00Z"});
});
it("offers the same move/resize proposals as keyboard-activated buttons and restores focus",()=>{
 const ui=mount(),toggle=ui.getByRole("button",{name:/^Change time/});fireEvent.click(toggle,{detail:0});
 const action=ui.getByRole("button",{name:"Move later 15 minutes"});fireEvent.click(action,{detail:0});
 expect(ui.onTimeChange).toHaveBeenCalledExactlyOnceWith(row,{...row,start:"2026-10-29T09:15:00Z",end:"2026-10-29T10:15:00Z"});expect(toggle).toHaveFocus();
 fireEvent.click(toggle,{detail:0});fireEvent.keyDown(ui.getByRole("group",{name:"Time actions for Review"}),{key:"Escape"});expect(ui.queryByRole("group")).toBeNull();expect(toggle).toHaveFocus();
});
it.each(["pointercancel","lostpointercapture","escape","blur"])("cancels %s without opening a proposal",(kind)=>{
 const ui=mount(),button=ui.getByRole("button",{name:/^Edit Review/});pointer(button,"pointerdown",100,650);pointer(button,"pointermove",100,665);
 if(kind==="escape")fireEvent.keyDown(button,{key:"Escape"});else if(kind==="blur")fireEvent.blur(window);else pointer(button,kind,100,665);
 pointer(button,"pointerup",100,665);fireEvent.click(button,{detail:1});expect(ui.onTimeChange).not.toHaveBeenCalled();expect(ui.onEdit).not.toHaveBeenCalled();expect(ui.queryByText(/Proposed/)).toBeNull();
});
it("rejects another day column and leaves touch scrolling to the browser",()=>{
 const ui=mount(),button=ui.getByRole("button",{name:/^Edit Review/});pointer(button,"pointerdown",100,650);pointer(button,"pointermove",700,665);pointer(button,"pointerup",700,665);
 expect(ui.onTimeChange).not.toHaveBeenCalled();expect(ui.onNotice).toHaveBeenLastCalledWith(expect.stringContaining("No time change applied"));
 pointer(button,"pointerdown",100,650,{pointerType:"touch"});pointer(button,"pointermove",100,665,{pointerType:"touch"});pointer(button,"pointerup",100,665,{pointerType:"touch"});expect(ui.onTimeChange).not.toHaveBeenCalled();
});
it("keeps cancelled events in their ordinary editor without time handles",()=>{
 const ui=mount({...row,status:"cancelled"});expect(ui.queryByRole("button",{name:/^Change time/})).toBeNull();fireEvent.click(ui.getByRole("button",{name:/^Edit Review/}));expect(ui.onEdit).toHaveBeenCalled();
});

it("keeps the action list open across focus scrolling and closes on a second pointer click",()=>{
 const ui=mount(),toggle=ui.getByRole("button",{name:/^Change time/});fireEvent.click(toggle,{detail:0});
 fireEvent.scroll(window);expect(ui.getByRole("group",{name:"Time actions for Review"})).toBeVisible();
 pointer(toggle,"pointerdown",590,690);pointer(toggle,"pointerup",590,690);fireEvent.click(toggle,{detail:1});
 expect(ui.queryByRole("group")).toBeNull();expect(ui.onTimeChange).not.toHaveBeenCalled();
});
