// Serializable defaults, action names, and context matching are independent of React/DOM layout.
// A future settings loader can provide another binding list without changing the actions.
export type ReadingAction = "preview.scrollDown" | "preview.scrollUp" | "tabs.next" | "tabs.previous" | "reading.releaseFocus";
export type KeybindingContext = "native.noteReading";
export interface Keybinding {
  key: string;
  shift?: boolean;
  ctrl?: boolean;
  alt?: boolean;
  meta?: boolean;
  context: KeybindingContext;
  action: ReadingAction;
  repeat: boolean;
}

export const defaultReadingBindings: readonly Keybinding[] = [
  { key: "j", context: "native.noteReading", action: "preview.scrollDown", repeat: true },
  { key: "k", context: "native.noteReading", action: "preview.scrollUp", repeat: true },
  { key: "Tab", context: "native.noteReading", action: "tabs.next", repeat: false },
  { key: "Tab", shift: true, context: "native.noteReading", action: "tabs.previous", repeat: false },
  { key: "Escape", context: "native.noteReading", action: "reading.releaseFocus", repeat: false },
];

export type ActionRegistry = Record<ReadingAction, {
  available: () => boolean;
  run: () => void;
}>;

export function dispatchKeybinding(
  event: KeyboardEvent,
  context: KeybindingContext | null,
  actions: ActionRegistry,
  bindings: readonly Keybinding[] = defaultReadingBindings,
): boolean {
  // WebKit can report keyCode 229 around an IME commit even without isComposing.
  if (!context || event.defaultPrevented || event.isComposing || event.keyCode === 229) return false;
  const binding = bindings.find((item) => item.context === context && item.key === event.key &&
    Boolean(item.shift) === event.shiftKey && Boolean(item.ctrl) === event.ctrlKey &&
    Boolean(item.alt) === event.altKey && Boolean(item.meta) === event.metaKey);
  if (!binding || !actions[binding.action].available()) return false;
  // Held Tab must not race navigation or fall through into focus movement on its repeats.
  event.preventDefault();
  if (!event.repeat || binding.repeat) actions[binding.action].run();
  return true;
}
