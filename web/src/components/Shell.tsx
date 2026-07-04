import { Link, Outlet, useNavigate, useRouterState } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { useEffect, useRef } from "react";
import { GraphBackground } from "./GraphBackground";
import { GraphPanel } from "./GraphPanel";
import { KMark } from "./Logo";
import { FloatingLayer } from "./preview/FloatingLayer";
import { FloatingProvider } from "./preview/floatingStore";
import { SidebarSearch } from "./SidebarSearch";
import { TabBar } from "./tabs/TabBar";
import { TabsProvider } from "./tabs/tabsStore";
import { ThemeMenu } from "./ThemeMenu";
import { getSite, openJournal } from "../api";
import { useLiveEvents } from "../hooks/useLiveEvents";
import { STATIC_MODE } from "../runtime";
import { SearchProvider } from "../searchState";

export function Shell() {
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const isHome = pathname === "/";
  const isGraph = pathname === "/graph";
  // The live workspace has a heatmap home at "/"; the static site does not — there "/" is the empty state
  // (all tabs closed), so it keeps the normal chrome (sidebar, no home hero, no ambient graph).
  const isLiveHome = isHome && !STATIC_MODE;
  const navigate = useNavigate();
  useLiveEvents();
  useStaticStartPage(navigate);

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
              <Link className="rail-button rail-brand" to="/" aria-label="track home" title="track home">
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
        {isHome || isGraph ? null : <GraphPanel />}
        <FloatingLayer />
      </main>
      </TabsProvider>
      </FloatingProvider>
    </SearchProvider>
  );
}

// useStaticStartPage opens the configured start page once, on launch, when the static site is entered at
// its home route. It never fires again when the user later returns to "/" by closing every tab, so that
// empty state stays empty instead of bouncing back to the start page.
function useStaticStartPage(navigate: ReturnType<typeof useNavigate>) {
  const site = useQuery({ queryKey: ["site"], queryFn: getSite, enabled: STATIC_MODE });
  const done = useRef(false);
  // Whether the app was entered at the home route (vs a deep link to a note/graph), from the launch hash.
  const enteredAtHome = useRef(
    typeof window === "undefined" || window.location.hash === "" || window.location.hash === "#/",
  );

  useEffect(() => {
    if (!STATIC_MODE || done.current) return;
    if (!enteredAtHome.current) {
      done.current = true;
      return;
    }
    const root = site.data?.root;
    if (root) {
      done.current = true;
      void navigate({ to: "/notes/$noteId", params: { noteId: String(root) }, replace: true });
    }
  }, [site.data, navigate]);
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
