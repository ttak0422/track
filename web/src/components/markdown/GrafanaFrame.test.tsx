import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { GrafanaFrame, grafanaURL } from "./GrafanaFrame";

const url = "https://grafana.example.test/d/sectors/sectors?from=now-7d&to=now";
describe("Grafana frame", () => {
  it("admits only external HTTPS Grafana paths without credential parameters", () => {
    expect(grafanaURL(url)?.origin).toBe("https://grafana.example.test");
    for (const value of ["javascript:alert(1)", "http://grafana.test/d/a/a", "https://user:secret@grafana.test/d/a/a", "https://127.0.0.1/d/a/a", "https://localhost/d/a/a", "https://[::1]/d/a/a", "https://[::ffff:127.0.0.1]/d/a/a", "https://localhost./d/a/a", "https://grafana.test/login", `${url}&token=secret`, `${url}#token`, "https://grafana.test/d/a/a?next=https://evil.test"]) {
      expect(grafanaURL(value)).toBeNull();
    }
    expect(grafanaURL(url, "https://grafana.example.test")).toBeNull();
  });
  it("starts offline, reconnects, disconnects, and forgets admission across navigation", () => {
    const text = JSON.stringify({url, title:"Sector Grafana"});
    const {container, unmount} = render(<GrafanaFrame text={text} />);
    expect(container.querySelector("iframe")).toBeNull();
    expect(screen.getByRole("link", {name:"Open Grafana externally"})).toHaveAttribute("href",url);
    fireEvent.click(screen.getByRole("button", {name:/Load Grafana/}));
    const first = container.querySelector("iframe")!;
    expect(first).toHaveAttribute("src",url);
    expect(first).toHaveAttribute("sandbox","allow-scripts allow-same-origin");
    fireEvent.click(screen.getByRole("button", {name:"Reconnect"}));
    expect(container.querySelector("iframe")).not.toBe(first);
    fireEvent.click(screen.getByRole("button", {name:"Disconnect Grafana"}));
    expect(container.querySelector("iframe")).toBeNull();
    unmount();
    const again = render(<GrafanaFrame text={text} />);
    expect(again.container.querySelector("iframe")).toBeNull();
  });
  it("stops an active frame when its saved target changes", () => {
    const {container, rerender} = render(<GrafanaFrame text={JSON.stringify({url})} />);
    fireEvent.click(screen.getByRole("button", {name:/Load Grafana/}));
    rerender(<GrafanaFrame text={JSON.stringify({url:"https://other.test/d/x/x"})} />);
    expect(container.querySelector("iframe")).toBeNull();
    rerender(<GrafanaFrame text={JSON.stringify({url})} />);
    expect(container.querySelector("iframe")).toBeNull();
  });
});
