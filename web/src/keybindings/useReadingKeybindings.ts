import { useEffect, useRef, type RefObject } from "react";
import { useNavigate } from "@tanstack/react-router";
import { STATIC_MODE } from "../runtime";
import { isViewTab, tabRoute, useTabs } from "../components/tabs/tabsStore";
import { dispatchKeybinding, type ActionRegistry } from "./bindings";
import { hasReadingOverlay, isReadingTextTarget } from "./readingContext";
import { TabCycle } from "./tabCycle";

declare global {
  interface Window {
    // Set only by Track.app on its top-level workspace origin. Not a native command bridge.
    __trackNativeReading?: boolean;
  }
}

export function nativeReadingEnabled(): boolean {
  return !STATIC_MODE && typeof window !== "undefined" && window.__trackNativeReading === true;
}

export function useReadingKeybindings(readerRef: RefObject<HTMLElement | null>, reading: boolean): boolean {
  const native = nativeReadingEnabled();
  const enabled = native && reading;
  const { tabs, activeID } = useTabs();
  const navigate = useNavigate();
  const cycleRef = useRef(new TabCycle());
  const current = useRef({ tabs, activeID, enabled, navigate });
  current.current = { tabs, activeID, enabled, navigate };

  useEffect(() => {
    if (!enabled) cycleRef.current.reset();
    else cycleRef.current.observe(tabs.filter((tab) => !isViewTab(tab.id)).map((tab) => tab.id), activeID);
  }, [tabs, activeID, enabled]);

  useEffect(() => {
    const reader = readerRef.current;
    if (!native || !reader) return;
    const cycle = cycleRef.current;
    let released = false;
    let composing = false;
    const inReadingContext = () => current.current.enabled && !released && !composing &&
      document.activeElement === reader && window.getSelection()?.isCollapsed === true && !hasReadingOverlay(document) &&
      reader.querySelector(".note-preview") !== null && !reader.querySelector("textarea");
    const noteIDs = () => current.current.tabs.filter((tab) => !isViewTab(tab.id)).map((tab) => tab.id);
    const canCycle = () => {
      const ids = noteIDs();
      return current.current.activeID !== null && ids.includes(current.current.activeID) && ids.length > 1;
    };
    const switchTab = (delta: 1 | -1) => {
      // Keep the router's existing unsaved-changes blocker as the only owner of confirmation.
      if (current.current.activeID === null) return;
      const id = cycle.next(noteIDs(), current.current.activeID, delta);
      if (!id) return;
      // Do not lock on this promise: TanStack leaves it pending when a blocker cancels navigation.
      // The next key resolves from the observed activeID, so a canceled/slow switch cannot skip notes.
      void current.current.navigate(tabRoute(id)).catch(() => cycle.reset());
    };
    const scroll = (direction: 1 | -1) => {
      // Preview-only notes flow through .reader, not .note-preview; avoid scrolling an aside,
      // floating preview, code block, or the document. Instant steps also behave well on key repeat.
      const prose = reader.querySelector(".note-preview .markdown-view") ?? reader.querySelector(".note-preview")!;
      const lineHeight = Number.parseFloat(getComputedStyle(prose).lineHeight) || 24;
      reader.scrollBy({ top: direction * lineHeight * 3, behavior: "instant" });
    };
    const actions: ActionRegistry = {
      "preview.scrollDown": { available: () => true, run: () => scroll(1) },
      "preview.scrollUp": { available: () => true, run: () => scroll(-1) },
      "tabs.next": { available: canCycle, run: () => switchTab(1) },
      "tabs.previous": { available: canCycle, run: () => switchTab(-1) },
      "reading.releaseFocus": { available: () => true, run: () => { released = true; cycle.reset(); } },
    };
    const keydown = (event: KeyboardEvent) => {
      if (event.target !== reader) return;
      dispatchKeybinding(event, inReadingContext() ? "native.noteReading" : null, actions);
    };
    const click = (event: MouseEvent) => {
      cycle.reset();
      if (event.button !== 0 || event.ctrlKey || event.metaKey || event.altKey || event.shiftKey ||
          !window.getSelection()?.isCollapsed || !current.current.enabled || hasReadingOverlay(document) ||
          !isReadingTextTarget(event.target, reader)) return;
      released = false;
      reader.focus({ preventScroll: true });
    };
    const focus = () => { released = false; cycle.reset(); };
    const compositionStart = () => { composing = true; };
    const compositionEnd = () => { composing = false; };
    reader.addEventListener("focus", focus);
    reader.addEventListener("blur", focus);
    document.addEventListener("keydown", keydown);
    document.addEventListener("click", click);
    document.addEventListener("compositionstart", compositionStart, true);
    document.addEventListener("compositionend", compositionEnd, true);
    return () => {
      reader.removeEventListener("focus", focus);
      reader.removeEventListener("blur", focus);
      document.removeEventListener("keydown", keydown);
      document.removeEventListener("click", click);
      document.removeEventListener("compositionstart", compositionStart, true);
      document.removeEventListener("compositionend", compositionEnd, true);
    };
  }, [native, readerRef]);
  return enabled;
}
