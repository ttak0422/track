import { Link, Outlet, useNavigate, useRouterState } from "@tanstack/react-router";
import { GraphPanel } from "./GraphPanel";
import { HierarchyMenu } from "./HierarchyMenu";
import { BrandMark } from "./Logo";
import { MobileDock } from "./MobileDock";
import { FloatingLayer } from "./preview/FloatingLayer";
import { FloatingProvider } from "./preview/floatingStore";
import { SidebarSearch } from "./SidebarSearch";
import { SidebarNew } from "./SidebarNew";
import { SidebarHistory } from "./SidebarHistory";
import { TabBar } from "./tabs/TabBar";
import { TabsProvider } from "./tabs/tabsStore";
import { ThemeMenu } from "./ThemeMenu";
import { openJournal } from "../api";
import { useLiveEvents } from "../hooks/useLiveEvents";
import { useSiteQuery } from "../queries";
import { START_PAGE_ID, STATIC_MODE } from "../runtime";
import { NoteControlsProvider } from "../noteControls";
import { NotificationToast } from "../notifications";
import { NoteRailControls } from "./NoteRailControls";
import { RailTip } from "./RailTip";
import { SearchProvider } from "../searchState";
import { IconAffiliate, IconCalendar, IconChecklist, IconNotebook, RailIcon } from "./icons";
import { IconMicrophone } from "@tabler/icons-react";

export function Shell() {
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  // Normalize a trailing slash: the prerendered static site serves routes as directories (/graph/).
  const path = pathname.replace(/\/$/, "") || "/";
  const isHome = path === "/";
  const isGraph = path === "/graph";
  const isCalendar = path === "/calendar";
  // Note pages carry their own always-on local graph in the aside; the static "/" renders the
  // start note, so it counts as one too — but only when a start page exists (without one, static
  // "/" is the empty state, which should keep the floating graph launcher).
  const isNote = /^\/notes\/[^/]+$/.test(path) || (isHome && START_PAGE_ID !== "");
  // The search hero: the live workspace's "/" when no home note is configured. With one, "/" renders
  // that note and is an ordinary note page; the static "/" is either the start page or the empty
  // state. The hero owns its own scrolling, but not the chrome — the rail and tab strip are on every
  // route, so the workspace's views are reachable from the landing screen too.
  const isHero = isHome && !STATIC_MODE && START_PAGE_ID === "";
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
      <NoteControlsProvider>
      <FloatingProvider>
      <TabsProvider>
      <main className={`workspace${isHero ? " home" : ""}`}>
          <aside className="sidebar">
            <nav className="activity-rail" aria-label="Workspace views">
              <div className="rail-scroll">
                {/* On the static site "/" is the start page; on the live server it is the heatmap home. */}
                <RailTip label={STATIC_MODE ? "Start page" : "track home"}>
                  <Link
                    className="rail-button rail-brand"
                    to="/"
                    aria-label={STATIC_MODE ? "Start page" : "track home"}
                  >
                    <BrandMark icon={site.data?.icon} className="rail-mark" />
                  </Link>
                </RailTip>
                <SidebarSearch />
                <SidebarNew />
                <SidebarHistory />
                {/* The published static site is read-only and cannot create journals. */}
                {!STATIC_MODE && (
                  <RailTip label="Today's journal">
                    <button
                      className="rail-button"
                      type="button"
                      aria-label="Today's journal"
                      onClick={openTodayJournal}
                    >
                      <RailIcon Icon={IconNotebook} />
                    </button>
                  </RailTip>
                )}
                {showCalendar && (
                  <RailTip label="Calendar">
                    <Link className="rail-button" to="/calendar" aria-label="Calendar">
                      <RailIcon Icon={IconCalendar} />
                    </Link>
                  </RailTip>
                )}
                {/* The open-task listing is live-only: the published bundle carries dated tasks alone. */}
                {!STATIC_MODE && (
                  <RailTip label="Tasks">
                    <Link className="rail-button" to="/tasks" aria-label="Tasks">
                      <RailIcon Icon={IconChecklist} />
                    </Link>
                  </RailTip>
                )}
                {/* The deliberate "up" tree, beside the link graph it is a hand-drawn path through. */}
                <HierarchyMenu />
                <RailTip label="Full graph">
                  <Link className="rail-button" to="/graph" aria-label="Full graph">
                    <RailIcon Icon={IconAffiliate} />
                  </Link>
                </RailTip>
                <RailTip label="Voice input">
                  <Link className="rail-button" to="/voice" aria-label="Voice input">
                    <RailIcon Icon={IconMicrophone} />
                  </Link>
                </RailTip>
                {/* The open note's own controls, below the workspace's views. Absent while no note is
                    open, so the dock keeps carrying nothing but navigation the rest of the time. */}
                {!STATIC_MODE && <NoteRailControls />}
              </div>
              {/* Settings is outside the scrolling group so it stays reachable on a short window. */}
              <ThemeMenu />
            </nav>
          </aside>
        <div className="reader-pane">
          <TabBar />
          <section className="reader">
            <Outlet />
          </section>
        </div>
        {/* The floating launcher serves the views without a graph of their own (day, tags, search);
            note pages show the local graph in their aside instead. */}
        {isHero || isGraph || isCalendar || isNote ? null : <GraphPanel />}
        <FloatingLayer />
        <NotificationToast />
        <MobileDock />
      </main>
      </TabsProvider>
      </FloatingProvider>
      </NoteControlsProvider>
    </SearchProvider>
  );
}
