import { KMark } from "./Logo";

// EmptyState fills the static site's reader when every tab is closed: a faint centered mark (VS Code's
// empty-editor watermark) plus arrows pointing back at the sidebar, so a suddenly-blank area reads as
// "nothing open" — here is how to open something — rather than looking broken.
export function EmptyState() {
  return (
    <div className="empty-state" aria-hidden="true">
      <KMark className="empty-mark" />
      <ul className="empty-guides">
        <li className="empty-guide empty-guide-home">Start page</li>
        <li className="empty-guide empty-guide-search">Search notes</li>
        <li className="empty-guide empty-guide-graph">Explore the graph</li>
        <li className="empty-guide empty-guide-settings">Settings</li>
      </ul>
    </div>
  );
}
