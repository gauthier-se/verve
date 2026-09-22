// The sidebar's collapsed state: a display preference of this device, kept in
// localStorage like the Appearance (ADR 0024), never server data.

const KEY = "verve.sidebar.collapsed";

/** readCollapsed is whether the owner collapsed the sidebar on this device. Storage
 *  can be missing or blocked (a private window, cleared site data), and then the
 *  sidebar is simply expanded. */
export function readCollapsed(storage: Storage): boolean {
  try {
    return storage.getItem(KEY) === "1";
  } catch {
    return false;
  }
}

/** writeCollapsed remembers the choice; a storage that refuses it costs only the
 *  memory, never the toggle. */
export function writeCollapsed(storage: Storage, collapsed: boolean): void {
  try {
    storage.setItem(KEY, collapsed ? "1" : "0");
  } catch {
    // The toggle still works for this visit.
  }
}

/** dashboardInitial is what stands for a Dashboard on the collapsed rail: its first
 *  letter or digit, capitalised, with the full name in the tooltip beside it. */
export function dashboardInitial(name: string): string {
  const first = name.match(/[\p{L}\p{N}]/u)?.[0];
  return first ? first.toLocaleUpperCase() : "·";
}
