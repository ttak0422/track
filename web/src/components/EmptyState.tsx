import { Mark } from "./Logo";
import { IconArrowLeft, RailIcon } from "./icons";
import { useSiteQuery } from "../queries";

// EmptyState fills the static site's reader when every tab is closed: the mark, faint and centered
// (VS Code's empty-editor watermark), plus arrows pointing back at the floating rail, so a
// suddenly-blank area reads as "nothing open" — here is how to open something — rather than looking
// broken.
export function EmptyState() {
  const site = useSiteQuery();
  return (
    <div className="empty-state" aria-hidden="true">
      <Mark className="empty-mark" />
      {/* One entry per rail button, in rail order. The guides ride the rail's own rhythm (8px pad,
          40px rows, 6px gaps) instead of hardcoded offsets, so they stay lined up whatever the dock is
          carrying — hence the calendar row appearing on exactly the sites whose rail has a calendar
          button (Shell's showCalendar). */}
      <ul className="empty-guides">
        <Guide>Start page</Guide>
        <Guide>Search notes</Guide>
        <Guide>Recently opened</Guide>
        {site.data?.calendar === true && <Guide>Calendar</Guide>}
        <Guide>Hierarchy</Guide>
        <Guide>Explore the graph</Guide>
        <Guide className="empty-guide-settings">Settings</Guide>
      </ul>
    </div>
  );
}

function Guide({ children, className }: { children: string; className?: string }) {
  return (
    <li className={className ? `empty-guide ${className}` : "empty-guide"}>
      <RailIcon Icon={IconArrowLeft} size={14} />
      {children}
    </li>
  );
}
