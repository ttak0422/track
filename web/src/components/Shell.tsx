import { Link, Outlet, useRouterState } from "@tanstack/react-router";
import { useState } from "react";
import { ActivityPanel } from "./ActivityPanel";
import { GraphBackground } from "./GraphBackground";
import { GraphPanel } from "./GraphPanel";
import { KMark } from "./Logo";
import { SearchPanel } from "./SearchPanel";
import { ThemeMenu } from "./ThemeMenu";
import { SearchProvider } from "../searchState";

export function Shell() {
  const [sidebarCollapsed, setSidebarCollapsed] = useState(true);
  const isHome = useRouterState({ select: (state) => state.location.pathname === "/" });

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
            <ActivityPanel />
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
