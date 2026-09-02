import { render, screen } from "@testing-library/react";
import { act } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { NotificationProvider, NotificationToast, useNotifications } from "./notifications";

vi.mock("@tanstack/react-router", () => ({ useNavigate: () => vi.fn() }));

function Raise({ message }: { message: string }) {
  const { notify } = useNotifications();
  return (
    <button type="button" onClick={() => notify(message)}>
      raise
    </button>
  );
}

describe("NotificationToast", () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it("expires on its own instead of waiting to be dismissed", () => {
    render(
      <NotificationProvider>
        <Raise message="Vault updated" />
        <NotificationToast />
      </NotificationProvider>,
    );

    act(() => void screen.getByText("raise").click());
    expect(screen.getByRole("alert")).toBeInTheDocument();

    act(() => vi.advanceTimersByTime(10_000));
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("draws the countdown bar for exactly the lifetime the timer runs", () => {
    render(
      <NotificationProvider>
        <Raise message="Vault updated" />
        <NotificationToast />
      </NotificationProvider>,
    );

    act(() => void screen.getByText("raise").click());
    const timer = document.querySelector(".notification-timer");
    expect(timer).not.toBeNull();
    // The animation duration is the toast's own lifetime, so the bar cannot drift from the timer.
    expect((timer as HTMLElement).style.animationDuration).toBe("8000ms");
  });

  it("restarts the clock when a later notification replaces the current one", () => {
    render(
      <NotificationProvider>
        <Raise message="Vault updated" />
        <NotificationToast />
      </NotificationProvider>,
    );

    act(() => void screen.getByText("raise").click());
    act(() => vi.advanceTimersByTime(5_000));
    act(() => void screen.getByText("raise").click());
    act(() => vi.advanceTimersByTime(5_000));

    expect(screen.getByRole("alert")).toBeInTheDocument();
  });
});
