import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { getSite } from "../api";
import { TrackLogo } from "./Logo";
import { SearchPanel } from "./SearchPanel";

// Welcome is the static site's empty-state landing — VS Code's welcome tab. The reader has no note open,
// so show the mark, a jump to the site's entry note and graph, and search to reach any other note (the
// sidebar is hidden on the home route, so navigation has to live here).
export function Welcome() {
  const site = useQuery({ queryKey: ["site"], queryFn: getSite });

  if (site.isError) {
    return <p className="error">site data is missing</p>;
  }

  const root = site.data?.root;

  return (
    <section className="home-hero">
      <TrackLogo className="home-logo" />
      <nav className="welcome-actions" aria-label="Get started">
        {root ? (
          <Link className="welcome-action" to="/notes/$noteId" params={{ noteId: String(root) }}>
            Open the start page
          </Link>
        ) : null}
        <Link className="welcome-action" to="/graph">
          Explore the graph
        </Link>
      </nav>
      <SearchPanel />
    </section>
  );
}
