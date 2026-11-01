import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { NotificationProvider, NotificationToast, useNotifications } from "./notifications";
import { connectNativeNotifications, postNativeActivity } from "./nativeNotifications";

const navigate = vi.hoisted(() => vi.fn());
vi.mock("@tanstack/react-router", () => ({ useNavigate: () => navigate }));
afterEach(() => { delete window.__trackNativeNotifications; vi.clearAllMocks(); });

function Controls() {
  const { notify, notifyActivity } = useNotifications();
  return <>
    <button onClick={() => notifyActivity("Updated: private title", "work~123", "2026-11-01")}>activity</button>
    <button onClick={() => notify("Save failed")}>error</button>
  </>;
}
function mount() { render(<NotificationProvider><Controls /><NotificationToast /></NotificationProvider>); }
async function activity() { await act(async () => { fireEvent.click(screen.getByText("activity")); }); }

it("routes activity to native without private content or a duplicate toast; keeps action errors in-app", async () => {
  const postMessage = vi.fn().mockResolvedValue(true);
  window.__trackNativeNotifications = { postMessage };
  mount();
  await activity();
  expect(postMessage).toHaveBeenCalledWith({ action: "activity", noteID: "work~123", day: "2026-11-01" });
  expect(screen.queryByRole("alert")).toBeNull();
  fireEvent.click(screen.getByText("error"));
  expect(screen.getByRole("alert")).toHaveTextContent("Save failed");
  expect(postMessage).toHaveBeenCalledTimes(2);
});

it.each([false, "throws", "absent"])("keeps a clickable in-app fallback when native is %s", async (mode) => {
  if (mode !== "absent") window.__trackNativeNotifications = {
    postMessage: mode === "throws" ? vi.fn().mockRejectedValue(new Error("unavailable")) : vi.fn().mockResolvedValue(false),
  };
  mount();
  await activity();
  expect(screen.getByRole("alert")).toHaveTextContent("Updated: private title");
  fireEvent.click(screen.getByRole("button", { name: "Updated: private title" }));
  expect(navigate).toHaveBeenCalledWith({ to: "/notes/$noteId", params: { noteId: "work~123" } });
});

it("a late failure cannot hide a newer action error", async () => {
  let finish!: (value: boolean) => void;
  window.__trackNativeNotifications = { postMessage: vi.fn((body) => body.action === "ready" ? Promise.resolve(true) : new Promise<boolean>((resolve) => { finish = resolve; })) };
  mount();
  await activity();
  fireEvent.click(screen.getByText("error"));
  await act(async () => finish(false));
  expect(screen.getByRole("alert")).toHaveTextContent("Save failed");
});

it("connects the router before ready, validates click IDs, and removes its listener", () => {
  const open = vi.fn();
  window.__trackNativeNotifications = { postMessage: vi.fn(() => {
    window.dispatchEvent(new CustomEvent("track-native-notification-open", { detail: { noteID: "work~123" } }));
    return Promise.resolve(true);
  }) };
  const disconnect = connectNativeNotifications(open);
  expect(open).toHaveBeenCalledWith("work~123");
  window.dispatchEvent(new CustomEvent("track-native-notification-open", { detail: { noteID: "../../secret" } }));
  disconnect();
  window.dispatchEvent(new CustomEvent("track-native-notification-open", { detail: { noteID: "456" } }));
  expect(open).toHaveBeenCalledTimes(1);
});

it("does not treat a nonboolean native acknowledgement as delivery", async () => {
  window.__trackNativeNotifications = { postMessage: vi.fn().mockResolvedValue("true") };
  expect(await postNativeActivity("123", "2026-11-01")).toBe(false);
});
