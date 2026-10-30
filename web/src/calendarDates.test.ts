import { expect, it } from "vitest";
import { calendarClock, calendarDateKey, calendarDayRange, isCalendarDay } from "./calendarDates";
import { calendarEventOrder, eventOnDay, type CalendarEvent } from "./calendarEvents";
const event = (start: string, end: string): CalendarEvent => ({ version:1,id:"one",revision:1,title:"One",all_day:false,start,end,time_zone:"UTC",description:"",location:"",url:"",note_id:"",status:"confirmed",deleted:false,updated_at:"" });

it("finds display-zone boundaries independent of the device, including folds and gaps", () => {
  expect(calendarDayRange("2026-03-08","America/New_York")).toEqual({start:Date.parse("2026-03-08T00:00:00-05:00"),end:Date.parse("2026-03-09T00:00:00-04:00"),minutes:1380});
  expect(calendarDayRange("2026-11-01","America/New_York").minutes).toBe(1500);
  expect(calendarDayRange("2026-09-06","America/Santiago")).toEqual({start:Date.parse("2026-09-06T01:00:00-03:00"),end:Date.parse("2026-09-07T00:00:00-03:00"),minutes:1380});
  expect(calendarDayRange("2026-04-05","Australia/Lord_Howe").minutes).toBe(1470);
  const skipped=calendarDayRange("2011-12-30","Pacific/Apia"); expect(skipped.minutes).toBe(0); expect(skipped.start).toBe(skipped.end);
  expect(calendarClock(Date.parse("2026-11-01T01:30:00-04:00"),"America/New_York",true)).toBe("01:30 UTC−04:00");
  expect(calendarClock(Date.parse("2026-11-01T01:30:00-05:00"),"America/New_York",true)).toBe("01:30 UTC−05:00");
});

it("uses the display zone for today and timed membership, without moving civil all-day dates", () => {
  const instant=Date.parse("2027-01-01T00:30:00Z");
  expect(calendarDateKey(instant,"America/Los_Angeles")).toBe("2026-12-31");
  expect(calendarDateKey(instant,"Asia/Tokyo")).toBe("2027-01-01");
  const row=event("2027-01-01T00:00:00Z","2027-01-01T01:00:00Z");
  expect(eventOnDay(row,"2026-12-31","America/Los_Angeles")).toBe(true);
  expect(eventOnDay(row,"2026-12-31","Asia/Tokyo")).toBe(false);
  const allDay={...row,id:"all-day",all_day:true,start:"2027-01-01",end:"2027-01-02"};
  expect([allDay,row].sort((a,b)=>calendarEventOrder(a,b,"America/Los_Angeles"))[0].id).toBe("one");
  expect(eventOnDay({...row,all_day:true,start:"2027-01-01",end:"2027-01-02"},"2027-01-01","America/Los_Angeles")).toBe(true);
  const midnight=event("2026-09-07T00:15:00-03:00","2026-09-07T00:45:00-03:00");
  expect(eventOnDay(midnight,"2026-09-06","America/Santiago")).toBe(false);
  expect(eventOnDay(event("2011-12-29T12:00:00-10:00","2011-12-31T12:00:00+14:00"),"2011-12-30","Pacific/Apia")).toBe(false);
});

it("rejects impossible civil dates rather than normalizing them into another day", () => {
  expect(isCalendarDay("2024-02-29")).toBe(true);
  for (const day of ["2026-02-29","2026-13-01","2026-01-32","2026-1-1"]) {
    expect(isCalendarDay(day)).toBe(false); expect(calendarDayRange(day,"UTC").minutes).toBe(0);
  }
});
