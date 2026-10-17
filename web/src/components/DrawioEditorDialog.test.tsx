import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { DrawioEditorDialog } from "./DrawioEditorDialog";
import { DRAWIO_ORIGIN } from "./markdown/drawioEditing";

const xml = '<mxfile><diagram id="one"><mxGraphModel /></diagram><diagram id="two">compressed-payload</diagram></mxfile>';
const edited = xml.replace('id="one"', 'id="edited"');
let frame: HTMLIFrameElement;
let post: ReturnType<typeof vi.spyOn>;
const apply = vi.fn(() => true);
const close = vi.fn();
function message(data: unknown, origin = DRAWIO_ORIGIN, source: MessageEventSource | null = frame.contentWindow) {
  act(() => { window.dispatchEvent(new MessageEvent("message", { data: typeof data === "string" ? data : JSON.stringify(data), origin, source })); });
}
function open() {
  const view = render(<DrawioEditorDialog xml={xml} onApply={apply} onClose={close} />);
  fireEvent.click(screen.getByRole("button", { name: "Open online editor" }));
  frame = screen.getByTitle("Online draw.io editor") as HTMLIFrameElement;
  post = vi.spyOn(frame.contentWindow!, "postMessage");
  return view;
}
function loaded() { message({ event: "init" }); message({ event: "load" }); }

beforeEach(() => {
  vi.resetAllMocks();
  apply.mockReturnValue(true);
  vi.spyOn(HTMLDialogElement.prototype, "showModal").mockImplementation(function (this: HTMLDialogElement) { this.open = true; });
  vi.spyOn(HTMLDialogElement.prototype, "close").mockImplementation(function (this: HTMLDialogElement) { this.open = false; });
  vi.spyOn(window, "confirm").mockReturnValue(true);
});
afterEach(() => { vi.useRealTimers(); vi.restoreAllMocks(); });

describe("online draw.io editing session", () => {
  it("discloses the external origin before creating any iframe", () => {
    render(<DrawioEditorDialog xml={xml} onApply={apply} onClose={close} />);
    expect(screen.getByText(/Opening it shares/)).toBeInTheDocument();
    expect(screen.queryByTitle("Online draw.io editor")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(close).toHaveBeenCalledOnce();
    expect(window.confirm).not.toHaveBeenCalled();
  });
  it("loads full source only after authenticated init, without putting XML in the URL", () => {
    open();
    message({ event: "init" }, "https://evil.example");
    message({ event: "init" }, DRAWIO_ORIGIN, window);
    message("not json");
    message(null);
    expect(post).not.toHaveBeenCalled();
    message({ event: "init" });
    const load = JSON.parse(post.mock.calls[0][0] as string);
    expect(load).toMatchObject({ action: "load", xml, autosave: 1 });
    expect(post.mock.calls[0][1]).toBe(DRAWIO_ORIGIN);
    message({ event: "init" });
    expect(post).toHaveBeenCalledOnce();
    expect(frame.src).not.toContain("mxfile");
    expect(frame.getAttribute("sandbox")).toBe("allow-scripts allow-same-origin");
    expect(frame.getAttribute("referrerpolicy")).toBe("no-referrer");
  });
  it("requests current XML and requires matching export correlation, source and origin", () => {
    open(); loaded();
    message({ event: "autosave", xml: "stale" });
    fireEvent.click(screen.getByRole("button", { name: "Apply to draft" }));
    const request = JSON.parse(post.mock.calls.at(-1)![0] as string);
    expect(request).toMatchObject({ action: "export", format: "xml" });
    message({ event: "export", format: "xml", xml: edited, message: { trackRequest: "old" } });
    message({ event: "save", xml: edited });
    message({ event: "export", format: "xml", xml: edited, message: request }, "https://evil.example");
    expect(apply).not.toHaveBeenCalled();
    message({ event: "export", format: "xml", xml: edited, message: request });
    expect(apply).toHaveBeenCalledExactlyOnceWith(edited);
  });
  it("rejects early saves and invalid XML, and keeps conflicts open", () => {
    open(); message({ event: "save", xml: edited });
    expect(apply).not.toHaveBeenCalled();
    loaded();
    message({ event: "save", xml: "<svg />" });
    expect(apply).not.toHaveBeenCalled();
    expect(screen.getByRole("alert")).toHaveTextContent(/invalid or oversized/);
    apply.mockReturnValue(false);
    message({ event: "save", xml: edited });
    expect(screen.getByRole("alert")).toHaveTextContent(/draft changed/);
    expect(close).not.toHaveBeenCalled();
  });
  it.each(["Cancel", "Close", "Escape", "exit"])("confirms conservative session discard via %s", (action) => {
    open(); loaded();
    vi.mocked(window.confirm).mockReturnValue(false);
    const dismiss = () => {
      if (action === "Escape") fireEvent(screen.getByRole("dialog"), new Event("cancel", { cancelable: true }));
      else if (action === "exit") message({ event: "exit" });
      else fireEvent.click(screen.getByRole("button", { name: action }));
    };
    dismiss(); expect(close).not.toHaveBeenCalled();
    vi.mocked(window.confirm).mockReturnValue(true);
    dismiss(); expect(close).toHaveBeenCalledOnce();
    expect(apply).not.toHaveBeenCalled();
  });
  it("rejects messages from a previous diagram's browsing context", () => {
    const first = open(); loaded(); const old = frame.contentWindow;
    first.unmount(); open(); loaded();
    message({ event: "save", xml: edited }, DRAWIO_ORIGIN, old);
    expect(apply).not.toHaveBeenCalled();
    message({ event: "save", xml: edited });
    expect(apply).toHaveBeenCalledExactlyOnceWith(edited);
  });
  it("cleans up listeners and ignores late events after unmount", () => {
    const view = render(<DrawioEditorDialog xml={xml} onApply={apply} onClose={close} />);
    fireEvent.click(screen.getByRole("button", { name: "Open online editor" }));
    frame = screen.getByTitle("Online draw.io editor") as HTMLIFrameElement;
    const old = frame.contentWindow;
    view.unmount();
    message({ event: "save", xml: edited }, DRAWIO_ORIGIN, old);
    expect(apply).not.toHaveBeenCalled();
  });
  it("keeps the draft untouched on connection and export timeouts", () => {
    vi.useFakeTimers(); open();
    act(() => vi.advanceTimersByTime(30000));
    expect(screen.getByRole("alert")).toHaveTextContent(/did not respond/);
    loaded();
    fireEvent.click(screen.getByRole("button", { name: "Apply to draft" }));
    act(() => vi.advanceTimersByTime(15000));
    expect(screen.getByRole("alert")).toHaveTextContent(/did not return XML/);
    expect(apply).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "Apply to draft" })).not.toBeDisabled();
  });
});
