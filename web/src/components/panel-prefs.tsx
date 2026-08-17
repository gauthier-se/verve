import * as React from "react";
import { Settings2 } from "lucide-react";
import { Button } from "./ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "./ui/popover";
import { Segment } from "./ui/segmented";

const STORAGE_KEY = "verve-summary-prefs";

/** SummaryPrefs are the global, per-Account display choices for a Panel summary: show
 *  figures as **period averages** (a `sum`/extensive total ÷ its window days, a `latest`
 *  Metric's window mean) instead of the window total, and show numbers exact ("94 100")
 *  vs compact ("94,1 k"). Kept in localStorage like the Appearance — a display preference,
 *  not server data. */
export interface SummaryPrefs {
  average: boolean;
  exact: boolean;
}

const defaultPrefs: SummaryPrefs = { average: false, exact: false };

const PrefsContext = React.createContext<{
  prefs: SummaryPrefs;
  set: (patch: Partial<SummaryPrefs>) => void;
}>({ prefs: defaultPrefs, set: () => {} });

export function SummaryPrefsProvider({ children }: { children: React.ReactNode }) {
  const [prefs, setPrefs] = React.useState<SummaryPrefs>(() => {
    try {
      const raw = localStorage.getItem(STORAGE_KEY);
      if (raw) return { ...defaultPrefs, ...(JSON.parse(raw) as Partial<SummaryPrefs>) };
    } catch {
      // Corrupt or unavailable storage falls back to defaults.
    }
    return defaultPrefs;
  });

  React.useEffect(() => {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(prefs));
  }, [prefs]);

  const set = React.useCallback(
    (patch: Partial<SummaryPrefs>) => setPrefs((p) => ({ ...p, ...patch })),
    [],
  );

  return <PrefsContext.Provider value={{ prefs, set }}>{children}</PrefsContext.Provider>;
}

export function useSummaryPrefs() {
  return React.useContext(PrefsContext);
}

/** SummaryPrefsMenu is the gear in the sidebar footer: two segmented toggles that flip
 *  every Panel summary at once (ADR 0019 keeps the summary universal, so its rendering
 *  is a global choice, not a per-Panel one). */
export function SummaryPrefsMenu() {
  const { prefs, set } = useSummaryPrefs();
  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button variant="ghost" size="icon" aria-label="Display settings">
          <Settings2 className="size-4" />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-60 space-y-3">
        <Segment
          label="Summary"
          hint="Averages read better for trends: per-day for totals (steps, calories), a period mean for body mass."
          value={prefs.average ? "average" : "total"}
          options={[
            { value: "total", label: "Total" },
            { value: "average", label: "Average" },
          ]}
          onChange={(v) => set({ average: v === "average" })}
        />
        <Segment
          label="Numbers"
          hint="Compact abbreviates large values."
          value={prefs.exact ? "exact" : "compact"}
          options={[
            { value: "compact", label: "Compact" },
            { value: "exact", label: "Exact" },
          ]}
          onChange={(v) => set({ exact: v === "exact" })}
        />
      </PopoverContent>
    </Popover>
  );
}
