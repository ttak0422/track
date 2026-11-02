import { Component, type ReactNode } from "react";
import { reportRecoverable } from "../recovery";

// A failed optional renderer must not replace its note, siblings, or navigation.
export class FeatureBoundary extends Component<{
  name: string; children: ReactNode; fallback?: ReactNode; resetKey?: string;
}, { failed: boolean; resetKey?: string }> {
  state = { failed: false, resetKey: this.props.resetKey };
  // Reset an error when its source changes without replacing a healthy subtree.
  // Keying the boundary by source remounts every async embed on ordinary edits.
  static getDerivedStateFromProps(props: { resetKey?: string }, state: { resetKey?: string }) {
    return props.resetKey !== state.resetKey ? { failed: false, resetKey: props.resetKey } : null;
  }
  static getDerivedStateFromError() { return { failed: true }; }
  componentDidCatch(error: Error) { reportRecoverable(this.props.name, error); }
  render() {
    return this.state.failed
      ? (this.props.fallback ?? <p className="muted">{this.props.name} is unavailable. Other content remains usable.</p>)
      : this.props.children;
  }
}
