import { KMark } from "./Logo";
import { useSiteQuery } from "../queries";

// EmptyState fills the static site's reader when every tab is closed: a faint centered mark (VS Code's
// empty-editor watermark) plus arrows pointing back at the rail dock, so a suddenly-blank area reads as
// "nothing open" — here is how to open something — rather than looking broken.
export function EmptyState() {
  const site = useSiteQuery();
  return (
    <div className="empty-state" aria-hidden="true">
      <KMark className="empty-mark" />
      {/* One entry per dock button, in dock order. The guides ride the dock's own rhythm (40px rows,
          6px gaps, centered on the same axis) instead of hardcoded offsets, so they stay lined up
          whatever the dock is carrying — hence the calendar row appearing on exactly the sites whose
          dock has a calendar button (Shell's showCalendar). */}
      <ul className="empty-guides">
        <li className="empty-guide">Start page</li>
        <li className="empty-guide">Search notes</li>
        {site.data?.calendar === true && <li className="empty-guide">Calendar</li>}
        <li className="empty-guide">Explore the graph</li>
        <li className="empty-guide">Settings</li>
      </ul>
    </div>
  );
}
