import { Component, type ReactNode } from "react";
import { reportRecoverable } from "../recovery";

// A failed optional renderer must not replace its note, siblings, or navigation.
export class FeatureBoundary extends Component<{
  name: string; children: ReactNode; fallback?: ReactNode;
}, { failed: boolean }> {
  state = { failed: false };
  static getDerivedStateFromError() { return { failed: true }; }
  componentDidCatch(error: Error) { reportRecoverable(this.props.name, error); }
  render() {
    return this.state.failed
      ? (this.props.fallback ?? <p className="muted">{this.props.name} is unavailable. Other content remains usable.</p>)
      : this.props.children;
  }
}
