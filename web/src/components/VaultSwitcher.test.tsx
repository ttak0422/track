import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { VaultScopeProvider } from "../vaultScope";
import { VaultSwitcher } from "./VaultSwitcher";

const listVaults = vi.hoisted(() => vi.fn());

vi.mock("../api", () => ({ listVaults }));

function renderSwitcher() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <VaultScopeProvider>
        <VaultSwitcher />
      </VaultScopeProvider>
    </QueryClientProvider>,
  );
}

const twoVaults = {
  active: { name: "main", path: "/v/main" },
  vaults: [
    { name: "main", path: "/v/main", active: true },
    { name: "work", path: "/v/work", active: false },
  ],
  unavailable: [],
};

describe("VaultSwitcher", () => {
  beforeEach(() => {
    window.localStorage.clear();
    listVaults.mockReset();
  });

  it("names the working vault and lists every served vault in its menu", async () => {
    listVaults.mockResolvedValue(twoVaults);
    renderSwitcher();

    const toggle = await screen.findByRole("button", { name: /working vault: main/i });
    expect(toggle).toHaveTextContent("main");

    fireEvent.click(toggle);
    const menu = screen.getByRole("menu", { name: "Working vault" });
    expect(menu).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: /main.*launch/ })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: "work" })).toBeInTheDocument();
  });

  it("switches the working vault and persists it", async () => {
    listVaults.mockResolvedValue(twoVaults);
    renderSwitcher();

    fireEvent.click(await screen.findByRole("button", { name: /working vault: main/i }));
    fireEvent.click(screen.getByRole("menuitem", { name: "work" }).querySelector("button")!);

    // The menu closes and the strip now names the picked vault.
    await waitFor(() =>
      expect(screen.getByRole("button", { name: /working vault: work/i })).toBeInTheDocument(),
    );
    expect(window.localStorage.getItem("track.vault-scope")).toBe("work");
  });

  it("stays hidden for a single unregistered vault", async () => {
    listVaults.mockResolvedValue({
      active: { name: "", path: "/v" },
      vaults: [{ name: "", path: "/v", active: true }],
      unavailable: [],
    });
    renderSwitcher();

    // Let the query settle: the strip gains no switcher for a choice that does not exist.
    await waitFor(() => expect(listVaults).toHaveBeenCalled());
    expect(screen.queryByRole("button", { name: /working vault/i })).not.toBeInTheDocument();
  });

  it("reports an unreachable vault as disabled rather than omitting it", async () => {
    listVaults.mockResolvedValue({
      ...twoVaults,
      unavailable: [{ name: "off", path: "/v/off", error: "unmounted" }],
    });
    renderSwitcher();

    fireEvent.click(await screen.findByRole("button", { name: /working vault: main/i }));
    const row = screen.getByRole("menuitem", { name: /off.*unavailable/ });
    const button = row.querySelector("button")!;
    expect(button).toBeDisabled();
    expect(button).toHaveAttribute("title", expect.stringContaining("unmounted"));
  });
});
