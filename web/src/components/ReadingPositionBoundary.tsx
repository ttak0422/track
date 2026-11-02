import { Component, type ReactNode, type RefObject } from "react";

type Props = {
  root: RefObject<HTMLElement | null>;
  scope: string;
  revision: string;
  children: ReactNode;
};
type Snapshot = { anchor: HTMLElement; scroller: HTMLElement; top: number } | null;

// Health notices can appear above the passage being read. Snapshot the actual visible
// passage before React changes layout, not only scrollTop (which can be clamped by
// a shorter document). This runs only for health changes, never on a scroll event.
export class ReadingPositionBoundary extends Component<Props, object, Snapshot> {
  getSnapshotBeforeUpdate(previous: Props): Snapshot {
    if (previous.scope !== this.props.scope || previous.revision === this.props.revision) return null;
    const root = this.props.root.current;
    if (!root) return null;
    for (const anchor of root.querySelectorAll<HTMLElement>(
      '.markdown-view [data-copy-line-start], .markdown-view p, .markdown-view h1, .markdown-view h2, .markdown-view h3, .markdown-view pre, .markdown-view table',
    )) {
      let scroller = anchor.parentElement;
      while (scroller) {
        if (scroller.scrollHeight > scroller.clientHeight && /auto|scroll/.test(getComputedStyle(scroller).overflowY)) break;
        scroller = scroller.parentElement;
      }
      // At the top, show the notice naturally rather than scrolling it out of view.
      if (!scroller || scroller.scrollTop <= 0) continue;
      const rect = anchor.getBoundingClientRect();
      const viewport = scroller.getBoundingClientRect();
      if (rect.height > 0 && rect.bottom > viewport.top && rect.top < viewport.bottom) {
        return { anchor, scroller, top: rect.top - viewport.top - scroller.clientTop };
      }
    }
    return null;
  }
  componentDidUpdate(_previous: Props, _state: object, snapshot: Snapshot) {
    if (!snapshot?.anchor.isConnected || !snapshot.scroller.isConnected) return;
    const delta = snapshot.anchor.getBoundingClientRect().top - snapshot.scroller.getBoundingClientRect().top - snapshot.scroller.clientTop - snapshot.top;
    // Native browser anchoring may have already compensated. Do not apply it twice.
    if (Math.abs(delta) >= 0.5) snapshot.scroller.scrollTop += delta;
  }
  render() { return this.props.children; }
}
