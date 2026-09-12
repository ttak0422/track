import { useEffect, useRef, useState } from "react";
import { STATIC_MODE } from "../runtime";
import { useVaultScope } from "../vaultScope";

// VaultSwitcher is the working vault's name at the tab strip's right end plus
// the menu that changes it — the TODO's "show the selected vault in the tab
// bar". It is a text control (variant 1) opening a floating layer (variant
// 3), the same pair the +N overflow menu is. Picking a vault sets the default
// scope for the actions that need exactly one vault (today's journal);
// search stays federated and open tabs keep their own vaults.
export function VaultSwitcher() {
  const { scope, setScope, activeName, vaults, unavailable, isPending } = useVaultScope();
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;

    function onPointerDown(event: globalThis.MouseEvent) {
      if (!ref.current?.contains(event.target as Node)) setOpen(false);
    }

    function onKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") setOpen(false);
    }

    document.addEventListener("mousedown", onPointerDown);
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("mousedown", onPointerDown);
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [open ]);

  // The published site is one vault baked into the bundle: nothing to show.
  if (STATIC_MODE || isPending) return null;
  // A single unregistered vault is the ordinary local workspace — naming it
  // would add chrome for a choice that does not exist.
  if (vaults.length <= 1 && unavailable.length === 0 && activeName === "") return null;

  const current = scope === "" ? activeName : scope;
  const label = current === "" ? "local" : current;

  function choose(vault: string) {
    setScope(vault);
    setOpen(false);
  }

  return (
    <div className="vault-switcher" ref={ref}>
      <button
        className="vault-switcher-toggle"
        type="button"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={`Working vault: ${label}. Change the working vault`}
        title={`Working vault: ${label}`}
        onClick={() => setOpen((value) => !value)}
      >
        <span className="tab-vault">{label}</span>
      </button>
      {open ? (
        <div className="vault-switcher-panel" role="menu" aria-label="Working vault">
          {vaults.map((vault) => {
            const selected = (scope === "" ? vault.active : vault.name === scope) === true;
            return (
              <div key={vault.name === "" ? "__launch__" : vault.name} role="menuitem" className="vault-switcher-item">
                <button
                  type="button"
                  className="vault-switcher-open"
                  aria-current={selected ? true : undefined}
                  title={vault.path || vault.name}
                  onClick={() => choose(vault.active ? "" : vault.name)}
                >
                  {vault.name === "" ? "local" : vault.name}
                  {vault.active ? <span className="vault-switcher-launch">launch</span> : null}
                </button>
              </div>
            );
          })}
          {unavailable.map((vault) => (
            <div key={vault.name} role="menuitem" className="vault-switcher-item">
              <button
                type="button"
                className="vault-switcher-open"
                disabled
                aria-disabled="true"
                title={vault.error ? `${vault.name}: ${vault.error}` : vault.name}
              >
                {vault.name}
                <span className="vault-switcher-launch">unavailable</span>
              </button>
            </div>
          ))}
        </div>
      ) : null}
    </div>
  );
}
