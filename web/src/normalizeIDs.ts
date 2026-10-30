import { qualify } from "./vaultId";

// The live server marshals note ids as JSON numbers plus a separate "vault" label, but the frontend
// treats ids as opaque strings (so they line up with route params and with the static site's slug
// ids). stringifyIDs normalizes every id field in a response at the boundary: numbers become
// strings, and an id belonging to a named vault becomes "<vault>~<id>" so two vaults' notes can
// never collide in a route, a tab, or the query cache. The rest of the app never sees a bare
// numeric note id. The vault label itself remains in the payload.
const ID_KEYS = new Set(["note_id", "source_id", "target_id", "center_id", "root"]);

// normalizeIDs applies the same id normalization to a payload that did not arrive through api() —
// an SSE event body. Server-sent state carries the same (vault, note_id) pair as a response, and a
// consumer that parsed it itself would otherwise navigate with a bare id, i.e. to the launch
// vault's same-numbered note.

// A response labels its vault once at the level the label applies to: per hit for a cross-vault
// search, once at the top for a single vault's note or graph. Child objects inherit the nearest
// enclosing label, so graph edges are qualified by the graph's own vault.
export function normalizeIDs<T>(value: T, vault = ""): T {
  return stringifyIDs(value, vault);
}

function stringifyIDs<T>(value: T, vault = ""): T {
  if (Array.isArray(value)) {
    return value.map((item) => stringifyIDs(item, vault)) as T;
  }
  if (value !== null && typeof value === "object") {
    const own = (value as { vault?: unknown }).vault;
    const scope = typeof own === "string" ? own : vault;
    const out: Record<string, unknown> = {};
    for (const [key, child] of Object.entries(value)) {
      out[key] =
        ID_KEYS.has(key) && (typeof child === "number" || typeof child === "string")
          ? qualify(scope, child)
          : stringifyIDs(child, scope);
    }
    return out as T;
  }
  return value;
}
