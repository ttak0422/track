// The visible strip promotes every selected tab to the front. Keep the order at the start of a
// keyboard walk, otherwise repeated Tab alternates between only the two most recent notes.
export class TabCycle {
  private order: string[] = [];
  private expected: string | null = null;

  reset(): void {
    this.order = [];
    this.expected = null;
  }

  observe(ids: readonly string[], active: string | null): void {
    if (active !== this.expected || ids.length !== this.order.length ||
        ids.some((id) => !this.order.includes(id))) this.reset();
  }

  next(ids: readonly string[], active: string, delta: 1 | -1): string | null {
    if (ids.length < 2 || !ids.includes(active)) return null;
    if (active !== this.expected || ids.length !== this.order.length ||
        ids.some((id) => !this.order.includes(id))) {
      this.order = [...ids];
    }
    const index = this.order.indexOf(active);
    const target = this.order[(index + delta + this.order.length) % this.order.length];
    this.expected = target;
    return target;
  }
}
