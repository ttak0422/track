// Owns this source's listeners and debounce timers. Refresh callbacks update the shared cache;
// requests they start belong to QueryClient, so disposing a subscription does not cancel them.
export function subscribeToLiveEvents(source: EventSource, refresh: { change: () => void; data: () => void }) {
  let disposed = false;
  const timers: { change?: number; data?: number } = {};

  function schedule(kind: keyof typeof refresh) {
    if (disposed) return;
    window.clearTimeout(timers[kind]);
    timers[kind] = window.setTimeout(() => {
      delete timers[kind];
      if (!disposed) refresh[kind]();
    }, 150);
  }

  const change = () => schedule("change");
  const data = () => schedule("data");
  const open = () => { change(); data(); };
  // Open also covers reconnection: events missed while disconnected are not replayed.
  source.addEventListener("open", open);
  source.addEventListener("change", change);
  source.addEventListener("data", data);

  return () => {
    if (disposed) return;
    disposed = true;
    window.clearTimeout(timers.change);
    window.clearTimeout(timers.data);
    source.removeEventListener("open", open);
    source.removeEventListener("change", change);
    source.removeEventListener("data", data);
    source.close();
  };
}
