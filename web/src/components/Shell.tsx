import { Link, Outlet } from "@tanstack/react-router";
import { useState } from "react";
import { ActivityPanel } from "./ActivityPanel";
import { GraphPanel } from "./GraphPanel";
import { SearchPanel } from "./SearchPanel";
import { ThemeMenu } from "./ThemeMenu";
import { SearchProvider } from "../searchState";

export function Shell() {
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false);

  return (
    <SearchProvider>
      <main className={sidebarCollapsed ? "workspace sidebar-collapsed" : "workspace"}>
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
                <h1>
                  <Link to="/">track</Link>
                </h1>
                <p>Local graph workspace</p>
              </div>
              <ThemeMenu />
            </header>
            <SearchPanel />
            <ActivityPanel />
          </div>
        </aside>
        <section className="reader">
          <Outlet />
        </section>
        <GraphPanel />
      </main>
    </SearchProvider>
  );
}
