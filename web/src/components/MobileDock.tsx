import { Link, useNavigate } from "@tanstack/react-router";
import { IconMicrophone } from "@tabler/icons-react";
import {
  useEffect,
  useRef,
  useState,
  type CSSProperties,
  type PointerEvent as ReactPointerEvent,
  type ReactNode,
} from "react";
import { createPortal } from "react-dom";
import { openJournal } from "../api";
import { keys } from "../keys";
import { editorModes, useNoteControls } from "../noteControls";
import { useSiteQuery } from "../queries";
import { STATIC_MODE } from "../runtime";
import { themeModes, useThemeMode } from "../themeState";
import { BrandMark } from "./Logo";
import {
  IconAffiliate,
  IconCalendar,
  IconChecklist,
  IconFileText,
  IconHistory,
  IconNotebook,
  IconSearch,
  IconSettings,
  RailIcon,
} from "./icons";
import { SearchPanel } from "./SearchPanel";
import { railAnchor } from "./railAnchor";
import { useTabs } from "./tabs/tabsStore";

// The phone's navigation: a round track logo floating over the reading surface that drags anywhere
// and, tapped, fans its controls out in the half-circle facing away from the edge it sits at. It
// replaces the foot dock, which spent a whole strip of the window on the same controls. The fan is
// the dock's buttons — search, history, the views, settings — arranged around the mark instead of
// in a row that takes the screen's bottom. The logo itself is the home link's mark, so the brand
// stays where the mark always is.
const FAB_SIZE = 48;
const EDGE = 12;
const FAN_RADIUS = 108;
const FAN_BUTTON = 44;

type Popup = "search" | "history" | "settings" | "note" | null;

interface FanAction {
  key: string;
  label: string;
  icon: ReactNode;
  run: () => void;
}

