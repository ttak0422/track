import { createPortal } from "react-dom";
import {
  useEffect,
  useRef,
  useState,
  type CSSProperties,
  type FocusEvent,
  type ReactNode,
} from "react";
import { hoverOpen } from "./hoverOpen";
import { railAnchor } from "./railAnchor";

// RailTip names the rail's icon-only controls that open no panel of their own (journal, Calendar,
// Tasks, the full graph). It is variant 3's floating layer cut down to a single-line label — the
// .tab-tools look — and it replaces those controls' native title tooltip: a control that carries a
// popup of its own does not also carry one. It is a label, not a menu — pointer-events never reach
// it, so hover opens nothing but a name. Portalled like every rail flyout, because the fixed rail
// clips its overflow and owns a stacking context below floating previews.
export function RailTip({ label, children }: { label: string; children: ReactNode }) {
  const [open, setOpen] = useState(false);
  const [anchor, setAnchor] = useState<CSSProperties | undefined>(undefined);
  const hostRef = useRef<HTMLDivElement>(null);
  const closeTimer = useRef<number | undefined>(undefined);

  function cancelClose() {
    if (closeTimer.current !== undefined) window.clearTimeout(closeTimer.current);
    closeTimer.current = undefined;
  }

  function showTip() {
    cancelClose();
    setAnchor(railAnchor(hostRef.current));
    setOpen(true);
  }

  function scheduleClose() {
    cancelClose();
    closeTimer.current = window.setTimeout(() => setOpen(false), 160);
  }

  // Keyboard focus opens it too — :focus-visible only, because the focus a tap or click gives has
  // a click on its way. Focus bubbles up from the wrapped control, so the host carries the handler.
  function focusTip(event: FocusEvent<HTMLDivElement>) {
    if (event.target.matches(":focus-visible")) showTip();
  }

  // And focus leaving closes it again: the label names what is aimed at, it is not a stop of its own.
  function blurTip(event: FocusEvent<HTMLDivElement>) {
    const next = event.relatedTarget;
    if (!(next instanceof Node && event.currentTarget.contains(next))) setOpen(false);
  }

  useEffect(() => cancelClose, []);

  const tip = open ? (
    <div className="rail-tip" style={anchor} aria-hidden="true">
      {label}
    </div>
  ) : null;

  return (
    <div ref={hostRef} {...hoverOpen(showTip, scheduleClose)} onFocus={focusTip} onBlur={blurTip}>
      {children}
      {tip && typeof document !== "undefined" ? createPortal(tip, document.body) : null}
    </div>
  );
}
