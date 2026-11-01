import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { STATIC_MODE } from "./runtime";

type StorageStatus = {
  phase: "initializing" | "partial" | "unavailable" | "ready";
  read_only: boolean;
  reason?: string;
  verified?: number;
  home_note_id?: string;
  generation?: number;
};
async function fetchStorage(): Promise<StorageStatus> {
  const response = await fetch("/api/storage", { cache: "no-store" });
  if (response.status === 404) return { phase: "ready", read_only: false };
  if (!response.ok) throw new Error("Storage status is unavailable");
  const data = await response.json();
  if (!["initializing", "partial", "unavailable", "ready"].includes(data?.phase) || typeof data.read_only !== "boolean") {
    throw new Error("Invalid storage status");
  }
  return data;
}
export function useStorageStatus() {
  return useQuery({ queryKey: ["storage-status"], queryFn: fetchStorage, enabled: !STATIC_MODE, retry: false, refetchInterval: 1000 });
}
export function StorageNotice() {
  const status = useStorageStatus();
  const client = useQueryClient();
  const previous = useRef("");
  const [retrying, setRetrying] = useState(false);
  useEffect(() => {
    if (status.data?.phase === "ready" && previous.current !== "ready") {
      void client.invalidateQueries({ predicate: q => q.queryKey[0] !== "storage-status" });
    }
    previous.current = status.data?.phase ?? "";
  }, [status.data?.phase, client]);
  if (STATIC_MODE || !status.data || status.data.phase === "ready") return null;
  async function retry() {
    setRetrying(true);
    try { await fetch("/api/storage", { method: "POST" }); } finally {
      await status.refetch();
      setRetrying(false);
    }
  }
  return <div className="note-recovery-error" role="status">
    <p>{status.data.phase === "initializing" ? "Storage is initializing. Confirmed files remain available read-only." : "Storage is not fully verified. Available information may be partial or stale."}</p>
    {status.data.reason && <p className="muted">{status.data.reason}</p>}
    <button type="button" className="note-recovery-action" disabled={retrying} onClick={() => { void retry().catch(() => {}); }}>Retry storage</button>
  </div>;
}
