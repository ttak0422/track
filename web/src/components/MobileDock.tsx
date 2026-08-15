import { Link, useNavigate } from "@tanstack/react-router";
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
import { useSiteQuery } from "../queries";
import { STATIC_MODE } from "../runtime";
import { themeModes, useThemeMode } from "../themeState";
import { BrandMark } from "./Logo";
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

type Popup = "search" | "history" | "settings" | null;

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
  const fabRef = useRef<HTMLButtonElement>(null);
  // The mark's position, as a fixed left/top pair. null means "the corner it starts in", which is
  // measured on demand so the very first frame before layout still has a place to stand.
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
    const x = clamp(cur.x + event.clientX - drag.x, EDGE, window.innerWidth - FAB_SIZE - EDGE);
    const y = clamp(cur.y + event.clientY - drag.y, EDGE, window.innerHeight - FAB_SIZE - EDGE);
    setPos({ x, y });
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
  if (!STATIC_MODE) {
    actions.push({
      key: "journal",
      label: "Today's journal",
      icon: <JournalIcon />,
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
      icon: <SearchIcon />,
      run: () => openPopup("search"),
    },
    {
      key: "history",
      label: "Recently opened notes",
      icon: <HistoryIcon />,
      run: () => openPopup("history"),
    },
  );
  if (showCalendar) {
    actions.push({
      key: "calendar",
      label: "Calendar",
      icon: <CalendarIcon />,
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
      icon: <TasksIcon />,
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
      icon: <GraphIcon />,
      run: () => {
        setOpen(false);
        void navigate({ to: "/graph" });
      },
    },
    {
      key: "settings",
      label: "Settings",
      icon: <GearIcon />,
      run: () => openPopup("settings"),
    },
  );

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
        style={{ left: p.x, top: p.y }}
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

// fanPlacement spreads n buttons on a 180° arc pointing away from the edge the mark is nearest to —
// a mark at the foot of the screen fans upward, one at the left edge fans rightward, and so on —
// each clamped into the window so no button opens off-screen.
function fanPlacement(
  p: { x: number; y: number },
  n: number,
): CSSProperties[] {
  if (typeof window === "undefined") return [];
  const cx = p.x + FAB_SIZE / 2;
  const cy = p.y + FAB_SIZE / 2;
  const distances = {
    top: cy,
    bottom: window.innerHeight - cy,
    left: cx,
    right: window.innerWidth - cx,
  } as const;
  const nearest = (Object.keys(distances) as (keyof typeof distances)[]).sort(
    (a, b) => distances[a] - distances[b],
  )[0];
  // Screen angles: 0° is right, 90° down, 180° left, 270° up.
  const base = nearest === "bottom" ? 270 : nearest === "top" ? 90 : nearest === "left" ? 0 : 180;

  const out: CSSProperties[] = [];
  for (let i = 0; i < n; i++) {
    const angle = ((base - 90 + (i * 180) / (n - 1)) * Math.PI) / 180;
    const x = clamp(
      cx + FAN_RADIUS * Math.cos(angle) - FAN_BUTTON / 2,
      8,
      window.innerWidth - FAN_BUTTON - 8,
    );
    const y = clamp(
      cy - FAN_RADIUS * Math.sin(angle) - FAN_BUTTON / 2,
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

// The fan glyphs are the rail's own family: 24-unit viewBox at 20px, stroke-only, round caps.
function SearchIcon() {
  return (
    <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <circle cx="10.75" cy="10.75" r="6.5" />
      <line x1="15.5" y1="15.5" x2="20.5" y2="20.5" />
    </svg>
  );
}

function HistoryIcon() {
  return (
    <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M3.5 12a8.5 8.5 0 1 0 2.8-6.3L3.5 8" />
      <path d="M3.5 3.5v4.5h4.5" />
      <path d="M12 7v5l3 2" />
    </svg>
  );
}

function CalendarIcon() {
  return (
    <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <rect x="4" y="5.5" width="16" height="14" rx="2" />
      <line x1="4" y1="9.5" x2="20" y2="9.5" />
      <line x1="8.5" y1="3.5" x2="8.5" y2="6.5" />
      <line x1="15.5" y1="3.5" x2="15.5" y2="6.5" />
      <circle cx="8.5" cy="13" r="0.9" />
      <circle cx="12" cy="13" r="0.9" />
      <circle cx="15.5" cy="13" r="0.9" />
      <circle cx="8.5" cy="16.5" r="0.9" />
      <circle cx="12" cy="16.5" r="0.9" />
    </svg>
  );
}

function TasksIcon() {
  return (
    <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="m4 8 2 2 3.5-4" />
      <path d="m4 17 2 2 3.5-4" />
      <line x1="13" y1="8" x2="20" y2="8" />
      <line x1="13" y1="17" x2="20" y2="17" />
    </svg>
  );
}

function GraphIcon() {
  return (
    <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <line x1="7" y1="8" x2="16" y2="7" />
      <line x1="7" y1="8" x2="12" y2="17" />
      <line x1="16" y1="7" x2="12" y2="17" />
      <circle cx="7" cy="8" r="2" />
      <circle cx="16" cy="7" r="2" />
      <circle cx="12" cy="17" r="2" />
    </svg>
  );
}

function JournalIcon() {
  return (
    <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <rect x="4" y="3.5" width="16" height="17" rx="2" />
      <line x1="4" y1="8.5" x2="20" y2="8.5" />
      <line x1="8" y1="12.5" x2="16" y2="12.5" />
      <line x1="8" y1="16" x2="14" y2="16" />
    </svg>
  );
}

function GearIcon() {
  return (
    <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <circle cx="12" cy="12" r="3" />
      <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z" />
    </svg>
  );
}
