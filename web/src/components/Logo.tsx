// The app's own marks. The standalone files in web/public are the source of truth for both the app and
// the favicon; the light/dark pairs are switched with the same explicit theme selection as the rest of
// the shell. A configured site icon still wins over the built-in mark.

interface LogoProps {
  className?: string;
}

// The site's brand mark: the published site icon (site.json icon, a file at the site root) when one
// is configured, the built-in square otherwise. Decorative (alt="") — the wrapping link carries the
// accessible label. A user-supplied image keeps its own colors rather than adapting to the theme.
export function BrandMark({ icon, className }: { icon?: string; className?: string }) {
  if (!icon) return <Mark className={className} />;
  // BASE_URL always ends with "/" (see vite.config.ts); icon is a bare site-root file name.
  return <img className={className} src={import.meta.env.BASE_URL + icon} alt="" />;
}

// The built-in square mark. Both theme variants are mounted so a manual theme change can switch the
// asset without requiring a rerender of the surrounding shell.
export function Mark({ className }: LogoProps) {
  return <ThemedAsset light="track-icon.svg" dark="track-icon-dark.svg" className={className} />;
}

// Full "track" wordmark, using the refreshed light/dark lockups from web/public.
export function TrackLogo({ className }: LogoProps) {
  return (
    <ThemedAsset
      light="track-lockup.svg"
      dark="track-lockup-dark.svg"
      className={className}
      alt="track"
    />
  );
}

function ThemedAsset({
  light,
  dark,
  className,
  alt = "",
}: {
  light: string;
  dark: string;
  className?: string;
  alt?: string;
}) {
  const lightClass = [className, "theme-asset-light"].filter(Boolean).join(" ");
  const darkClass = [className, "theme-asset-dark"].filter(Boolean).join(" ");
  return (
    <>
      <img className={lightClass} src={import.meta.env.BASE_URL + light} alt={alt} />
      <img className={darkClass} src={import.meta.env.BASE_URL + dark} alt={alt} />
    </>
  );
}
