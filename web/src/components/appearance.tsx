import * as React from "react";
import { Palette } from "lucide-react";
import { cn } from "@/lib/utils";
import { Button } from "./ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "./ui/popover";
import { Segment } from "./ui/segmented";

/** Appearance is the pair (Mode, Palette) — see CONTEXT.md and ADR 0024.
 *
 *  Mode is how light or dark the surfaces are; `system` follows the OS. Palette is
 *  which curated color set paints the chrome and the categorical chart ramp. The two
 *  are orthogonal: every Palette ships both a light and a dark variant, so switching
 *  one never disturbs the other. Both are display preferences, kept in localStorage
 *  like the summary prefs — per device, never server data. */
export type Mode = "light" | "dark" | "system";
export type PaletteId = "verve" | "slate" | "nord" | "ember" | "forest" | "rose";

/** PALETTES is the closed, curated set (ADR 0024): Verve defines them, the Account
 *  chooses one. The ids are the `data-palette` values index.css keys off. */
export const PALETTES: { id: PaletteId; label: string }[] = [
  { id: "verve", label: "Verve" },
  { id: "slate", label: "Slate" },
  { id: "nord", label: "Nord" },
  { id: "ember", label: "Ember" },
  { id: "forest", label: "Forest" },
  { id: "rose", label: "Rose" },
];

const MODE_KEY = "verve-mode";
const PALETTE_KEY = "verve-palette";
// Pre-Appearance key, when the only axis was dark/light. Read once so an Account
// that had already picked a side keeps it instead of being reset to `system`.
const LEGACY_MODE_KEY = "verve-theme";

const DARK_QUERY = "(prefers-color-scheme: dark)";

interface Appearance {
  mode: Mode;
  palette: PaletteId;
  /** The Mode actually painted: `system` resolved against the OS. */
  resolved: "light" | "dark";
  setMode: (mode: Mode) => void;
  setPalette: (palette: PaletteId) => void;
}

const AppearanceContext = React.createContext<Appearance>({
  mode: "system",
  palette: "verve",
  resolved: "dark",
  setMode: () => {},
  setPalette: () => {},
});

function readMode(): Mode {
  try {
    const stored = localStorage.getItem(MODE_KEY) ?? localStorage.getItem(LEGACY_MODE_KEY);
    if (stored === "light" || stored === "dark" || stored === "system") return stored;
  } catch {
    // Unavailable storage falls back to the default.
  }
  return "system";
}

function readPalette(): PaletteId {
  try {
    const stored = localStorage.getItem(PALETTE_KEY);
    if (PALETTES.some((p) => p.id === stored)) return stored as PaletteId;
  } catch {
    // Unavailable storage falls back to the default.
  }
  return "verve";
}

/** AppearanceProvider owns the Mode and the Palette, persists both, and reflects them
 *  on <html> — the `dark` class (which Tailwind's variants key off) and the
 *  `data-palette` attribute. The inline script in index.html applies the same two
 *  before first paint; this provider is what keeps them in sync afterwards. */
export function AppearanceProvider({ children }: { children: React.ReactNode }) {
  const [mode, setModeState] = React.useState<Mode>(readMode);
  const [palette, setPaletteState] = React.useState<PaletteId>(readPalette);
  const [systemDark, setSystemDark] = React.useState(
    () => typeof window !== "undefined" && window.matchMedia(DARK_QUERY).matches,
  );

  // `system` is resolved here rather than by a CSS media query so the `dark` class
  // stays the single source of truth for both Tailwind and the palette selectors.
  React.useEffect(() => {
    const mq = window.matchMedia(DARK_QUERY);
    const onChange = (e: MediaQueryListEvent) => setSystemDark(e.matches);
    mq.addEventListener("change", onChange);
    return () => mq.removeEventListener("change", onChange);
  }, []);

  const resolved: "light" | "dark" = mode === "system" ? (systemDark ? "dark" : "light") : mode;

  React.useEffect(() => {
    document.documentElement.classList.toggle("dark", resolved === "dark");
  }, [resolved]);

  React.useEffect(() => {
    document.documentElement.setAttribute("data-palette", palette);
  }, [palette]);

  const setMode = React.useCallback((next: Mode) => {
    setModeState(next);
    localStorage.setItem(MODE_KEY, next);
  }, []);

  const setPalette = React.useCallback((next: PaletteId) => {
    setPaletteState(next);
    localStorage.setItem(PALETTE_KEY, next);
  }, []);

  const value = React.useMemo(
    () => ({ mode, palette, resolved, setMode, setPalette }),
    [mode, palette, resolved, setMode, setPalette],
  );
  return <AppearanceContext.Provider value={value}>{children}</AppearanceContext.Provider>;
}

export function useAppearance() {
  return React.useContext(AppearanceContext);
}

/** AppearanceMenu is the sidebar-footer control: the Mode segmented control over the
 *  Palette grid. One popover for both axes, because they are one choice to the eye. */
export function AppearanceMenu() {
  const { mode, palette, resolved, setMode, setPalette } = useAppearance();
  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button variant="ghost" size="icon" aria-label="Appearance">
          <Palette className="size-4" />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-64 space-y-3">
        <Segment
          label="Mode"
          hint="System follows your operating system."
          value={mode}
          options={[
            { value: "light", label: "Light" },
            { value: "dark", label: "Dark" },
            { value: "system", label: "System" },
          ]}
          onChange={(v) => setMode(v as Mode)}
        />
        <div className="space-y-1">
          <p className="text-xs font-medium">Palette</p>
          <div className="grid grid-cols-2 gap-1">
            {PALETTES.map((p) => (
              <button
                key={p.id}
                type="button"
                onClick={() => setPalette(p.id)}
                aria-pressed={palette === p.id}
                className={cn(
                  "flex items-center gap-2 rounded-md border px-2 py-1.5 text-xs transition-colors",
                  palette === p.id
                    ? "border-primary bg-secondary font-medium text-secondary-foreground"
                    : "text-muted-foreground hover:text-foreground",
                )}
              >
                <PaletteSwatch id={p.id} resolved={resolved} />
                {p.label}
              </button>
            ))}
          </div>
        </div>
      </PopoverContent>
    </Popover>
  );
}

/** PaletteSwatch previews a Palette in the Mode currently painted. It carries the
 *  palette's own `data-palette` (and `dark`, when that is the resolved Mode), so the
 *  cascade hands it that palette's tokens — the swatch reads its colors from
 *  index.css rather than restating them here, which is why it can never drift. */
function PaletteSwatch({ id, resolved }: { id: PaletteId; resolved: "light" | "dark" }) {
  return (
    <span
      data-palette={id}
      className={cn("flex shrink-0 gap-0.5", resolved === "dark" && "dark")}
      aria-hidden
    >
      <span className="size-2 rounded-[2px] bg-primary" />
      <span className="size-2 rounded-[2px] bg-chart-2" />
      <span className="size-2 rounded-[2px] bg-chart-3" />
    </span>
  );
}
