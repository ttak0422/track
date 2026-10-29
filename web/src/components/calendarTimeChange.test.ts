import { expect, it } from "vitest";
import type { CalendarEvent } from "../calendarEvents";
import { canChangeTime, proposeTimeChange } from "./calendarTimeChange";
export const fixture: CalendarEvent = {version:1,id:"one",revision:7,title:"Review",all_day:false,start:"2026-10-29T09:00:00Z",end:"2026-10-29T10:00:00Z",time_zone:"UTC",description:"Keep",location:"Desk",url:"https://example.com/",note_id:"",status:"confirmed",deleted:false,updated_at:""};

it("moves both ends or resizes only the end, preserving identity and all other fields", () => {
  const original=structuredClone(fixture);
  expect(proposeTimeChange(fixture,"2026-10-29","UTC","move",15)).toEqual({...fixture,start:"2026-10-29T09:15:00Z",end:"2026-10-29T10:15:00Z"});
  expect(proposeTimeChange(fixture,"2026-10-29","UTC","resize-end",-15)).toEqual({...fixture,end:"2026-10-29T09:45:00Z"});
  expect(fixture).toEqual(original);
});
it("rejects other dates, civil events, terminal status and display-zone transition days", () => {
  for(const extra of [{all_day:true},{deleted:true},{status:"cancelled" as const},{start:"2026-10-28T23:00:00Z"},{end:"2026-10-30T01:00:00Z"},{time_zone:"Not/A_Zone"}]) expect(canChangeTime({...fixture,...extra},"2026-10-29","UTC")).toBe(false);
  const fall={...fixture,start:"2026-11-01T01:15:00-04:00",end:"2026-11-01T01:45:00-04:00",time_zone:"America/New_York"};
  expect(canChangeTime(fall,"2026-11-01","America/New_York")).toBe(false);
  expect(canChangeTime({...fixture,start:"2026-09-06T01:15:00-03:00",end:"2026-09-06T02:00:00-03:00",time_zone:"America/Santiago"},"2026-09-06","America/Santiago")).toBe(false);
});
it("keeps the proposal inside the day and enforces a 15-minute minimum when resizing", () => {
  expect(proposeTimeChange(fixture,"2026-10-29","UTC","move",-555)).toBeNull();
  expect(proposeTimeChange(fixture,"2026-10-29","UTC","resize-end",-60)).toBeNull();
  expect(proposeTimeChange(fixture,"2026-10-29","UTC","resize-end",-45)?.end).toBe("2026-10-29T09:15:00Z");
  expect(proposeTimeChange(fixture,"2026-10-29","UTC","resize-end",855)).toBeNull();
  for(const delta of [0,1,NaN,Infinity,15.5]) expect(proposeTimeChange(fixture,"2026-10-29","UTC","move",delta)).toBeNull();
});
it("preserves authored-zone offsets and nanosecond fractions across an authored-zone fold", () => {
  const row={...fixture,start:"2026-11-01T01:15:00.123456789-04:00",end:"2026-11-01T01:45:00.987654321-04:00",time_zone:"US/Eastern"};
  const result=proposeTimeChange(row,"2026-11-01","UTC","move",60)!;
  expect(result.start).toBe("2026-11-01T01:15:00.123456789-05:00");
  expect(result.end).toBe("2026-11-01T01:45:00.987654321-05:00");
  expect(Date.parse(result.end)-Date.parse(result.start)).toBe(Date.parse(row.end)-Date.parse(row.start));
});
it("converts instants back to a different authored zone without rewriting that zone", () => {
  const row={...fixture,start:"2026-10-29T09:00:45.123+09:00",end:"2026-10-29T10:00:45.456+09:00",time_zone:"Asia/Tokyo"};
  expect(proposeTimeChange(row,"2026-10-29","UTC","move",15)).toEqual({...row,start:"2026-10-29T09:15:45.123+09:00",end:"2026-10-29T10:15:45.456+09:00"});
});
