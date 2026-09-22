import { describe, expect, it } from "vitest";

import { dashboardInitial, readCollapsed, writeCollapsed } from "./sidebar";

function memoryStorage(): Storage {
  const m = new Map<string, string>();
  return {
    get length() {
      return m.size;
    },
    clear: () => m.clear(),
    getItem: (k) => m.get(k) ?? null,
    key: (i) => [...m.keys()][i] ?? null,
    removeItem: (k) => void m.delete(k),
    setItem: (k, v) => void m.set(k, v),
  };
}

const throwing = {
  getItem: () => {
    throw new Error("blocked");
  },
  setItem: () => {
    throw new Error("blocked");
  },
} as unknown as Storage;

describe("the collapsed sidebar preference", () => {
  it("is expanded until the owner collapses it", () => {
    expect(readCollapsed(memoryStorage())).toBe(false);
  });

  it("remembers what was written", () => {
    const s = memoryStorage();
    writeCollapsed(s, true);
    expect(readCollapsed(s)).toBe(true);
    writeCollapsed(s, false);
    expect(readCollapsed(s)).toBe(false);
  });

  it("falls back to expanded when storage is unavailable, and never throws", () => {
    expect(readCollapsed(throwing)).toBe(false);
    expect(() => writeCollapsed(throwing, true)).not.toThrow();
  });
});

describe("dashboardInitial", () => {
  it("is the first letter, capitalised", () => {
    expect(dashboardInitial("overview")).toBe("O");
  });

  it("skips what is not a letter or a digit", () => {
    expect(dashboardInitial("  (Sleep) travel")).toBe("S");
    expect(dashboardInitial("2026 block")).toBe("2");
  });

  it("keeps an accented letter whole", () => {
    expect(dashboardInitial("été")).toBe("É");
  });

  it("has something to show for a name with no letter", () => {
    expect(dashboardInitial("—")).toBe("·");
  });
});
