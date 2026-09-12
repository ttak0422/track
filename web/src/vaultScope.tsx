import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { useVaultsQuery } from "./queries";
import { STATIC_MODE } from "./runtime";
import type { UnavailableVault, VaultEntry } from "./types";

// The working vault: the default scope for the actions that need exactly one
// vault (opening today's journal), now that the workspace reads all of them.
// "" is the launch vault — what every ?vault=-taking endpoint does with a
// missing parameter — so callers pass scope straight through without
// branching. Search stays federated and takes no scope.
const storageKey = "track.vault-scope";

interface VaultScope {
  // The selected vault's registry name, "" for the launch vault.
  scope: string;
  setScope: (vault: string) => void;
  // The launch vault's registry name ("" when unregistered).
  activeName: string;
  vaults: VaultEntry[];
  unavailable: UnavailableVault[];
  isPending: boolean;
}

const VaultScopeContext = createContext<VaultScope>({
  scope: "",
  setScope: () => {},
  activeName: "",
  vaults: [],
  unavailable: [],
  isPending: false,
});

function readStored(): string {
  try {
    return window.localStorage.getItem(storageKey) ?? "";
  } catch {
    // A full or unavailable storage just means the scope is session-only.
    return "";
  }
}

export function VaultScopeProvider({ children }: { children: ReactNode }) {
  const vaultsQuery = useVaultsQuery(!STATIC_MODE);
  const vaults = vaultsQuery.data?.vaults ?? [];
  const unavailable = vaultsQuery.data?.unavailable ?? [];
  const activeName = vaultsQuery.data?.active.name ?? "";
  const [scope, setScopeState] = useState(readStored);

  const setScope = useCallback((vault: string) => {
    setScopeState(vault);
    try {
      if (vault === "") window.localStorage.removeItem(storageKey);
      else window.localStorage.setItem(storageKey, vault);
    } catch {
      // Session-only then; the selection still holds for this run.
    }
  }, []);

  // A stored name that no longer resolves (vault unregistered, workspace
  // relaunched single-vault) falls back to the launch vault rather than
  // addressing a name the server would refuse.
  useEffect(() => {
    if (vaultsQuery.data === undefined || scope === "") return;
    if (!vaults.some((vault) => vault.name === scope)) setScope("");
  }, [vaultsQuery.data, vaults, scope, setScope]);

  const value = useMemo<VaultScope>(
    () => ({
      scope,
      setScope,
      activeName,
      vaults,
      unavailable,
      isPending: vaultsQuery.isPending,
    }),
    [scope, setScope, activeName, vaults, unavailable, vaultsQuery.isPending],
  );
  return <VaultScopeContext.Provider value={value}>{children}</VaultScopeContext.Provider>;
}

export function useVaultScope(): VaultScope {
  return useContext(VaultScopeContext);
}
