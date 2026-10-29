import { fireEvent, render, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { CalendarPreferencesProvider, calendarPreferencesKey, calendarPreferencesError, useCalendarPreferences } from "./calendarPreferences";
import { CalendarDisplaySettings } from "./components/CalendarDisplaySettings";
import timeZoneNames from "./calendarTimeZones.txt?raw";
// @ts-expect-error node builtin — only used by this repository test
import { readFileSync } from "node:fs";
const mode=vi.hoisted(()=>({static:false}));
vi.mock("./runtime",()=>({get STATIC_MODE(){return mode.static;}}));
function Current(){const {settings,zone}=useCalendarPreferences();return <output aria-label="Current display">{settings.weekStartsOn}:{settings.timeZone}:{zone}</output>;}
function Fixture(){return <CalendarPreferencesProvider><CalendarDisplaySettings/><Current/></CalendarPreferencesProvider>;}
beforeEach(()=>{localStorage.clear();mode.static=false;});
afterEach(()=>{vi.restoreAllMocks();localStorage.clear();});

it("writes only on Apply and restores settings on a fresh mount", () => {
  const ui=render(<Fixture/>); if(!mode.static) fireEvent.click(ui.getByText(/^Calendar display ·/));expect(localStorage.getItem(calendarPreferencesKey)).toBeNull();
  fireEvent.change(ui.getByLabelText("Week starts on"),{target:{value:"1"}});
  fireEvent.change(ui.getByLabelText("Display time zone"),{target:{value:" America/New_York "}});
  expect(localStorage.getItem(calendarPreferencesKey)).toBeNull();
  fireEvent.submit(ui.getByRole("button",{name:"Apply display"}).closest("form")!);
  expect(JSON.parse(localStorage.getItem(calendarPreferencesKey)!)).toEqual({version:1,weekStartsOn:1,timeZone:"America/New_York"});
  ui.unmount();const reopened=render(<Fixture/>);fireEvent.click(reopened.getByText(/^Calendar display ·/));expect(reopened.getByLabelText("Current display")).toHaveTextContent("1:America/New_York:America/New_York");
  fireEvent.click(reopened.getByRole("button",{name:"Use defaults"}));expect(localStorage.getItem(calendarPreferencesKey)).toBeNull();expect(reopened.getByLabelText("Display time zone")).toHaveValue("");
});

it("retains invalid or unsaved inputs without changing the active display", () => {
  const ui=render(<Fixture/>); if(!mode.static) fireEvent.click(ui.getByText(/^Calendar display ·/));const original=ui.getByLabelText("Current display").textContent;
  fireEvent.change(ui.getByLabelText("Display time zone"),{target:{value:"Factory"}});
  fireEvent.submit(ui.getByRole("button",{name:"Apply display"}).closest("form")!);
  expect(ui.getByRole("alert")).toHaveTextContent("supported IANA");expect(ui.getByLabelText("Display time zone")).toHaveValue("Factory");expect(ui.getByLabelText("Current display").textContent).toBe(original);
  fireEvent.change(ui.getByLabelText("Display time zone"),{target:{value:"US/Eastern"}});
  vi.spyOn(Storage.prototype,"setItem").mockImplementation(()=>{throw new DOMException("full","QuotaExceededError");});
  fireEvent.submit(ui.getByRole("button",{name:"Apply display"}).closest("form")!);
  expect(ui.getByRole("alert")).toHaveTextContent("Could not save");expect(ui.getByLabelText("Display time zone")).toHaveValue("US/Eastern");expect(ui.getByLabelText("Current display").textContent).toBe(original);
});

it("falls back safely from corrupt or unsupported stored settings without overwriting them", () => {
  localStorage.setItem(calendarPreferencesKey,JSON.stringify({version:1,weekStartsOn:1,timeZone:"Factory"}));
  const ui=render(<Fixture/>); if(!mode.static) fireEvent.click(ui.getByText(/^Calendar display ·/));expect(ui.getByRole("alert")).toHaveTextContent("could not be read");expect(ui.getByLabelText("Display time zone")).toHaveValue("");expect(JSON.parse(localStorage.getItem(calendarPreferencesKey)!).timeZone).toBe("Factory");
});

it("applies same-origin storage changes while ignoring unrelated keys", async () => {
  const ui=render(<Fixture/>); if(!mode.static) fireEvent.click(ui.getByText(/^Calendar display ·/));
  localStorage.setItem(calendarPreferencesKey,JSON.stringify({version:1,weekStartsOn:1,timeZone:"UTC"}));
  fireEvent(window,new StorageEvent("storage",{key:"unrelated"}));expect(ui.getByLabelText("Display time zone")).toHaveValue("");
  fireEvent(window,new StorageEvent("storage",{key:calendarPreferencesKey}));await waitFor(()=>expect(ui.getByLabelText("Current display")).toHaveTextContent("1:UTC:UTC"));
});

it("keeps published output at its defaults without reading or writing local preferences", () => {
  mode.static=true;const read=vi.spyOn(Storage.prototype,"getItem");const ui=render(<Fixture/>); if(!mode.static) fireEvent.click(ui.getByText(/^Calendar display ·/));expect(read).not.toHaveBeenCalled();expect(ui.queryByLabelText("Display time zone")).toBeNull();
});

it("accepts named IANA aliases and refuses fixed-offset strings as event defaults", () => {
  expect(calendarPreferencesError({weekStartsOn:1,timeZone:"US/Eastern"})).toBe("");
  expect(calendarPreferencesError({weekStartsOn:0,timeZone:"+09:00"})).toContain("IANA");
});

it("keeps display names in sync with the event backend and refuses placeholders even if ICU accepts them", () => {
  expect(timeZoneNames).toBe(readFileSync("../internal/track/calendarevent/zones.txt","utf8"));
  const original=Intl.DateTimeFormat;
  vi.spyOn(Intl,"DateTimeFormat").mockImplementation(function(...args: Parameters<typeof Intl.DateTimeFormat>){return new original(args[0],{...args[1],timeZone:"UTC"});});
  for(const zone of ["Factory","localtime","posixrules","SystemV/EST5","+09:00"]){expect(calendarPreferencesError({weekStartsOn:0,timeZone:zone})).toContain("IANA");}
});
