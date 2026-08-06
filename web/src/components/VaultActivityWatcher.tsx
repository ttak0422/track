import { useQuery } from "@tanstack/react-query";
import { useEffect } from "react";
import { listNotes } from "../api";
import { useNotifications } from "../notifications";
import { queryKeys } from "../queries";
import { activityMessage, newlyActive, today } from "../vaultActivity";

// How often the notes list is refetched to spot work done outside this tab. The vault is edited by a
// person, not a firehose, so a minute is soon enough to be useful and rare enough to stay invisible.
const POLL_MS = 60_000;

// VaultActivityWatcher toasts what someone else added or changed — another tab, the Neovim plugin, a
// sync landing files under the vault. It renders nothing; the live workspace mounts one.
export function VaultActivityWatcher() {
  const { notify } = useNotifications();
  // Shares the notes cache with the calendar and the home lists; the interval is what keeps it fresh.
  const query = useQuery({ queryKey: queryKeys.notes(), queryFn: listNotes, refetchInterval: POLL_MS });
  const notes = query.data?.notes;

  useEffect(() => {
    if (!notes) return;
    const { titles, priming } = newlyActive(notes, today());
    if (!priming && titles.length > 0) notify(activityMessage(titles));
  }, [notes, notify]);

  return null;
}
