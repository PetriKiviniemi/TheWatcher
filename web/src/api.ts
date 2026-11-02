// Mirrors the Go types in store.go and plots.go.
export interface Filter {
  unitClassIds: number[];
  keywords: string[];
  maxPrice: number | null;
  maxRent: number | null;
  minArea: number | null;
}

export interface Settings {
  topic: string;
  intervalMinutes: number;
  filter: Filter;
}

export interface Site {
  Id: number;
  UnitClassId: number;
  SubArea: string;
  Address: string;
  PropertyLabel: string;
  Area: number | null;
  BuildingRight: number | null;
  Price: number | null;
  Rent: number | null;
}

export interface Profile extends Settings {
  lastCheck: string;
  lastError: string;
  matches: Site[];
}

export type UnitClasses = Record<string, { Name: string; Order: number }>;

export const MIN_INTERVAL_MINUTES = 10;
export const USERNAME_PATTERN = /^[\p{L}\p{N}_.-]{1,32}$/u;

// Same link format as siteURL in plots.go.
export const siteUrl = (site: Site) =>
  `https://kartta.ouka.fi/eSite/fi/SiteSearch#/site/${encodeURIComponent(site.PropertyLabel)}`;

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`/api/${path}`, { ...init, headers: { "Content-Type": "application/json" } });
  if (!res.ok) {
    const body = await res.json().catch(() => null);
    throw new Error(body?.error ?? `Request failed (HTTP ${res.status})`);
  }
  return res.status === 204 ? (undefined as T) : res.json();
}

const profilePath = (username: string) => `profiles/${encodeURIComponent(username)}`;

export const api = {
  profile: (username: string) => request<Profile>(profilePath(username)),
  save: (username: string, settings: Settings) =>
    request<Profile>(profilePath(username), { method: "PUT", body: JSON.stringify(settings) }),
  testAlert: (username: string) => request<void>(`${profilePath(username)}/test`, { method: "POST", body: "{}" }),
  unitClasses: () => request<UnitClasses>("unit-classes"),
};
