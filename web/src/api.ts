export interface ActivityDay {
  date: string;
  count: number;
}

export interface ActivitySummary {
  start_date: string;
  days: number;
  total: number;
  counts: ActivityDay[];
}

export interface ActivityResponse {
  activity: ActivitySummary;
}

export async function api<T>(path: string): Promise<T> {
  const response = await fetch(path);
  const body = await response.json().catch(() => ({}));

  if (!response.ok) {
    const message =
      typeof body === "object" && body !== null && "error" in body
        ? String(body.error)
        : `${response.status} ${response.statusText}`;
    throw new Error(message);
  }

  return body as T;
}
