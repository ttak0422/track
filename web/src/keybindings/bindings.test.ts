import { describe, expect, it, vi } from "vitest";
import { defaultReadingBindings, dispatchKeybinding, type ActionRegistry } from "./bindings";

const registry = (): ActionRegistry => ({
  "preview.scrollDown": { available: () => true, run: vi.fn() },
  "preview.scrollUp": { available: () => true, run: vi.fn() },
  "tabs.next": { available: () => true, run: vi.fn() },
  "tabs.previous": { available: () => true, run: vi.fn() },
  "reading.releaseFocus": { available: () => true, run: vi.fn() },
});
const key = (name: string, init: KeyboardEventInit = {}) => new KeyboardEvent("keydown", { key: name, cancelable: true, ...init });

describe("reading keybinding dispatch", () => {
  it.each(defaultReadingBindings)("dispatches $action from data", (binding) => {
    const actions = registry();
    const event = key(binding.key, { shiftKey: binding.shift });
    expect(dispatchKeybinding(event, "native.noteReading", actions)).toBe(true);
    expect(event.defaultPrevented).toBe(true);
    expect(actions[binding.action].run).toHaveBeenCalledOnce();
  });
  it("accepts another binding table without changing actions", () => {
    const actions = registry();
    dispatchKeybinding(key("ArrowDown"), "native.noteReading", actions,
      [{ key: "ArrowDown", action: "preview.scrollDown", context: "native.noteReading", repeat: true }]);
    expect(actions["preview.scrollDown"].run).toHaveBeenCalledOnce();
  });
  it.each([{ ctrlKey: true }, { metaKey: true }, { altKey: true }, { shiftKey: true }, { isComposing: true }, { keyCode: 229 }])("leaves modified/IME input alone: %j", (init) => {
    const event = key("j", init);
    expect(dispatchKeybinding(event, "native.noteReading", registry())).toBe(false);
    expect(event.defaultPrevented).toBe(false);
  });
  it("respects a missing context, consumed event, and unavailable action", () => {
    const actions = registry();
    const event = key("Tab");
    expect(dispatchKeybinding(event, null, actions)).toBe(false);
    actions["tabs.next"].available = () => false;
    expect(dispatchKeybinding(event, "native.noteReading", actions)).toBe(false);
    expect(event.defaultPrevented).toBe(false);
    event.preventDefault();
    expect(dispatchKeybinding(event, "native.noteReading", registry())).toBe(false);
  });
  it("repeats scroll but consumes held Tab without navigating repeatedly", () => {
    const actions = registry();
    dispatchKeybinding(key("j", { repeat: true }), "native.noteReading", actions);
    expect(actions["preview.scrollDown"].run).toHaveBeenCalledOnce();
    const tab = key("Tab", { repeat: true });
    dispatchKeybinding(tab, "native.noteReading", actions);
    expect(tab.defaultPrevented).toBe(true);
    expect(actions["tabs.next"].run).not.toHaveBeenCalled();
  });
});
