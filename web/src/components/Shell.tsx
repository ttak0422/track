import { Link, Outlet, useNavigate, useRouterState } from "@tanstack/react-router";
import { useState } from "react";
import { GraphBackground } from "./GraphBackground";
import { GraphPanel } from "./GraphPanel";
import { KMark } from "./Logo";
import { SearchPanel } from "./SearchPanel";
import { ThemeMenu } from "./ThemeMenu";
import { openJournal } from "../api";
import { useLiveEvents } from "../hooks/useLiveEvents";
import { SearchProvider } from "../searchState";

export function Shell() {
  const [sidebarCollapsed, setSidebarCollapsed] = useState(true);
  const isHome = useRouterState({ select: (state) => state.location.pathname === "/" });
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
      <main
        className={`workspace${isHome ? " home" : ""}${
          sidebarCollapsed ? " sidebar-collapsed" : ""
        }`}
      >
        {isHome ? null : (
        <aside className="sidebar">
          <nav className="activity-rail" aria-label="Workspace views">
            <button
              className="rail-button"
              type="button"
              aria-label={sidebarCollapsed ? "Expand sidebar" : "Collapse sidebar"}
              aria-expanded={!sidebarCollapsed}
              title={sidebarCollapsed ? "Expand sidebar" : "Collapse sidebar"}
              onClick={() => setSidebarCollapsed((collapsed) => !collapsed)}
            >
              <span className="rail-icon rail-icon-sidebar" aria-hidden="true" />
            </button>
            <Link className="rail-button" to="/" aria-label="Home" title="Home">
              <span className="rail-icon rail-icon-home" aria-hidden="true" />
            </Link>
            <button
              className="rail-button"
              type="button"
              aria-label="Today's journal"
              title="Today's journal"
              onClick={openTodayJournal}
            >
              <span className="rail-icon rail-icon-journal" aria-hidden="true" />
            </button>
          </nav>
          <div className="sidebar-content">
            <header className="brand">
              <div>
                <h1 className="brand-title">
                  <Link to="/" aria-label="track home">
                    <KMark className="brand-mark" />
                  </Link>
                </h1>
              </div>
              <ThemeMenu />
            </header>
            <SearchPanel />
          </div>
        </aside>
        )}
        <section className="reader">
          {isHome ? <GraphBackground /> : null}
          <Outlet />
        </section>
        {isHome ? null : <GraphPanel />}
      </main>
    </SearchProvider>
  );
}
