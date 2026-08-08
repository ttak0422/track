import { useEffect, useState } from "react";

// useThemeVersion bumps whenever the app theme changes (the data-theme attribute or the OS
// preference), so theme-dependent renders can redraw with the new colors.
export function useThemeVersion(): number {
  const [version, setVersion] = useState(0);

  useEffect(() => {
    const bump = () => setVersion((value) => value + 1);
    const observer = new MutationObserver(bump);
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });

    const media = window.matchMedia?.("(prefers-color-scheme: dark)");
    media?.addEventListener("change", bump);

    return () => {
      observer.disconnect();
      media?.removeEventListener("change", bump);
    };
  }, []);

  return version;
}
