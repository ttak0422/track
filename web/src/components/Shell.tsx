import { Link, Outlet, useNavigate, useRouterState } from "@tanstack/react-router";
import { GraphBackground } from "./GraphBackground";
import { GraphPanel } from "./GraphPanel";
import { KMark } from "./Logo";
import { FloatingLayer } from "./preview/FloatingLayer";
import { FloatingProvider } from "./preview/floatingStore";
import { SidebarSearch } from "./SidebarSearch";
import { ThemeMenu } from "./ThemeMenu";
import { openJournal } from "../api";
import { useLiveEvents } from "../hooks/useLiveEvents";
import { SearchProvider } from "../searchState";

export function Shell() {
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const isHome = pathname === "/";
  const isGraph = pathname === "/graph";
  const navigate = useNavigate();
  useLiveEvents();

  // Open (creating if needed) today's journal and jump to it, mirroring how the activity heatmap opens a
  // day. The local-time YYYY-MM-DD key matches the journal id the server derives from the date.
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

  return (
    <SearchProvider>
      <FloatingProvider>
      <main className={`workspace${isHome ? " home" : ""}`}>
        {isHome ? null : (
          <aside className="sidebar">
            <nav className="activity-rail" aria-label="Workspace views">
              <Link className="rail-button rail-brand" to="/" aria-label="track home" title="track home">
                <KMark className="rail-mark" />
              </Link>
              <SidebarSearch />
              <button
                className="rail-button"
                type="button"
                aria-label="Today's journal"
                title="Today's journal"
                onClick={openTodayJournal}
              >
                <span className="rail-icon rail-icon-journal" aria-hidden="true" />
              </button>
              <Link
                className="rail-button"
                to="/graph"
                aria-label="Full graph"
                title="Full graph"
              >
                <RailGraphIcon />
              </Link>
              <div className="rail-spacer" />
              <ThemeMenu />
            </nav>
          </aside>
        )}
        <section className="reader">
          {isHome ? <GraphBackground /> : null}
          <Outlet />
        </section>
        {isHome || isGraph ? null : <GraphPanel />}
        <FloatingLayer />
      </main>
      </FloatingProvider>
    </SearchProvider>
  );
}

function RailGraphIcon() {
  return (
    <svg className="rail-icon-svg" viewBox="0 0 24 24" width="20" height="20" aria-hidden="true">
      <line x1="7" y1="8" x2="16" y2="7" stroke="currentColor" strokeWidth="1.6" />
      <line x1="7" y1="8" x2="12" y2="17" stroke="currentColor" strokeWidth="1.6" />
      <line x1="16" y1="7" x2="12" y2="17" stroke="currentColor" strokeWidth="1.6" />
      <circle cx="7" cy="8" r="2.4" fill="currentColor" />
      <circle cx="16" cy="7" r="2.4" fill="currentColor" />
      <circle cx="12" cy="17" r="2.4" fill="currentColor" />
    </svg>
  );
}
