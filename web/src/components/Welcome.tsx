import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { getSite } from "../api";

// Welcome is the static site's empty state — VS Code's "no editor open" watermark: nothing is loaded, so
// show only a faint, centered list of the ways in. Deliberately minimal (no logo hero or search box) so
// it reads as an empty canvas, not the live workspace home.
export function Welcome() {
  const site = useQuery({ queryKey: ["site"], queryFn: getSite });

  if (site.isError) {
    return <p className="error">site data is missing</p>;
  }

  const root = site.data?.root;

  return (
    <div className="welcome" aria-label="Nothing open">
      <ul className="welcome-hints">
        {root ? (
          <li>
            <Link to="/notes/$noteId" params={{ noteId: String(root) }}>
              Open the start page
            </Link>
          </li>
        ) : null}
        <li>
          <Link to="/graph">Explore the graph</Link>
        </li>
      </ul>
    </div>
  );
}
