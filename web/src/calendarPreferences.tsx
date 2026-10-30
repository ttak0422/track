import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from "react";
import { browserSupportsTimeZone } from "./calendarEvents";
import { deviceTimeZone } from "./calendarDates";
import timeZoneNames from "./calendarTimeZones.txt?raw";
import { STATIC_MODE } from "./runtime";

export interface CalendarPreferences { weekStartsOn: 0 | 1; timeZone: string }
export const calendarPreferencesKey = "track.calendar-display.v1";
// Generated with the event backend: browser ICU can also accept host-only placeholders.
const eventTimeZones = new Set(timeZoneNames.trim().split(/\s+/));
const defaults: CalendarPreferences = { weekStartsOn: 0, timeZone: "" };
export function calendarPreferencesError(value: CalendarPreferences): string {
  if (value.weekStartsOn !== 0 && value.weekStartsOn !== 1) return "Choose Sunday or Monday.";
  if (typeof value.timeZone !== "string" || value.timeZone.length > 128 || (value.timeZone && (!eventTimeZones.has(value.timeZone) || !browserSupportsTimeZone(value.timeZone)))) return "Enter a supported IANA time zone, or leave it empty for the device time zone.";
  return "";
}

function readPreferences(): { settings: CalendarPreferences; warning: string } {
  if (STATIC_MODE || typeof window === "undefined") return { settings: defaults, warning: "" };
  try {
    const raw = localStorage.getItem(calendarPreferencesKey);
    if (!raw) return { settings: defaults, warning: "" };
    const value = JSON.parse(raw);
    if (!value || value.version !== 1 || calendarPreferencesError(value)) throw new Error("Invalid settings");
    return { settings: { weekStartsOn: value.weekStartsOn, timeZone: value.timeZone }, warning: "" };
  } catch {
    return { settings: defaults, warning: "Saved calendar settings could not be read. Using Sunday and the device time zone." };
  }
}

const CalendarPreferencesContext = createContext<{
  settings: CalendarPreferences; zone: string; warning: string;
  save: (settings: CalendarPreferences) => string;
} | null>(null);

export function CalendarPreferencesProvider({ children }: { children: ReactNode }) {
  const [snapshot, setSnapshot] = useState(readPreferences);
  const [deviceZone, setDeviceZone] = useState(deviceTimeZone);
  useEffect(() => {
    const storage = (event: StorageEvent) => { if (!STATIC_MODE && (event.key === calendarPreferencesKey || event.key === null)) setSnapshot(readPreferences()); };
    const focus = () => setDeviceZone(deviceTimeZone());
    window.addEventListener("storage", storage);
    window.addEventListener("focus", focus);
    return () => { window.removeEventListener("storage", storage); window.removeEventListener("focus", focus); };
  }, []);
  const save = useCallback((settings: CalendarPreferences): string => {
    const error = calendarPreferencesError(settings);
    if (error) return error;
    if (STATIC_MODE) return "Published calendars are read-only.";
    try {
      if (settings.weekStartsOn === 0 && !settings.timeZone) localStorage.removeItem(calendarPreferencesKey);
      else localStorage.setItem(calendarPreferencesKey, JSON.stringify({ version: 1, ...settings }));
    } catch { return "Could not save calendar settings. Your current display is unchanged; keep the input and retry."; }
    setSnapshot({ settings, warning: "" });
    return "";
  }, []);
  return <CalendarPreferencesContext.Provider value={{ settings: snapshot.settings, zone: snapshot.settings.timeZone || deviceZone, warning: snapshot.warning, save }}>{children}</CalendarPreferencesContext.Provider>;
}

export function useCalendarPreferences() {
  return useContext(CalendarPreferencesContext) ?? { settings: defaults, zone: deviceTimeZone(), warning: "", save: () => "Calendar settings are unavailable." };
}
