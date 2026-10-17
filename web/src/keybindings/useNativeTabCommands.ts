import { useEffect, useLayoutEffect, useRef } from "react";
import { useNavigate } from "@tanstack/react-router";
import { tabRoute, useTabs } from "../components/tabs/tabsStore";
import { nativeReadingEnabled } from "./useReadingKeybindings";
import { hasReadingOverlay } from "./readingContext";
import { TabCycle } from "./tabCycle";

// Host-to-workspace events use the same tab store and router guards as the visible controls.
export function useNativeTabCommands(): void {
  const native = nativeReadingEnabled();
  const { tabs, activeID, close } = useTabs();
  const navigate = useNavigate();
  const cycle = useRef(new TabCycle());
  const current = useRef({ tabs, activeID, close, navigate });
  current.current = { tabs, activeID, close, navigate };
  useLayoutEffect(() => { cycle.current.observe(tabs.map((tab) => tab.id), activeID); }, [tabs, activeID]);

  useEffect(() => {
    if (!native) return;
    const command = (event: Event) => {
      if (hasReadingOverlay(document) || document.activeElement?.tagName === "IFRAME") return;
      const { tabs, activeID, close, navigate } = current.current;
      if (activeID === null) return;
      const action = (event as CustomEvent).detail;
      if (action === "tabs.close") {
        cycle.current.reset();
        close(activeID);
      } else if (action === "tabs.previous" || action === "tabs.next") {
        const id = cycle.current.next(tabs.map((tab) => tab.id), activeID, action === "tabs.next" ? 1 : -1);
        if (id) void navigate(tabRoute(id)).catch(() => cycle.current.reset());
      }
    };
    const reset = () => cycle.current.reset();
    window.addEventListener("track:native-tab-command", command);
    document.addEventListener("pointerdown", reset);
    return () => {
      window.removeEventListener("track:native-tab-command", command);
      document.removeEventListener("pointerdown", reset);
    };
  }, [native]);
}
