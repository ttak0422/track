import { Link, Outlet, useNavigate, useRouterState } from "@tanstack/react-router";
import { GraphBackground } from "./GraphBackground";
import { GraphPanel } from "./GraphPanel";
import { KMark } from "./Logo";
import { FloatingLayer } from "./preview/FloatingLayer";
import { FloatingProvider } from "./preview/floatingStore";
import { SidebarSearch } from "./SidebarSearch";
import { TabBar } from "./tabs/TabBar";
import { TabsProvider } from "./tabs/tabsStore";
import { ThemeMenu } from "./ThemeMenu";
import { openJournal } from "../api";
import { useLiveEvents } from "../hooks/useLiveEvents";
import { useSiteQuery } from "../queries";
import { STATIC_MODE } from "../runtime";
import { SearchProvider } from "../searchState";

export function Shell() {
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  // Normalize a trailing slash: the prerendered static site serves routes as directories (/graph/).
  const path = pathname.replace(/\/$/, "") || "/";
  const isHome = path === "/";
  const isGraph = path === "/graph";
  const isCalendar = path === "/calendar";
  // The live workspace has a heatmap home at "/"; the static site does not — there "/" is the empty state
  // (all tabs closed), so it keeps the normal chrome (sidebar, no home hero, no ambient graph).
  const isLiveHome = isHome && !STATIC_MODE;
  const navigate = useNavigate();
  useLiveEvents();

  // A published site opts into the calendar explicitly (`track export-site --calendar`): reference
  // sites (help docs) skip it, activity-shaped ones (a blog over a vault) include it. The live
  // workspace always shows it.
  const site = useSiteQuery();
  const showCalendar = !STATIC_MODE || site.data?.calendar === true;

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
      <TabsProvider>
      <main className={`workspace${isLiveHome ? " home" : ""}`}>
        {isLiveHome ? null : (
          <aside className="sidebar">
            <nav className="activity-rail" aria-label="Workspace views">
              {/* On the static site "/" is the start page; on the live server it is the heatmap home. */}
              <Link
                className="rail-button rail-brand"
                to="/"
                aria-label={STATIC_MODE ? "Start page" : "track home"}
                title={STATIC_MODE ? "Start page" : "track home"}
              >
                <KMark className="rail-mark" />
              </Link>
              <SidebarSearch />
              {/* The published static site is read-only and cannot create journals. */}
              {!STATIC_MODE && (
                <button
                  className="rail-button"
                  type="button"
                  aria-label="Today's journal"
                  title="Today's journal"
                  onClick={openTodayJournal}
                >
                  <span className="rail-icon rail-icon-journal" aria-hidden="true" />
                </button>
              )}
              {showCalendar && (
                <Link
                  className="rail-button"
                  to="/calendar"
                  aria-label="Calendar"
                  title="Calendar"
                >
                  <RailCalendarIcon />
                </Link>
              )}
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
        <div className="reader-pane">
          {isLiveHome ? null : <TabBar />}
          <section className="reader">
            {isLiveHome ? <GraphBackground /> : null}
            <Outlet />
          </section>
        </div>
        {/* The live home is a hero screen with no note; the static "/" renders the start note, so it
            keeps the graph launcher like any other note page. */}
        {isLiveHome || isGraph || isCalendar ? null : <GraphPanel />}
        <FloatingLayer />
      </main>
      </TabsProvider>
      </FloatingProvider>
    </SearchProvider>
  );
}

function RailCalendarIcon() {
  return (
    <svg className="rail-icon-svg" viewBox="0 0 24 24" width="20" height="20" aria-hidden="true">
      <rect
        x="4"
        y="5.5"
        width="16"
        height="14"
        rx="2"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.6"
      />
      <line x1="4" y1="9.5" x2="20" y2="9.5" stroke="currentColor" strokeWidth="1.6" />
      <line x1="8.5" y1="3.5" x2="8.5" y2="6.5" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
      <line x1="15.5" y1="3.5" x2="15.5" y2="6.5" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
      <circle cx="8.5" cy="13" r="1.2" fill="currentColor" />
      <circle cx="12" cy="13" r="1.2" fill="currentColor" />
      <circle cx="15.5" cy="13" r="1.2" fill="currentColor" />
      <circle cx="8.5" cy="16.5" r="1.2" fill="currentColor" />
      <circle cx="12" cy="16.5" r="1.2" fill="currentColor" />
    </svg>
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
