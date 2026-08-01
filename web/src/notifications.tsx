import { createContext, type ReactNode, useCallback, useContext, useMemo, useState } from "react";

interface Notifications {
  notify: (message: string) => void;
}

const NotificationContext = createContext<Notifications>({ notify: () => {} });

export function NotificationProvider({ children }: { children: ReactNode }) {
  const [message, setMessage] = useState("");
  const notify = useCallback((next: string) => setMessage(next), []);
  const value = useMemo(() => ({ notify }), [notify]);

  return (
    <NotificationContext.Provider value={value}>
      {children}
      {message ? (
        <div className="notification-toast" role="alert">
          <span>{message}</span>
          <button type="button" aria-label="Dismiss notification" onClick={() => setMessage("")}>
            ×
          </button>
        </div>
      ) : null}
    </NotificationContext.Provider>
  );
}

export function useNotifications(): Notifications {
  return useContext(NotificationContext);
}
