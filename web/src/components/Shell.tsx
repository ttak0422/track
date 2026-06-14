import { Link, Outlet } from "@tanstack/react-router";
import { ActivityPanel } from "./ActivityPanel";
import { SearchPanel } from "./SearchPanel";
import { ThemeMenu } from "./ThemeMenu";

export function Shell() {
  return (
    <main className="workspace">
      <aside className="sidebar">
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
      </aside>
      <section className="reader">
        <Outlet />
      </section>
    </main>
  );
}
