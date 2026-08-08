import { Mark } from "./Logo";
import { useSiteQuery } from "../queries";

// EmptyState fills the static site's reader when every tab is closed: the mark, faint and centered
// (VS Code's empty-editor watermark), plus arrows pointing back at the rail, so a suddenly-blank area
// reads as "nothing open" — here is how to open something — rather than looking broken.
export function EmptyState() {
  const site = useSiteQuery();
  return (
    <div className="empty-state" aria-hidden="true">
      <Mark className="empty-mark" />
      {/* One entry per rail button, in rail order. The guides ride the rail's own rhythm (8px pad,
          40px rows, 6px gaps, a spacer before settings) instead of hardcoded offsets, so they stay
          lined up whatever the rail is carrying — hence the calendar row appearing on exactly the
          sites whose rail has a calendar button (Shell's showCalendar). */}
      <ul className="empty-guides">
        <li className="empty-guide">Start page</li>
        <li className="empty-guide">Search notes</li>
        {site.data?.calendar === true && <li className="empty-guide">Calendar</li>}
        <li className="empty-guide">Explore the graph</li>
        <li className="empty-guide empty-guide-settings">Settings</li>
      </ul>
    </div>
  );
}