export function MobileDock() {
  const navigate = useNavigate();
  const site = useSiteQuery();
  const { recent } = useTabs();
  const [theme, setTheme] = useThemeMode();
  // The open note's own controls. They live in the rail on a desk, and the rail is what the mark
  // replaced here, so the phone reaches them through the fan instead — the note group appears in it
  // for exactly as long as a note is open, the way the rail's does.
  const { mode, setMode, follow, setFollow, actions: noteActions } = useNoteControls();
  const fabRef = useRef<HTMLButtonElement>(null);
  // The mark's position, as a fixed left/top pair. null means "the corner it starts in" — the mark is
  // left to the stylesheet's own right/bottom offsets there, so an untouched mark follows the window
  // without React hearing about the resize at all.
  const [pos, setPos] = useState<{ x: number; y: number } | null>(null);
  const [open, setOpen] = useState(false);
  const [popup, setPopup] = useState<Popup>(null);
  const dragRef = useRef<{ x: number; y: number; moved: boolean } | null>(null);

  function position(): { x: number; y: number } {
    if (pos) return pos;
    if (typeof window === "undefined") return { x: EDGE, y: EDGE };
    return { x: window.innerWidth - FAB_SIZE - EDGE, y: window.innerHeight - FAB_SIZE - EDGE };
  }

  // Drag moves the mark, with the fan's 180° arc pointing away from the edge it ends up at; a press
  // that stays put is a tap and opens the fan. The drag threshold tells the two apart — a thumb
  // always shifts a little while it lands.
  function onPointerDown(event: ReactPointerEvent<HTMLButtonElement>) {
    event.currentTarget.setPointerCapture?.(event.pointerId);
    dragRef.current = { x: event.clientX, y: event.clientY, moved: false };
  }

  function onPointerMove(event: ReactPointerEvent<HTMLButtonElement>) {
    const drag = dragRef.current;
    if (!drag) return;
    if (!drag.moved && Math.hypot(event.clientX - drag.x, event.clientY - drag.y) > 6) {
      drag.moved = true;
    }
    if (!drag.moved) return;
    const cur = position();
    setPos(clampToWindow({ x: cur.x + event.clientX - drag.x, y: cur.y + event.clientY - drag.y }));
    // The move is relative to the last one, not to where the thumb landed: keeping the original
    // pointer as the origin added the whole travel again on every event, so the mark ran away from
    // the thumb and pinned itself to whichever edge it reached first.
    drag.x = event.clientX;
    drag.y = event.clientY;
  }

  function onPointerUp() {
    const drag = dragRef.current;
    dragRef.current = null;
    if (drag && !drag.moved) {
      if (popup) {
        setPopup(null);
      } else {
        setOpen((value) => !value);
      }
    }
  }

  // One surface at a time: opening a popup closes the fan, and vice versa.
  function openPopup(next: Popup) {
    setOpen(false);
    setPopup(next);
  }

  // A dragged mark is held inside the window as it changes size: a mark parked at the right edge of a
  // wide window sat off-screen entirely once the window became a phone's, and nothing brought it back.
  useEffect(() => {
    function onResize() {
      setPos((cur) => (cur ? clampToWindow(cur) : cur));
    }
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, []);

  // Outside taps and Escape close whatever is up. The dock sits over the reading surface, so the
  // surface itself is the backdrop.
  useEffect(() => {
    if (!open && !popup) return;
    function onPointerDown(event: MouseEvent) {
      const target = event.target as Element | null;
      if (!target || !target.closest?.(".mobile-dock")) {
        setOpen(false);
        setPopup(null);
      }
    }
    function onKeyDown(event: KeyboardEvent) {
      if (keys.close(event)) {
        setOpen(false);
        setPopup(null);
      }
    }
    document.addEventListener("mousedown", onPointerDown);
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("mousedown", onPointerDown);
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [open, popup]);

  async function openTodayJournal() {
    const now = new Date();
    const date = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}-${String(
      now.getDate(),
    ).padStart(2, "0")}`;
    try {
      const { note_id } = await openJournal(date);
      navigate({ to: "/notes/$noteId", params: { noteId: String(note_id) } });
    } catch {
      // A failed open simply leaves the user on the current view.
    }
  }

  const showCalendar = !STATIC_MODE || site.data?.calendar === true;

  // The fan's actions, mirroring the dock they replaced: the view switches, the search and history
  // popups, and settings. Live-only surfaces (journal, tasks) stay live-only here.
  const actions: FanAction[] = [];
  if (noteActions) {
    actions.push({
      key: "note",
      label: "This note",
      icon: <RailIcon Icon={IconFileText} />,
      run: () => openPopup("note"),
    });
  }
  if (!STATIC_MODE) {
    actions.push({
      key: "journal",
      label: "Today's journal",
      icon: <RailIcon Icon={IconNotebook} />,
      run: () => {
        setOpen(false);
        void openTodayJournal();
      },
    });
  }
  actions.push(
    {
      key: "search",
      label: "Search notes",
      icon: <RailIcon Icon={IconSearch} />,
      run: () => openPopup("search"),
    },
    {
      key: "history",
      label: "Recently opened notes",
      icon: <RailIcon Icon={IconHistory} />,
      run: () => openPopup("history"),
    },
  );
  if (showCalendar) {
    actions.push({
      key: "calendar",
      label: "Calendar",
      icon: <RailIcon Icon={IconCalendar} />,
      run: () => {
        setOpen(false);
        void navigate({ to: "/calendar" });
      },
    });
  }
  if (!STATIC_MODE) {
    actions.push({
      key: "tasks",
      label: "Tasks",
      icon: <RailIcon Icon={IconChecklist} />,
      run: () => {
        setOpen(false);
        void navigate({ to: "/tasks" });
      },
    });
  }
  actions.push(
    {
      key: "graph",
      label: "Full graph",
      icon: <RailIcon Icon={IconAffiliate} />,
      run: () => {
        setOpen(false);
        void navigate({ to: "/graph" });
      },
    },
    {
      key: "settings",
      label: "Settings",
      icon: <RailIcon Icon={IconSettings} />,
      run: () => openPopup("settings"),
    },
  );
  if (!noteActions) {
    actions.splice(actions.length - 1, 0, {
      key: "voice",
      label: "Voice input",
      icon: <RailIcon Icon={IconMicrophone} />,
      run: () => {
        setOpen(false);
        void navigate({ to: "/voice" });
      },
    });
  }

  const p = position();
  const fan = open ? fanPlacement(p, actions.length) : [];

  return (
    <div className="mobile-dock">
      <button
        ref={fabRef}
        type="button"
        className="mobile-dock-fab"
        aria-label={open ? "Close note menu" : "Open note menu"}
        aria-expanded={open || popup !== null}
        style={pos ? { left: pos.x, top: pos.y } : undefined}
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={onPointerUp}
      >
        <BrandMark icon={site.data?.icon} className="mobile-dock-mark" />
      </button>
      {open
        ? fan.map((button, index) => (
            <button
              key={actions[index].key}
              type="button"
              className="mobile-dock-fan-btn"
              aria-label={actions[index].label}
              title={actions[index].label}
              style={button}
              onClick={actions[index].run}
            >
              {actions[index].icon}
            </button>
          ))
        : null}
      {popup === "search"
        ? createPortal(
            // The palette is the same layer SidebarSearch draws: a body sibling over the page, so
            // the phone's search behaves exactly like the desk's.
            <div className="mobile-dock-popup">
              <div className="search-backdrop" onMouseDown={() => setPopup(null)} />
              <div className="search-popup" role="dialog" aria-label="Search notes">
                <SearchPanel autoFocus onNavigate={() => setPopup(null)} />
              </div>
            </div>,
            document.body,
          )
        : null}
      {popup === "history" && typeof document !== "undefined"
        ? createPortal(
            <div className="menu-panel note-menu-panel history-panel" style={railAnchor(fabRef.current)}>
              <h2 className="rail-panel-title">History</h2>
              <div className="history-scroll" role="menu" aria-label="Recently opened notes">
                {recent.length === 0 ? (
                  <p className="history-empty">No recently opened notes</p>
                ) : (
                  <ul className="history-list">
                    {recent.map((note) => (
                      <li key={note.id}>
                        <Link
                          className="backlink"
                          role="menuitem"
                          to="/notes/$noteId"
                          params={{ noteId: String(note.id) }}
                          title={note.title || note.id}
                          onClick={() => setPopup(null)}
                        >
                          {note.title || note.id}
                        </Link>
                      </li>
                    ))}
                  </ul>
                )}
              </div>
            </div>,
            document.body,
          )
        : null}
      {popup === "note" && noteActions && typeof document !== "undefined"
        ? createPortal(
            // The rail's note group as one panel: the follow toggle, the display mode, and the two
            // dialogs. The copy actions stay off it — a phone's own selection copies text, and the
            // panel is worth more as the four controls that have nowhere else to go.
            <div className="menu-panel note-menu-panel" style={railAnchor(fabRef.current)}>
              <h2 className="rail-panel-title">This note</h2>
              <button
                type="button"
                aria-pressed={follow}
                onClick={() => setFollow(!follow)}
              >
                Follow the editor: {follow ? "On" : "Off"}
              </button>
              <div className="theme-switch" role="group" aria-label="Display mode">
                {editorModes.map((each) => (
                  <button
                    aria-pressed={mode === each}
                    key={each}
                    type="button"
                    onClick={() => setMode(each)}
                  >
                    {each[0].toUpperCase() + each.slice(1)}
                  </button>
                ))}
              </div>
              <button
                type="button"
                onClick={() => {
                  setPopup(null);
                  noteActions.onMeta();
                }}
              >
                Meta…
              </button>
              <button
                type="button"
                className="danger-item"
                onClick={() => {
                  setPopup(null);
                  noteActions.onDelete();
                }}
              >
                Delete…
              </button>
            </div>,
            document.body,
          )
        : null}
      {popup === "settings" && typeof document !== "undefined"
        ? createPortal(
            <div className="menu-panel note-menu-panel mobile-settings" style={railAnchor(fabRef.current)}>
              <h2 className="rail-panel-title">Settings</h2>
              <div className="theme-switch" role="group" aria-label="Theme">
                {themeModes.map((mode) => (
                  <button
                    aria-pressed={theme === mode}
                    key={mode}
                    type="button"
                    onClick={() => setTheme(mode)}
                  >
                    {mode[0].toUpperCase() + mode.slice(1)}
                  </button>
                ))}
              </div>
            </div>,
            document.body,
          )
        : null}
    </div>
  );
}

// fanPlacement spreads n buttons on an arc around the mark, opening toward the middle of the window
// and so away from whatever edges the mark is resting against. A mark against one edge has a half
// circle to fan into; one in a corner has only the quadrant facing the middle, so the spread narrows
// there — and the radius grows with the number of buttons, because an arc that cannot hold them
// side by side used to end with the clamp below stacking half the fan on one point.
function fanPlacement(
  p: { x: number; y: number },
  n: number,
): CSSProperties[] {
  if (typeof window === "undefined") return [];
  const cx = p.x + FAB_SIZE / 2;
  const cy = p.y + FAB_SIZE / 2;
  // An edge closer than the fan's own radius is a wall the arc cannot reach across. The arc points
  // away from the walls the mark is resting against: away from one of them across a half circle,
  // away from two (a corner) across the quadrant between them.
  const away = {
    x: cx < FAN_RADIUS ? 1 : window.innerWidth - cx < FAN_RADIUS ? -1 : 0,
    y: cy < FAN_RADIUS ? -1 : window.innerHeight - cy < FAN_RADIUS ? 1 : 0,
  };
  const spread = away.x !== 0 && away.y !== 0 ? 90 : 180;
  // Screen angles, counter-clockwise from east: 0° right, 90° up, 180° left, 270° down. A mark with
  // room on every side fans upward, over the note rather than along it.
  const base =
    away.x === 0 && away.y === 0 ? 90 : (Math.atan2(away.y, away.x) * 180) / Math.PI;
  const arc = (spread * Math.PI) / 180;
  const radius = Math.max(FAN_RADIUS, ((n - 1) * (FAN_BUTTON + 6)) / arc);

  const out: CSSProperties[] = [];
  for (let i = 0; i < n; i++) {
    const step = n > 1 ? (i * spread) / (n - 1) : spread / 2;
    const angle = ((base - spread / 2 + step) * Math.PI) / 180;
    const x = clamp(
      cx + radius * Math.cos(angle) - FAN_BUTTON / 2,
      8,
      window.innerWidth - FAN_BUTTON - 8,
    );
    const y = clamp(
      cy - radius * Math.sin(angle) - FAN_BUTTON / 2,
      8,
      window.innerHeight - FAN_BUTTON - 8,
    );
    out.push({ left: x, top: y });
  }
  return out;
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value));
}

// Keep the mark whole inside the window, at the same EDGE the stylesheet parks it at.
function clampToWindow(p: { x: number; y: number }): { x: number; y: number } {
  return {
    x: clamp(p.x, EDGE, window.innerWidth - FAB_SIZE - EDGE),
    y: clamp(p.y, EDGE, window.innerHeight - FAB_SIZE - EDGE),
  };
}






