import { ActivityPanel } from "./ActivityPanel";
import { TrackLogo } from "./Logo";
import { SearchPanel } from "./SearchPanel";

interface SearchHomeProps {
  // Closes a host that opened this (the sidebar popup) once a result is chosen; the route home passes
  // nothing.
  onNavigate?: () => void;
  autoFocus?: boolean;
}

// SearchHome is the wordmark + activity heatmap + search field shown on the live workspace's "/" home.
// The sidebar search popup reuses it so pressing the magnifier opens the same start surface, not a
// bare input. The caller supplies the wrapper (.home-hero full page, or .search-popup beside the rail).
export function SearchHome({ onNavigate, autoFocus }: SearchHomeProps) {
  return (
    <>
      <TrackLogo className="home-logo" />
      <ActivityPanel variant="home" />
      <SearchPanel autoFocus={autoFocus} onNavigate={onNavigate} />
    </>
  );
}
