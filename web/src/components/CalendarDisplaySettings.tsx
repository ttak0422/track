import { useEffect, useState } from "react";
import { useCalendarPreferences, type CalendarPreferences } from "../calendarPreferences";
import { STATIC_MODE } from "../runtime";

export function CalendarDisplaySettings() {
  const { settings, zone, warning, save } = useCalendarPreferences();
  const [weekStartsOn, setWeekStartsOn] = useState(settings.weekStartsOn);
  const [timeZone, setTimeZone] = useState(settings.timeZone);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  useEffect(() => { setWeekStartsOn(settings.weekStartsOn); setTimeZone(settings.timeZone); }, [settings.weekStartsOn, settings.timeZone]);
  if (STATIC_MODE) return null;
  function apply(value: CalendarPreferences) {
    const error = save(value);
    setError(error);
    setNotice(error ? "" : "Calendar display saved for this browser.");
  }
  return <details className="calendar-display-settings">
    <summary>Calendar display · {zone} · {settings.weekStartsOn === 1 ? "Monday" : "Sunday"} first</summary>
    <form onSubmit={(event) => { event.preventDefault(); apply({ weekStartsOn, timeZone: timeZone.trim() }); }}>
      <label>Week starts on<select value={weekStartsOn} onChange={(event) => setWeekStartsOn(Number(event.target.value) as 0 | 1)}><option value={0}>Sunday</option><option value={1}>Monday</option></select></label>
      <label>Display time zone<input value={timeZone} placeholder="Device time zone" maxLength={128} onChange={(event) => setTimeZone(event.target.value)} /></label>
      <div className="calendar-nav"><button type="submit">Apply display</button><button type="button" onClick={() => apply({ weekStartsOn: 0, timeZone: "" })}>Use defaults</button></div>
    </form>
    <p className="muted">Blank uses the device time zone. Saved for this browser and server address across vaults; event dates and authored time zones stay unchanged.</p>
    {error || warning ? <p className="error" role="alert">{error || warning}</p> : null}
    {notice ? <p role="status">{notice}</p> : null}
  </details>;
}
