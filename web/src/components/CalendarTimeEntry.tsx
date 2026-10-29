import { useEffect, useId, useRef, useState, type PointerEvent as ReactPointerEvent } from "react";
import { createPortal } from "react-dom";
import { calendarClock, calendarDayRange } from "../calendarDates";
import { eventTimeLabel, type CalendarEvent } from "../calendarEvents";
import { canChangeTime, proposeTimeChange, type TimeChangeMode } from "./calendarTimeChange";
import { gridClock, type TimedSegment } from "./eventTimeLayout";

interface Props {
  segment: TimedSegment; day: string; zone: string;
  onEdit: (event: CalendarEvent) => void;
  onTimeChange?: (original: CalendarEvent, proposal: CalendarEvent) => void;
  onNotice: (text: string) => void;
}
interface Gesture { mode: TimeChangeMode; pointer: number; x: number; y: number; moved: boolean; target: HTMLButtonElement; dayElement: HTMLElement }

export function CalendarTimeEntry({ segment, day, zone, onEdit, onTimeChange, onNotice }: Props) {
  const { event, start, end, column, columns } = segment;
  const range = calendarDayRange(day, zone);
  const transition = range.minutes !== 1440;
  const enabled = Boolean(onTimeChange) && canChangeTime(event, day, zone);
  const gesture = useRef<Gesture | null>(null);
  const suppressClick = useRef(false);
  const toggle = useRef<HTMLButtonElement>(null);
  const popup = useRef<HTMLDivElement>(null);
  const menuId = useId();
  const [menu, setMenu] = useState<{ left: number; top: number } | null>(null);
  const [preview, setPreview] = useState<{ mode: TimeChangeMode; minutes: number } | null>(null);
  const proposal = preview ? proposeTimeChange(event, day, zone, preview.mode, preview.minutes) : null;

  function clearGesture(cancelClick = false) {
    const previous = gesture.current;
    if (cancelClick && previous) suppressClick.current = true;
    gesture.current = null;
    if (previous?.target.hasPointerCapture?.(previous.pointer)) previous.target.releasePointerCapture(previous.pointer);
  }
  function cancelGesture() { if (gesture.current) onNotice("Time change cancelled."); clearGesture(true); setPreview(null); }
  useEffect(() => {
    clearGesture(true); setPreview(null); setMenu(null);
    window.addEventListener("blur", cancelGesture);
    return () => { clearGesture(true); window.removeEventListener("blur", cancelGesture); };
  }, [event.id, event.revision, day, zone]);
  const menuOpen = menu !== null;
  useEffect(() => {
    if (!menuOpen) return;
    popup.current?.querySelector<HTMLButtonElement>("button:not(:disabled)")?.focus({ preventScroll: true });
    const outside = (event: PointerEvent) => { if (!popup.current?.contains(event.target as Node) && !toggle.current?.contains(event.target as Node)) setMenu(null); };
    const reposition = () => { const next = menuPosition(); setMenu((current) => current && (current.left !== next.left || current.top !== next.top) ? next : current); };
    document.addEventListener("pointerdown", outside);
    window.addEventListener("scroll", reposition, true); window.addEventListener("resize", reposition);
    return () => { document.removeEventListener("pointerdown", outside); window.removeEventListener("scroll", reposition, true); window.removeEventListener("resize", reposition); };
  }, [menuOpen]);

  function begin(eventInput: ReactPointerEvent<HTMLButtonElement>, mode: TimeChangeMode) {
    suppressClick.current = false;
    if (!enabled || !eventInput.isPrimary || eventInput.button !== 0 || eventInput.pointerType === "touch") return;
    const dayElement = eventInput.currentTarget.closest<HTMLElement>(".calendar-time-day");
    if (!dayElement) return;
    eventInput.currentTarget.focus({ preventScroll: true });
    try { eventInput.currentTarget.setPointerCapture(eventInput.pointerId); }
    catch { onNotice("Drag is unavailable. Use Change time or the event editor."); return; }
    gesture.current = { mode, pointer: eventInput.pointerId, x: eventInput.clientX, y: eventInput.clientY - dayElement.getBoundingClientRect().top, moved: false, target: eventInput.currentTarget, dayElement };
    onNotice("");
  }
  function position(input: ReactPointerEvent<HTMLButtonElement>, active: Gesture) {
    const bounds = active.dayElement.getBoundingClientRect();
    const delta = input.clientY - bounds.top - active.y;
    active.moved ||= Math.abs(delta) >= 8 || Math.abs(input.clientX - active.x) >= 8;
    return { minutes: Math.round(delta / 15) * 15, withinDay: input.clientX >= bounds.left && input.clientX <= bounds.right };
  }
  function move(input: ReactPointerEvent<HTMLButtonElement>) {
    const active = gesture.current;
    if (!active || active.pointer !== input.pointerId) return;
    const value = position(input, active);
    if (!active.moved) return;
    input.preventDefault();
    setMenu(null);
    setPreview(value.withinDay ? { mode: active.mode, minutes: value.minutes } : null);
  }
  function finish(input: ReactPointerEvent<HTMLButtonElement>) {
    const active = gesture.current;
    if (!active || active.pointer !== input.pointerId) return;
    const value = position(input, active);
    clearGesture(); setPreview(null);
    if (!active.moved) return;
    suppressClick.current = true;
    const next = value.withinDay ? proposeTimeChange(event, day, zone, active.mode, value.minutes) : null;
    if (next) onTimeChange?.(event, next);
    else onNotice("No time change applied. Keep the proposal within this day; use the editor for other dates or intervals.");
  }
  function clicked(detail: number): boolean {
    const skip = detail !== 0 && suppressClick.current;
    suppressClick.current = false;
    return !skip;
  }
  function menuPosition() {
    const bounds = toggle.current!.getBoundingClientRect();
    return { left: Math.max(8, Math.min(window.innerWidth - 230, bounds.right - 222)), top: Math.max(8, Math.min(window.innerHeight - 190, bounds.bottom + 4)) };
  }
  function openMenu() { setMenu((current) => current ? null : menuPosition()); }
  const pointerHandlers = { onPointerMove: move, onPointerUp: finish, onPointerCancel: cancelGesture, onLostPointerCapture: () => { if (gesture.current) cancelGesture(); }, onKeyDown: (input: React.KeyboardEvent<HTMLButtonElement>) => { if (input.key === "Escape" && gesture.current) { input.preventDefault(); cancelGesture(); } } };

  return <div className="calendar-time-entry" style={{ top: `${start}px`, height: `${Math.max(24, end - start)}px`, left: `${column * 100 / columns}%`, width: `${100 / columns}%` }}>
    <button type="button" className={`calendar-time-event${enabled ? " calendar-time-movable" : ""}`} {...pointerHandlers} onPointerDown={(input) => begin(input, "move")}
      onClick={(input) => { if (clicked(input.detail)) { input.currentTarget.focus(); onEdit(event); } }}
      aria-label={`Edit ${event.title} on ${day}: ${event.status === "cancelled" ? "Cancelled · " : ""}Display time ${gridClock(range.start + start * 60_000, true, zone)}–${end === range.minutes ? "24:00" : gridClock(range.start + end * 60_000, true, zone)} · ${zone}. ${eventTimeLabel(event)}`}
      title={`${eventTimeLabel(event)}${enabled ? " · Drag to move; use Change time for keyboard actions." : ""}`}>
      <span>{event.status === "cancelled" ? "Cancelled · " : ""}{event.title}</span>
      <small>{Date.parse(event.start) < range.start ? "Continues · " : ""}{gridClock(range.start + start * 60_000, transition, zone)}–{end === range.minutes ? "24:00" : gridClock(range.start + end * 60_000, transition, zone)}{Date.parse(event.end) > range.end ? " · continues" : ""}</small>
    </button>
    {enabled ? <button ref={toggle} type="button" className="calendar-time-adjust-handle" {...pointerHandlers} onPointerDown={(input) => begin(input, "resize-end")}
      onClick={(input) => { if (clicked(input.detail)) openMenu(); }} aria-label={`Change time for ${event.title} on ${day}`} aria-expanded={Boolean(menu)} aria-controls={menuId}
      title="Drag to resize the end. Activate for 15-minute move and resize actions.">↕</button> : null}
    {proposal ? <div className="calendar-time-proposal" aria-hidden="true" style={{ top: `${preview!.mode === "move" ? preview!.minutes : 0}px`, height: `${Math.max(24, end - start + (preview!.mode === "resize-end" ? preview!.minutes : 0))}px` }}>Proposed {calendarClock(Date.parse(proposal.start), zone)}–{calendarClock(Date.parse(proposal.end), zone)}</div> : null}
    {menu ? createPortal(<div ref={popup} id={menuId} className="menu-panel calendar-time-actions" role="group" aria-label={`Time actions for ${event.title}`} style={menu}
      onKeyDown={(input) => { if (input.key === "Escape") { input.preventDefault(); setMenu(null); toggle.current?.focus(); } }}
      onBlur={(input) => { if (!input.currentTarget.contains(input.relatedTarget) && !toggle.current?.contains(input.relatedTarget)) setMenu(null); }}>
      {([['move', -15, 'Move earlier 15 minutes'], ['move', 15, 'Move later 15 minutes'], ['resize-end', -15, 'End earlier 15 minutes'], ['resize-end', 15, 'End later 15 minutes']] as const).map(([mode, minutes, label]) => {
        const next = proposeTimeChange(event, day, zone, mode, minutes);
        return <button type="button" key={label} disabled={!next} onClick={() => { if (next) { toggle.current?.focus(); setMenu(null); onTimeChange?.(event, next); } }}>{label}</button>;
      })}
    </div>, document.body) : null}
  </div>;
}
